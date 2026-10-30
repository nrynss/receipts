package main

import (
	"receipts/internal/evaluation"
	"testing"
)

func TestDiagnosticModeCannotAuthorizeAccuracy(t *testing.T) {
	for _, tc := range []struct {
		partial      bool
		review, root string
		wantErr      bool
	}{{true, "review.json", ".", true}, {false, "", "old-source", true}, {true, "", "old-source", false}, {false, "review.json", ".", false}} {
		if (validateReportMode(tc.partial, tc.review, tc.root) != nil) != tc.wantErr {
			t.Fatalf("unexpected mode result %+v", tc)
		}
	}
}

func TestReceiptProvenanceRejectsDuplicateSources(t *testing.T) {
	good := []evaluation.ReceiptSource{{ReceiptID: "r1", SourceID: "s1"}, {ReceiptID: "r2", SourceID: "s2"}}
	if !validReceiptSources(good, []string{"s1", "s2"}) {
		t.Fatal("valid provenance rejected")
	}
	bad := []evaluation.ReceiptSource{{ReceiptID: "r1", SourceID: "s1"}, {ReceiptID: "r2", SourceID: "s1"}}
	if validReceiptSources(bad, []string{"s1", "s1"}) {
		t.Fatal("duplicate source accepted")
	}
}
