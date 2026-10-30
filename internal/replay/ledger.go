package replay

import (
	"context"
	"fmt"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/ports"
	"receipts/internal/receipt"
	"receipts/internal/store"
)

// Ledger is the real T1.2 store boundary used by fixture applications. The
// deterministic channel and session are created before recorded inputs run.
type Ledger struct {
	Store   *store.Store
	db      *sqlite.DB
	Channel receipt.Channel
	Session receipt.Session
}

func OpenLedger(ctx context.Context, path string, clock *Clock) (*Ledger, error) {
	if clock == nil {
		return nil, fmt.Errorf("%w: clock is required", ErrInvalidFixture)
	}
	db, err := sqlite.Open(ctx, sqlite.Config{Path: path, Now: clock.Now})
	if err != nil {
		return nil, err
	}
	s, err := store.NewWithIDSource(ctx, db, store.Retention{MaxSegments: 600}, clock.Now, IDs())
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	channel, err := s.CreateChannel(ctx, "fixture-channel", "fixture")
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	session, err := s.StartSession(ctx, channel.ID)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Ledger{Store: s, db: db, Channel: channel, Session: session}, nil
}

func (l *Ledger) Close() error { return l.db.Close() }

// SeedOwnerAccount creates the inert account row required by recorded
// detection acceptance. It stores no provider credential and cannot activate
// a provider adapter.
func (l *Ledger) SeedOwnerAccount(ctx context.Context) error {
	_, err := l.db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('fixture-owner','twitch','fixture-subject','fixture',?,X'00',0,'','connected',0)`, l.Channel.ID)
	return err
}

// TranscriptSink adapts recorded transcript inputs to the actual durable
// ledger. Session IDs are validated, never rewritten or inferred.
type TranscriptSink struct{ Ledger *Ledger }

func (s TranscriptSink) AcceptTranscript(ctx context.Context, in ports.TranscriptSegment) error {
	if s.Ledger == nil || in.SessionID != s.Ledger.Session.ID {
		return fmt.Errorf("%w: transcript session does not match replay ledger", ErrInvalidFixture)
	}
	return s.Ledger.Store.AddSegment(ctx, receipt.Segment{ID: in.ID, SessionID: in.SessionID, Start: in.Start, End: in.End, Text: in.Text, SpeakerID: in.SpeakerID, Attribution: string(in.Attribution)})
}
