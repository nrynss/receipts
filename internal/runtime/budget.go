package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"

	"github.com/nrynss/keel/cost"
	costsql "github.com/nrynss/keel/cost/sqlitestore"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
)

// BudgetConfig uses USD nanodollars. Both ceilings are finite and strictly
// positive; period zero means a lifetime ceiling, not an automatic reset.
type BudgetConfig struct {
	GlobalLimit    cost.Price
	ChannelLimit   cost.Price
	SessionLimit   cost.Price
	Period         cost.Period
	ReservationTTL time.Duration
	Now            func() time.Time
}

type Budgets struct {
	store        *costsql.Store
	keyed        *costsql.KeyedBudget
	channelLimit cost.Price
	sessionLimit cost.Price
	sessionStore *costsql.Store
	sessions     *costsql.KeyedBudget
	db           *sqlite.DB
	blocked      atomic.Bool
}

// OpenBudgets shares the application database and never owns its lifetime.
func OpenBudgets(ctx context.Context, db *sqlite.DB, cfg BudgetConfig) (*Budgets, error) {
	if cfg.GlobalLimit <= 0 || cfg.ChannelLimit <= 0 || cfg.ChannelLimit > cfg.GlobalLimit {
		return nil, errors.New("runtime: positive channel and global ceilings are required; channel cannot exceed global")
	}
	if cfg.SessionLimit == 0 {
		cfg.SessionLimit = cfg.ChannelLimit
	}
	if cfg.SessionLimit < 0 || cfg.SessionLimit > cfg.ChannelLimit {
		return nil, errors.New("runtime: invalid session ceiling")
	}
	store, err := costsql.Open(ctx, costsql.Config{DB: db, Namespace: "receipts_budget", Limit: cfg.GlobalLimit, Period: cfg.Period, ReservationTTL: cfg.ReservationTTL, Now: cfg.Now})
	if err != nil {
		return nil, err
	}
	sessionStore, err := costsql.Open(ctx, costsql.Config{DB: db, Namespace: "receipts_session_budget", Limit: cfg.GlobalLimit, Period: cfg.Period, ReservationTTL: cfg.ReservationTTL, Now: cfg.Now})
	if err != nil {
		return nil, err
	}
	if _, err = db.Writer().ExecContext(ctx, `CREATE TABLE IF NOT EXISTS receipts_budget_pending (id TEXT PRIMARY KEY)`); err != nil {
		return nil, err
	}
	b := &Budgets{store: store, keyed: costsql.NewKeyedBudget(store), channelLimit: cfg.ChannelLimit, sessionLimit: cfg.SessionLimit, sessionStore: sessionStore, sessions: costsql.NewKeyedBudget(sessionStore), db: db}
	var pending int
	if err = db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM receipts_budget_pending`).Scan(&pending); err != nil {
		return nil, err
	}
	if pending > 0 {
		b.blocked.Store(true)
	}
	return b, nil
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
	if b.blocked.Load() {
		return cost.Usage{}, errors.New("runtime: unresolved paid attempt; spending disabled")
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
	sessionKey, _ := json.Marshal([]string{channelID, sessionID})
	// A session with charges from before the session ledger existed cannot be
	// assigned a fresh allowance. Refuse until a new live session begins.
	var exists, legacy int
	if err := b.db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM receipts_session_budget_owner_budget WHERE owner=?`, string(sessionKey)).Scan(&exists); err != nil {
		return cost.Usage{}, err
	}
	if exists == 0 {
		if err := b.db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM receipts_budget_charge WHERE json_valid(ref) AND json_extract(ref,'$[0]')=? AND json_extract(ref,'$[1]')=?`, channelID, sessionID).Scan(&legacy); err != nil {
			return cost.Usage{}, err
		}
		if legacy > 0 {
			return cost.Usage{}, errors.New("runtime: session has legacy spend; begin a new session")
		}
	}
	if err := b.sessions.SetLimit(ctx, string(sessionKey), b.sessionLimit); err != nil {
		return cost.Usage{}, err
	}
	sessionAccount, err := b.sessions.Owner(ctx, string(sessionKey))
	if err != nil {
		return cost.Usage{}, err
	}
	sessionMeter, err := cost.NewContextMeter(sessionAccount, b.sessionStore)
	if err != nil {
		return cost.Usage{}, err
	}
	var innerErr, workErr error
	var attemptID string
	defer func() {
		if attemptID != "" {
			b.blocked.Store(true)
		}
	}()
	usage, err := sessionMeter.Call(ctx, estimate, kind, string(scoped), func(ctx context.Context) (cost.Usage, error) {
		called := false
		var actual cost.Usage
		actual, innerErr = meter.Call(ctx, estimate, kind, string(scoped), func(ctx context.Context) (cost.Usage, error) {
			attemptID, err = id.New()
			if err != nil {
				return cost.Usage{}, err
			}
			if _, err = b.db.Writer().ExecContext(ctx, `INSERT INTO receipts_budget_pending(id) VALUES(?)`, attemptID); err != nil {
				attemptID = ""
				return cost.Usage{}, err
			}
			called = true
			var u cost.Usage
			u, workErr = work(ctx)
			return u, nil
		})
		if innerErr != nil && !called {
			return cost.Usage{}, innerErr
		}
		if innerErr != nil {
			return cost.Usage{}, nil
		}
		return actual, nil
	})
	if attemptID != "" {
		if err != nil || innerErr != nil {
			b.blocked.Store(true)
		} else if _, e := b.db.Writer().ExecContext(context.WithoutCancel(ctx), `DELETE FROM receipts_budget_pending WHERE id=?`, attemptID); e != nil {
			b.blocked.Store(true)
			return cost.Usage{}, e
		} else {
			attemptID = ""
		}
	}
	if err != nil {
		return cost.Usage{}, err
	}
	if innerErr != nil {
		return cost.Usage{}, innerErr
	}
	if workErr != nil {
		return cost.Usage{}, workErr
	}
	return usage, nil
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

// ForgetChannel removes keyed spend state while retaining anonymized global charges.
func (b *Budgets) ForgetChannel(ctx context.Context, channelID string) error {
	if b == nil || b.keyed == nil || channelID == "" {
		return errors.New("runtime: budget unavailable")
	}
	err := b.keyed.ForgetOwner(ctx, channelID)
	if err != nil && !errors.Is(err, cost.ErrUnknownOwner) {
		return err
	}
	rows, err := b.db.Reader().QueryContext(ctx, `SELECT owner FROM receipts_session_budget_owner_budget WHERE json_valid(owner) AND json_extract(owner, '$[0]')=?`, channelID)
	if err != nil {
		return err
	}
	var keys []string
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, key)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, key := range keys {
		if err = b.sessions.ForgetOwner(ctx, key); err != nil && !errors.Is(err, cost.ErrUnknownOwner) {
			return err
		}
	}
	if _, err = b.db.Writer().ExecContext(ctx, `UPDATE receipts_session_budget_charge SET ref='erased:' || id WHERE json_valid(ref) AND json_extract(ref, '$[0]')=?`, channelID); err != nil {
		return err
	}
	return b.redactChannelCharges(ctx, channelID)
}

func (b *Budgets) redactChannelCharges(ctx context.Context, channelID string) error {
	_, err := b.db.Writer().ExecContext(ctx, `UPDATE receipts_budget_charge SET ref='erased:' || id WHERE json_valid(ref) AND json_extract(ref, '$[0]')=?`, channelID)
	return err
}
