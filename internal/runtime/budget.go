package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nrynss/keel/cost"
	costsql "github.com/nrynss/keel/cost/sqlitestore"
	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
)

// BudgetConfig uses USD nanodollars. Both ceilings are finite and strictly
// positive; period zero means a lifetime ceiling, not an automatic reset.
type BudgetConfig struct {
	GlobalLimit    cost.Price
	ChannelLimit   cost.Price
	Period         cost.Period
	ReservationTTL time.Duration
	Now            func() time.Time
}

type Budgets struct {
	store        *costsql.Store
	keyed        *costsql.KeyedBudget
	channelLimit cost.Price
}

// OpenBudgets shares the application database and never owns its lifetime.
func OpenBudgets(ctx context.Context, db *sqlite.DB, cfg BudgetConfig) (*Budgets, error) {
	if cfg.GlobalLimit <= 0 || cfg.ChannelLimit <= 0 || cfg.ChannelLimit > cfg.GlobalLimit {
		return nil, errors.New("runtime: positive channel and global ceilings are required; channel cannot exceed global")
	}
	store, err := costsql.Open(ctx, costsql.Config{DB: db, Namespace: "receipts_budget", Limit: cfg.GlobalLimit, Period: cfg.Period, ReservationTTL: cfg.ReservationTTL, Now: cfg.Now})
	if err != nil {
		return nil, err
	}
	return &Budgets{store: store, keyed: costsql.NewKeyedBudget(store), channelLimit: cfg.ChannelLimit}, nil
}

func (b *Budgets) authorize(ctx context.Context, channelID string) error {
	authority, err := auth.ChannelFromContext(ctx)
	if err != nil || authority.ChannelID != channelID || authority.Device {
		return auth.ErrForbidden
	}
	if b == nil || b.store == nil {
		return errors.New("runtime: budgets unavailable")
	}
	// SetLimit preserves recorded spend when the operator changes the ceiling.
	return b.keyed.SetLimit(ctx, channelID, b.channelLimit)
}

// Call meters every actual execution, including retries. It intentionally
// does not use cost.Once: a job must deduplicate before doing paid work, not
// erase the price of a second provider call after it already happened.
// The supervisor must also reject stale live-session generations before work.
func (b *Budgets) Call(ctx context.Context, channelID, sessionID, kind, ref string, estimate cost.Price, work cost.Work) (cost.Usage, error) {
	if err := b.authorize(ctx, channelID); err != nil {
		return cost.Usage{}, err
	}
	if sessionID == "" || kind == "" || ref == "" || estimate <= 0 || work == nil {
		return cost.Usage{}, errors.New("runtime: session, kind, reference, positive estimate and work are required")
	}
	account, err := b.keyed.Owner(ctx, channelID)
	if err != nil {
		return cost.Usage{}, err
	}
	meter, err := cost.NewContextMeter(account, b.store)
	if err != nil {
		return cost.Usage{}, err
	}
	scoped, _ := json.Marshal([]string{channelID, sessionID, ref})
	return meter.Call(ctx, estimate, kind, string(scoped), work)
}

func (b *Budgets) Remaining(ctx context.Context, channelID string) (channel, global cost.Price, err error) {
	if err = b.authorize(ctx, channelID); err != nil {
		return 0, 0, err
	}
	channel, err = b.keyed.Remaining(ctx, channelID)
	if err != nil {
		return 0, 0, err
	}
	global, err = b.store.Remaining(ctx)
	return
}
