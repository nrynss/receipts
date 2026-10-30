package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
)

func budgetContext(channel string) context.Context {
	return auth.WithChannelContext(context.Background(), auth.ChannelContext{AccountID: "owner-" + channel, ChannelID: channel})
}
func openTestBudgets(t *testing.T, path string, global, channel cost.Price) (*Budgets, *sqlite.DB) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	b, err := OpenBudgets(context.Background(), db, BudgetConfig{GlobalLimit: global, ChannelLimit: channel})
	if err != nil {
		t.Fatal(err)
	}
	return b, db
}
func measured(price cost.Price) cost.Work {
	return func(context.Context) (cost.Usage, error) { return cost.Usage{Price: price, Measured: true}, nil }
}
func TestBudgetChannelIsolationGlobalCeilingAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "budget.db")
	b, db := openTestBudgets(t, path, 100, 60)
	a, z := budgetContext("a"), budgetContext("z")
	if _, err := b.Call(a, "z", "live", "judge", "same", 40, measured(40)); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("foreign budget: %v", err)
	}
	if _, err := b.Call(a, "a", "live", "judge", "same", 40, measured(40)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Call(z, "z", "live", "judge", "same", 40, measured(40)); err != nil {
		t.Fatal(err)
	}
	ac, g, err := b.Remaining(a, "a")
	if err != nil || ac != 20 || g != 20 {
		t.Fatalf("remaining=%d/%d err=%v", ac, g, err)
	}
	if _, err := b.Call(a, "a", "live", "judge", "over", 21, measured(21)); !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("owner ceiling: %v", err)
	}
	if _, err := b.Call(z, "z", "live", "judge", "last", 20, measured(20)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, _ := openTestBudgets(t, path, 100, 60)
	ac, g, err = reopened.Remaining(a, "a")
	if err != nil || ac != 20 || g != 0 {
		t.Fatalf("restart remaining=%d/%d err=%v", ac, g, err)
	}
	called := false
	_, err = reopened.Call(a, "a", "new-live", "judge", "later", 1, func(context.Context) (cost.Usage, error) { called = true; return cost.Usage{}, nil })
	if !errors.Is(err, cost.ErrOverBudget) || called {
		t.Fatalf("global ceiling: called=%v err=%v", called, err)
	}
}
func TestBudgetCancellationAndLateCancellationSettle(t *testing.T) {
	b, _ := openTestBudgets(t, filepath.Join(t.TempDir(), "budget.db"), 200, 100)
	ctx, cancel := context.WithCancel(budgetContext("a"))
	_, err := b.Call(ctx, "a", "live", "judge", "fail", 40, func(context.Context) (cost.Usage, error) { cancel(); return cost.Usage{}, context.Canceled })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ac, g, err := b.Remaining(budgetContext("a"), "a")
	if err != nil || ac != 60 || g != 160 {
		t.Fatalf("released=%d/%d err=%v", ac, g, err)
	}
	ctx, cancel = context.WithCancel(budgetContext("a"))
	_, err = b.Call(ctx, "a", "live", "judge", "success", 40, func(context.Context) (cost.Usage, error) { cancel(); return cost.Usage{Price: 30, Measured: true}, nil })
	if err != nil {
		t.Fatal(err)
	}
	ac, g, err = b.Remaining(budgetContext("a"), "a")
	if err != nil || ac != 30 || g != 130 {
		t.Fatalf("settled=%d/%d err=%v", ac, g, err)
	}
}
func TestBudgetConcurrentOwnersCannotOverspend(t *testing.T) {
	b, _ := openTestBudgets(t, filepath.Join(t.TempDir(), "budget.db"), 10, 10)
	var wg sync.WaitGroup
	results := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			channel := "a"
			if i%2 == 1 {
				channel = "b"
			}
			_, err := b.Call(budgetContext(channel), channel, "live", "judge", "same", 1, measured(1))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, cost.ErrOverBudget) {
			t.Fatal(err)
		}
	}
	if successes != 10 {
		t.Fatalf("paid executions=%d want10", successes)
	}
	_, g, err := b.Remaining(budgetContext("a"), "a")
	if err != nil || g != 0 {
		t.Fatalf("global=%d err=%v", g, err)
	}
}
func TestBudgetRejectsDeviceAndInvalidLimits(t *testing.T) {
	b, db := openTestBudgets(t, filepath.Join(t.TempDir(), "budget.db"), 100, 60)
	for _, ctx := range []context.Context{context.Background(), auth.WithChannelContext(context.Background(), auth.ChannelContext{AccountID: "owner", ChannelID: "a", Device: true})} {
		if _, err := b.Call(ctx, "a", "live", "judge", "x", 1, measured(1)); !errors.Is(err, auth.ErrForbidden) {
			t.Fatal(err)
		}
	}
	if _, err := OpenBudgets(context.Background(), db, BudgetConfig{GlobalLimit: 1, ChannelLimit: 2}); err == nil {
		t.Fatal("accepted invalid ceilings")
	}
}

func TestSessionCapConcurrentAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	db, err := sqlite.Open(context.Background(), sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	cfg := BudgetConfig{GlobalLimit: 100, ChannelLimit: 100, SessionLimit: 10}
	b, err := OpenBudgets(context.Background(), db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := b.Call(budgetContext("a"), "a", "live", "judge", "ref", 1, measured(1))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
		} else if !errors.Is(err, cost.ErrOverBudget) {
			t.Fatal(err)
		}
	}
	if ok != 10 {
		t.Fatalf("successful calls=%d", ok)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlite.Open(context.Background(), sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b, err = OpenBudgets(context.Background(), db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Call(budgetContext("a"), "a", "live", "judge", "ref", 1, measured(1)); !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("session restart cap: %v", err)
	}
	if _, err = b.Call(budgetContext("a"), "a", "next", "judge", "ref", 1, measured(1)); err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err = b.ForgetChannel(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	_, global, err := b.Remaining(budgetContext("a"), "a")
	if err != nil || global != 89 {
		t.Fatalf("global after deletion=%d: %v", global, err)
	}
}

func TestSessionAmbiguousAttemptSettlesOnce(t *testing.T) {
	b, _ := openTestBudgets(t, filepath.Join(t.TempDir(), "ambiguous.db"), 100, 100)
	ctx, cancel := context.WithCancel(budgetContext("a"))
	calls := 0
	_, err := b.Call(ctx, "a", "live", "judge", "ambiguous", 40, func(context.Context) (cost.Usage, error) { calls++; cancel(); return cost.Usage{}, nil })
	if err != nil || calls != 1 {
		t.Fatalf("attempts=%d err=%v", calls, err)
	}
	channel, global, err := b.Remaining(budgetContext("a"), "a")
	if err != nil || channel != 60 || global != 60 {
		t.Fatalf("remaining=%d/%d err=%v", channel, global, err)
	}
}

func TestSettlementFailureBlocksAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "failure.db")
	db, err := sqlite.Open(context.Background(), sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	cfg := BudgetConfig{GlobalLimit: 100, ChannelLimit: 100}
	b, err := OpenBudgets(context.Background(), db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Call(budgetContext("a"), "a", "live", "judge", "failure", 10, func(context.Context) (cost.Usage, error) {
		if e := db.Close(); e != nil {
			t.Fatal(e)
		}
		return cost.Usage{Price: 3, Measured: true}, nil
	})
	if err == nil {
		t.Fatal("settlement unexpectedly succeeded")
	}
	db, err = sqlite.Open(context.Background(), sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b, err = OpenBudgets(context.Background(), db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = b.Call(budgetContext("a"), "a", "live", "judge", "next", 1, func(context.Context) (cost.Usage, error) { called = true; return cost.Usage{}, nil })
	if err == nil || called {
		t.Fatalf("unresolved settlement: called=%v err=%v", called, err)
	}
}

func TestLegacySessionCannotGetFreshAllowance(t *testing.T) {
	b, _ := openTestBudgets(t, filepath.Join(t.TempDir(), "legacy.db"), 100, 100)
	ctx := budgetContext("a")
	if err := b.authorize(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	account, err := b.keyed.Owner(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	meter, err := cost.NewContextMeter(account, b.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = meter.Call(ctx, 10, "judge", `["a","live","old"]`, measured(10)); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = b.Call(ctx, "a", "live", "judge", "new", 1, func(context.Context) (cost.Usage, error) { called = true; return cost.Usage{}, nil })
	if err == nil || called {
		t.Fatalf("legacy session called=%v err=%v", called, err)
	}
}
