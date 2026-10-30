// Package retrieval ranks eligible receipt embeddings without an external index.
package retrieval

import (
	"errors"
	"math"
	"sort"

	"receipts/internal/receipt"
)

// Space identifies a specific embedding contract. Version is an application
// revision so a changed embedding policy cannot silently mix vectors.
type Space struct {
	Model, Version string
	Dimensions     int
}
type Entry struct {
	Receipt receipt.Receipt
	Vector  []float64
}
type Match struct {
	Receipt receipt.Receipt
	Score   float64
}

func ValidVector(v []float64, dim int) bool {
	if dim <= 0 || len(v) != dim {
		return false
	}
	sum := 0.0
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
		sum += x * x
	}
	return sum > 0 && !math.IsInf(sum, 0)
}

// Nearest accepts only vectors from the same validated space, supplied by the
// scoped ledger. Ties are deterministic. Callers must recheck eligibility before
// applying any decision because receipt state may change after retrieval.
func Nearest(query []float64, space Space, entries []Entry, limit int) ([]Match, error) {
	if space.Model == "" || space.Version == "" || !ValidVector(query, space.Dimensions) || limit < 3 || limit > 5 {
		return nil, errors.New("retrieval: invalid search")
	}
	var result []Match
	for _, e := range entries {
		if !ValidVector(e.Vector, space.Dimensions) {
			return nil, errors.New("retrieval: invalid indexed vector")
		}
		dot, a, b := 0.0, 0.0, 0.0
		for i, x := range query {
			y := e.Vector[i]
			dot += x * y
			a += x * x
			b += y * y
		}
		score := dot / math.Sqrt(a*b)
		if math.IsNaN(score) || math.IsInf(score, 0) {
			return nil, errors.New("retrieval: invalid score")
		}
		result = append(result, Match{Receipt: e.Receipt, Score: score})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score == result[j].Score {
			return result[i].Receipt.ID < result[j].Receipt.ID
		}
		return result[i].Score > result[j].Score
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
