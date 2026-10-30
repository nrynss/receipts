package intelligence

import (
	"context"
	"strings"
	"testing"

	"github.com/nrynss/keel/cost"
	"receipts/internal/auth"
	"receipts/internal/commands"
	runtime "receipts/internal/runtime"
)

type referenceLedger struct{ commands.Ledger }

func (referenceLedger) VerifyLiveGeneration(context.Context, string, string, string, uint64) error {
	return nil
}

type referenceMeter struct{ refs []string }

func (m *referenceMeter) Resolve(auth.ChannelContext) (*runtime.Channel, error) { return nil, nil }
func (m *referenceMeter) WithActive(auth.ChannelContext, func(*runtime.Channel) error) error {
	return nil
}
func (m *referenceMeter) Spend(ctx context.Context, _ auth.ChannelContext, _ string, ref string, _ cost.Price, work cost.Work) error {
	m.refs = append(m.refs, ref)
	_, err := work(ctx)
	return err
}
func TestChargeReferencesUniqueAcrossRestartsAndSessions(t *testing.T) {
	meter := &referenceMeter{}
	makeService := func(session string) *Service {
		return &Service{Authority: auth.ChannelContext{AccountID: "owner", ChannelID: "channel", LiveSessionID: session, Generation: 1}, Ledger: referenceLedger{}, Meter: meter}
	}
	for _, service := range []*Service{makeService("first-session"), makeService("first-session"), makeService("other-session")} {
		if err := service.call(context.Background(), "embedding", 1, func(context.Context) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(meter.refs[0], "first-session") || !strings.Contains(meter.refs[2], "other-session") {
		t.Fatal("references omitted session identity")
	}
	seen := map[string]bool{}
	for _, ref := range meter.refs {
		if seen[ref] {
			t.Fatalf("duplicate reference %q", ref)
		}
		seen[ref] = true
	}
	if len(seen) != 3 {
		t.Fatalf("got %d references", len(seen))
	}
}
