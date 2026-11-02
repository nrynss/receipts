package receipt

import "testing"

func TestSourcePromise(t *testing.T) {
	claim := "I promise I will always pick the blue card."
	for _, tc := range []struct {
		name, source, claim string
		want                bool
	}{
		{"whole", claim, claim, true},
		{"contained", "Blue card. " + claim, claim, true},
		{"paraphrase", claim, "I will pick blue.", false},
		{"partial", "Well, " + claim, claim, false},
		{"joined", claim + " I will win.", claim + " I will win.", false},
		{"quoted", "“" + claim + "”", claim, false},
		{"unsafe context", "She said this. " + claim, claim, false},
		{"curly", "Ready. I’ll choose blue.", "I’ll choose blue.", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSourcePromise(tc.source, tc.claim); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
