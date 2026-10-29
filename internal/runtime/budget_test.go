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
func TestBudgetCancellationReleasesAndLateCancellationSettles(t *testing.T) {
	b, _ := openTestBudgets(t, filepath.Join(t.TempDir(), "budget.db"), 100, 60)
	ctx, cancel := context.WithCancel(budgetContext("a"))
	_, err := b.Call(ctx, "a", "live", "judge", "fail", 40, func(context.Context) (cost.Usage, error) { cancel(); return cost.Usage{}, context.Canceled })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ac, g, err := b.Remaining(budgetContext("a"), "a")
	if err != nil || ac != 60 || g != 100 {
		t.Fatalf("released=%d/%d err=%v", ac, g, err)
	}
	ctx, cancel = context.WithCancel(budgetContext("a"))
	_, err = b.Call(ctx, "a", "live", "judge", "success", 40, func(context.Context) (cost.Usage, error) { cancel(); return cost.Usage{Price: 30, Measured: true}, nil })
	if err != nil {
		t.Fatal(err)
	}
	ac, g, err = b.Remaining(budgetContext("a"), "a")
	if err != nil || ac != 30 || g != 70 {
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
