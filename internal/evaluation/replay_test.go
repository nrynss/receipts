package evaluation

import (
	"receipts/internal/detect"
	"testing"
)

func TestReplaySyntheticContract(t *testing.T) {
	raw := []byte("{\"id\":\"a\",\"split\":\"dev\",\"claim\":\"I will stop.\",\"later\":\"I continued.\",\"label\":\"contradiction\",\"expected\":\"stamp\"}\n{\"id\":\"b\",\"split\":\"heldout\",\"claim\":\"I will stop.\",\"later\":\"I stopped.\",\"label\":\"no_contradiction\",\"expected\":\"quiet\"}\n")
	pairs, hash, err := ReadPairs(raw)
	if err != nil {
		t.Fatal(err)
	}
	b := Bundle{Manifest: Manifest{DatasetSHA256: hash, Split: "dev", Provenance: "synthetic contract only", JudgeModel: "gpt-4o-mini-2024-07-18", EmbeddingModel: "text-embedding-3-small", Policy: "review-only-v1", JudgeSourceSHA256: JudgeSourceHash, RetrievalSourceSHA256: RetrievalSourceHash, PolicySourceSHA256: PolicySourceHash}, Records: []Recording{{ID: "a", ClaimVector: []float64{1, 0}, LaterVector: []float64{1, 0}, Verdict: detect.Contradicts, ClaimSourceID: "a:claim", LaterSourceID: "a:later", UsageMeasured: true}}}
	got, err := Report(pairs, hash, b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || got.Stamps != 0 || got.Abstentions != 1 || got.RetrievalAttempts != 1 || got.Precision != nil || got.Recall != nil {
		t.Fatalf("unexpected report: %+v", got)
	}
	b.Records = append(b.Records, Recording{ID: "b"})
	if _, err = Report(pairs, hash, b); err == nil {
		t.Fatal("accepted cross-partition recording")
	}
	b.Records = b.Records[:1]
	b.Records[0].LaterSourceID = "wrong"
	if _, err = Report(pairs, hash, b); err == nil {
		t.Fatal("accepted wrong reference")
	}
}

func TestAccuracyRequiresHashBoundIndependentReview(t *testing.T) {
	raw := []byte("{\"id\":\"a\",\"split\":\"dev\",\"claim\":\"I will stop.\",\"later\":\"I continued.\",\"label\":\"contradiction\",\"expected\":\"stamp\"}\n")
	p, h, err := ReadPairs(raw)
	if err != nil {
		t.Fatal(err)
	}
	b := Bundle{Manifest: Manifest{DatasetSHA256: h, Split: "dev", Provenance: "synthetic contract only", JudgeModel: "gpt-4o-mini-2024-07-18", EmbeddingModel: "text-embedding-3-small", Policy: "review-only-v1", JudgeSourceSHA256: JudgeSourceHash, RetrievalSourceSHA256: RetrievalSourceHash, PolicySourceSHA256: PolicySourceHash}, Records: []Recording{{ID: "a", ClaimVector: []float64{1, 0}, LaterVector: []float64{1, 0}, Verdict: detect.Contradicts, ClaimSourceID: "a:claim", LaterSourceID: "a:later", UsageMeasured: true}}}
	if _, err = AccuracyReport(p, h, b, Review{DatasetSHA256: "wrong", Reviewer: "reviewer", Decision: "independent-human-reviewed"}); err == nil {
		t.Fatal("accepted wrong review hash")
	}
	r, err := AccuracyReport(p, h, b, Review{DatasetSHA256: h, Reviewer: "reviewer", Decision: "independent-human-reviewed"})
	if err != nil || !r.AccuracyReviewed || r.Recall == nil || *r.Recall != 0 {
		t.Fatalf("reviewed report %+v %v", r, err)
	}
}
