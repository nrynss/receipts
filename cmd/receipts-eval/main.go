// receipts-eval reports one complete, recorded corpus partition offline.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"receipts/internal/evaluation"
)

func main() {
	dataset := flag.String("dataset", "testdata/eval/pairs.jsonl", "labelled corpus")
	recording := flag.String("recording", "", "sanitized recording bundle")
	reviewFile := flag.String("review", "", "independent human review assertion JSON (required for accuracy)")
	flag.Parse()
	if *recording == "" {
		fmt.Fprintln(os.Stderr, "recording is required")
		os.Exit(2)
	}
	raw, e := os.ReadFile(*dataset)
	if e != nil {
		fail(e)
	}
	pairs, hash, e := evaluation.ReadPairs(raw)
	if e != nil {
		fail(e)
	}
	raw, e = os.ReadFile(*recording)
	if e != nil {
		fail(e)
	}
	var b evaluation.Bundle
	if e = json.Unmarshal(raw, &b); e != nil {
		fail(e)
	}
	var r evaluation.Summary
	if *reviewFile == "" {
		r, e = evaluation.Report(pairs, hash, b)
	} else {
		raw, e = os.ReadFile(*reviewFile)
		if e != nil {
			fail(e)
		}
		var review evaluation.Review
		if e = json.Unmarshal(raw, &review); e != nil {
			fail(e)
		}
		r, e = evaluation.AccuracyReport(pairs, hash, b, review)
	}
	if e != nil {
		fail(e)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if e = enc.Encode(r); e != nil {
		fail(e)
	}
}
func fail(e error) { fmt.Fprintln(os.Stderr, e); os.Exit(1) }
