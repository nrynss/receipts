package evaluation

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

type pair struct {
	ID       string `json:"id"`
	Split    string `json:"split"`
	Claim    string `json:"claim"`
	Later    string `json:"later"`
	Label    string `json:"label"`
	Expected string `json:"expected"`
	Source   string `json:"source"`
}

func TestPairsAreStableAndHaveFixedSplits(t *testing.T) {
	f, err := os.Open("../../testdata/eval/pairs.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	seen := map[string]bool{}
	counts := map[string]int{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var p pair
		if err := json.Unmarshal(scanner.Bytes(), &p); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		if p.ID == "" || seen[p.ID] {
			t.Fatalf("empty or duplicate id %q", p.ID)
		}
		seen[p.ID] = true
		if p.Split != "dev" && p.Split != "heldout" {
			t.Fatalf("%s has invalid split %q", p.ID, p.Split)
		}
		if p.Claim == "" || p.Later == "" || p.Source != "original Codex text" {
			t.Fatalf("%s is missing original text provenance", p.ID)
		}
		if p.Expected != "stamp" && p.Expected != "quiet" && p.Expected != "review" {
			t.Fatalf("%s has invalid expected result %q", p.ID, p.Expected)
		}
		if p.Label != "contradiction" && p.Label != "no_contradiction" && p.Label != "review" && p.Label != "suppress" {
			t.Fatalf("%s has invalid label %q", p.ID, p.Label)
		}
		if p.Label == "suppress" && p.Expected != "quiet" {
			t.Fatalf("%s must suppress sensitive material", p.ID)
		}
		counts[p.Split]++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) < 40 || counts["dev"] == 0 || counts["heldout"] == 0 {
		t.Fatalf("need at least 40 examples with both splits; got %d (%v)", len(seen), counts)
	}
}
