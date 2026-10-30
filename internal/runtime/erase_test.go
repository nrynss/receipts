package runtime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestForgetChannelRetainsGlobalSpendAndRedactsReferences(t *testing.T) {
	b, db := openTestBudgets(t, filepath.Join(t.TempDir(), "erase-budget.db"), 100, 60)
	for _, c := range []string{"a", "b"} {
		if _, err := b.Call(budgetContext(c), c, "live", "judge", "ref", 20, measured(20)); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.ForgetChannel(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Reader().QueryContext(context.Background(), `SELECT ref FROM receipts_budget_charge ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
	}
	if len(refs) != 2 || strings.Contains(refs[0], `"a"`) || !strings.Contains(refs[1], `"b"`) {
		t.Fatalf("refs=%v", refs)
	}
	channel, global, err := b.Remaining(budgetContext("b"), "b")
	if err != nil || channel != 40 || global != 60 {
		t.Fatalf("remaining=%d/%d %v", channel, global, err)
	}
}
