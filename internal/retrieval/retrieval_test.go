package retrieval

import (
	"math"
	"receipts/internal/receipt"
	"testing"
)

func TestNearest(t *testing.T) {
	s := Space{Model: "fixture", Version: "1", Dimensions: 2}
	entries := []Entry{{receipt.Receipt{ID: "opposite"}, []float64{-1, 0}}, {receipt.Receipt{ID: "diagonal"}, []float64{1, 1}}, {receipt.Receipt{ID: "exact"}, []float64{1, 0}}, {receipt.Receipt{ID: "orthogonal"}, []float64{0, 1}}}
	got, err := Nearest([]float64{1, 0}, s, entries, 3)
	if err != nil || len(got) != 3 || got[0].Receipt.ID != "exact" || got[1].Receipt.ID != "diagonal" || got[2].Receipt.ID != "orthogonal" {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err = Nearest([]float64{1, 0}, Space{Model: "fixture", Version: "1", Dimensions: 3}, entries, 3); err == nil {
		t.Fatal("dimension mismatch accepted")
	}
	if ValidVector([]float64{math.NaN(), 1}, 2) || ValidVector([]float64{0, 0}, 2) {
		t.Fatal("invalid vector accepted")
	}
}
