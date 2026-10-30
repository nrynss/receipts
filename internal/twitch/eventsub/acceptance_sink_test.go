package eventsub

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/appeal"
	"receipts/internal/auth"
	"receipts/internal/commands"
	"receipts/internal/lore"
	"receipts/internal/ports"
	"receipts/internal/receipt"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
	"receipts/internal/twitch/chat"
)

type liveExtractor struct{}

func (liveExtractor) Extract(_ context.Context, segs []receipt.Segment) (commands.Extraction, error) {
	if len(segs) == 0 {
		return commands.Extraction{}, nil
	}
	s := segs[len(segs)-1]
	return commands.Extraction{Claim: s.Text, SourceIDs: []string{s.ID}}, nil
}

type safeClassifier struct{}

func (safeClassifier) Sensitive(context.Context, string) (bool, error) { return false, nil }

type recordSender struct {
	text     string
	sent     bool
	messages []string
}

func (s *recordSender) SendChatMessage(_ context.Context, _ auth.ChannelContext, text string) (bool, error) {
	s.text = text
	s.sent = true
	s.messages = append(s.messages, text)
	return true, nil
}

type acceptanceFixture struct {
	ledger    *store.Store
	session   receipt.Session
	manager   *channelruntime.Manager
	authority auth.ChannelContext
	router    *chat.Router
	sink      *liveSink
	sender    *recordSender
	db        *sqlite.DB
}

func newAcceptanceFixture(t *testing.T, parent context.Context, platformID string) *acceptanceFixture {
	t.Helper()
	db, err := sqlite.Open(parent, sqlite.Config{Path: t.TempDir() + "/acceptance.sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := store.New(parent, db, store.Retention{}, nil)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	channel, err := ledger.CreateChannel(parent, platformID, "transport-test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := ledger.StartSession(parent, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	session.StartedAt = time.Now().UTC().Add(-time.Minute)
	if _, err = db.Writer().ExecContext(parent, "UPDATE live_sessions SET started_at=? WHERE id=?", session.StartedAt.UnixMilli(), session.ID); err != nil {
		t.Fatal(err)
	}
	a := auth.ChannelContext{AccountID: "synthetic-transport-account", ChannelID: channel.ID, LiveSessionID: session.ID, Generation: 1}
	if _, err = db.Writer().ExecContext(parent, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES(?,'twitch',?,?,?,?,0,'user:read:chat user:write:chat clips:edit','connected',0)`, a.AccountID, platformID, "transport-test", channel.ID, []byte{1}); err != nil {
		t.Fatal(err)
	}
	seed := receipt.Segment{ID: "seed-evidence", SessionID: session.ID, Start: 0, End: time.Second, Text: "A synthetic prior promise for appeal transport.", SpeakerID: "broadcaster", Attribution: string(ports.AttributionBroadcaster)}
	if err = ledger.AddSegment(parent, seed); err != nil {
		t.Fatal(err)
	}
	evidence := receipt.EvidenceInput{SourceID: seed.ID, Text: seed.Text, Start: seed.Start, End: seed.End, Attribution: seed.Attribution}
	prior, err := ledger.CreateReceipt(parent, receipt.ReceiptInput{ChannelID: channel.ID, SessionID: session.ID, Claim: "Synthetic prior promise", ClippedBy: "transport-fixture", Evidence: []receipt.EvidenceInput{evidence}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.RecordDetection(parent, receipt.DetectionInput{ChannelID: channel.ID, ReceiptID: prior.ID, Evidence: evidence, Outcome: receipt.Stamp}); err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.ApplyReceiptAction(parent, store.DetectionAuthority{AccountID: a.AccountID, ChannelID: a.ChannelID, LiveSessionID: a.LiveSessionID, Generation: a.Generation}, prior.ID, "transport-open-appeal", "appeal", "", 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	manager := channelruntime.New(channelruntime.Config{MaxChannels: 1})
	if _, err = manager.Start(parent, a); err != nil {
		t.Fatal(err)
	}
	sender := &recordSender{}
	router := &chat.Router{Authority: a, ChannelPlatformID: platformID, Capture: &commands.Service{Ledger: ledger, Extractor: liveExtractor{}, Sensitive: safeClassifier{}}, Appeals: appeal.Service{Store: ledger, Runtime: manager}, Lore: lore.Service{Ledger: ledger}, Sender: sender}
	want := []string{"!receipt", "!uphold", "!receipts off", "!receipts on", "!lore"}
	sink := &liveSink{router: router, ledger: ledger, session: session, got: make(chan string, len(want)), fail: make(chan error, 1), expected: want, manager: manager, authority: a, sender: sender}
	t.Cleanup(func() { _ = manager.Cancel(a); _ = ledger.Close(); _ = db.Close() })
	return &acceptanceFixture{ledger: ledger, session: session, manager: manager, authority: a, router: router, sink: sink, sender: sender, db: db}
}

type liveSink struct {
	router    *chat.Router
	ledger    *store.Store
	session   receipt.Session
	got       chan string
	fail      chan error
	expected  []string
	next      int
	manager   *channelruntime.Manager
	authority auth.ChannelContext
	sender    *recordSender
}

func (s *liveSink) HandleEvent(ctx context.Context, raw json.RawMessage, at time.Time) error {
	var e struct {
		MessageID string `json:"message_id"`
		Chatter   string `json:"chatter_user_id"`
		Login     string `json:"chatter_user_login"`
		Message   struct {
			Text string `json:"text"`
		} `json:"message"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &e) != nil {
		return nil
	}
	text := strings.TrimSpace(e.Message.Text)
	fields := strings.Fields(strings.ToLower(text))
	if len(fields) == 0 {
		return nil
	}
	if s.next >= len(s.expected) {
		return nil
	}
	actual := strings.Join(fields, " ")
	expected := false
	for _, pending := range s.expected[s.next:] {
		if actual == pending {
			expected = true
			break
		}
	}
	if !expected {
		return nil
	}
	if actual != s.expected[s.next] {
		return nil
	}
	command := fields[0]
	if command == "!receipt" {
		offset := at.Sub(s.session.StartedAt) - 3*time.Second
		if offset < 0 {
			offset = 0
		}
		if err := s.ledger.AddSegment(ctx, receipt.Segment{ID: "transport-" + e.MessageID, SessionID: s.session.ID, Start: offset, End: offset + 2*time.Second, Text: "I will never take the shortcut again.", SpeakerID: "broadcaster", Attribution: string(ports.AttributionBroadcaster)}); err != nil {
			s.fail <- err
			return nil
		}
	}
	// The synthetic chat actor is the broadcaster; user IDs are never logged.
	if e.Chatter == "" {
		e.Chatter = s.authority.ChannelID
	}
	if e.Login == "" {
		e.Login = "transport-test"
	}
	if err := s.router.HandleEvent(ctx, raw, at); err != nil {
		s.fail <- errors.New("product command service rejected accepted live command")
		return nil
	}
	switch command {
	case "!receipt":
		rows, err := s.ledger.PublicReceipts(ctx, s.authority.ChannelID, 10)
		if err != nil || len(rows) < 2 {
			s.fail <- errors.New("receipt service did not persist the chat request")
			return nil
		}
		if !s.sender.sent || len(s.sender.messages) != 1 || !strings.Contains(s.sender.text, "Receipt taken (@") || !strings.Contains(s.sender.text, "I will never take the shortcut again.") {
			s.fail <- errors.New("receipt capture did not send its bounded chat confirmation")
			return nil
		}
	case "!uphold":
		active, err := s.ledger.ActiveAppeal(ctx, s.authority.ChannelID)
		if err != nil || active == nil || active.Public.UpholdVotes != 1 {
			s.fail <- errors.New("appeal service did not record exactly one vote")
			return nil
		}
	case "!receipts":
		ch, err := s.manager.Resolve(s.authority)
		if err != nil {
			s.fail <- errors.New("control command lost synthetic runtime")
			return nil
		}
		if strings.Contains(text, " off") && !ch.Paused() {
			s.fail <- errors.New("receipts off did not pause runtime")
			return nil
		}
		if strings.Contains(text, " on") && ch.Paused() {
			s.fail <- errors.New("receipts on did not resume runtime")
			return nil
		}
	case "!lore":
		if !s.sender.sent || !strings.Contains(s.sender.text, "2 total | 2 open") {
			s.fail <- errors.New("lore service did not return reconciled safe ledger summary")
			return nil
		}
	}
	s.next++
	s.got <- actual
	return nil
}

func TestLiveSinkExercisesAllFiveRealServicesOffline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newAcceptanceFixture(t, ctx, "platform-test")
	commands := []string{"!receipt", "!uphold", "!receipts off", "!receipts on", "!lore"}
	now := time.Now().UTC()
	for i, command := range commands {
		id := string(rune('a' + i))
		raw, err := json.Marshal(map[string]any{"broadcaster_user_id": "platform-test", "chatter_user_id": "platform-test", "chatter_user_login": "transport-test", "message_id": id, "message": map[string]string{"text": command}, "badges": []any{map[string]string{"set_id": "broadcaster"}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.sink.HandleEvent(ctx, raw, now); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-f.sink.fail:
			t.Fatal(err)
		case got := <-f.sink.got:
			if got != command {
				t.Fatalf("got=%s want=%s", got, command)
			}
		}
	}
	if f.sink.next != 5 {
		t.Fatalf("accepted steps=%d", f.sink.next)
	}
}
