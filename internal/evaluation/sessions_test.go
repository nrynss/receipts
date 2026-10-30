package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestSyntheticSessionsHaveFixedIdentityAndSourceContracts(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/eval/sessions.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != "734826b070e27e9de96ef3b4b2f4607de52e5ccefe2d241cea44989c538b1216" {
		t.Fatal("fixture changed; review and version before use")
	}
	var v struct {
		Provenance string `json:"provenance"`
		Sessions   []struct {
			ID          string `json:"id"`
			Split       string `json:"split"`
			LaterID     string `json:"later_id"`
			Later       string `json:"later"`
			Attribution string `json:"later_attribution"`
			Target      string `json:"target_id"`
			Candidates  []struct {
				ID          string `json:"id"`
				Claim       string `json:"claim"`
				Attribution string `json:"attribution"`
				Before      bool   `json:"before_later"`
			} `json:"candidates"`
		} `json:"sessions"`
	}
	if err = json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.Provenance == "" || len(v.Sessions) != 4 {
		t.Fatal("missing provenance or sessions")
	}
	seen := map[string]bool{}
	splits := map[string]int{}
	for _, s := range v.Sessions {
		if seen[s.ID] || s.ID == "" || s.LaterID == "" || s.Later == "" || len(s.Candidates) < 4 || (s.Split != "dev" && s.Split != "heldout") {
			t.Fatal("invalid session")
		}
		seen[s.ID] = true
		splits[s.Split]++
		found := false
		ids := map[string]bool{}
		for _, c := range s.Candidates {
			if c.ID == "" || ids[c.ID] || c.Claim == "" || c.Attribution == "" || !c.Before {
				t.Fatal("invalid candidate")
			}
			ids[c.ID] = true
			found = found || c.ID == s.Target
		}
		if !found {
			t.Fatal("missing target")
		}
	}
	if splits["dev"] == 0 || splits["heldout"] == 0 {
		t.Fatal("missing split")
	}
}
