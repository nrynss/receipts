package receipt

import (
	"regexp"
	"strings"
)

var promise = regexp.MustCompile(`(?i)^(?:i (?:will|won't|will not|will never)\b|i promise i (?:will|won't|will not|will never)\b|i promise i'll\b|i'll\b)[^.!?]*[.!?]?$`)
var sentenceBoundary = regexp.MustCompile(`[^.!?]+(?:[.!?]+|$)`)
var curlyContraction = regexp.MustCompile(`(?i)\bi’ll\b`)
var unsafe = regexp.MustCompile(`(?i)(ignore|instruction|system|prompt|assistant|developer|say that|said|says|told|pretend|quote|according to|\"|“|”|‘|’|\n|\r)`)

// IsSourcePromise checks one complete promise sentence from a transcript segment that
// contains multiple sentences. Keep the returned claim byte-for-byte equal to
// that sentence; never let the model join text or paraphrase across sentences.
func IsSourcePromise(source, claim string) bool {
	if claim == "" || HasUnsafePromiseContext(source) || HasUnsafePromiseContext(claim) {
		return false
	}
	for _, sentence := range sentenceBoundary.FindAllString(source, -1) {
		if strings.TrimSpace(sentence) == claim {
			return promise.MatchString(curlyContraction.ReplaceAllString(claim, "I'll"))
		}
	}
	return false
}

// HasUnsafePromiseContext rejects reported speech and instruction-like source context.
func HasUnsafePromiseContext(text string) bool {
	// Treat only the internal apostrophe in first-person "I'll" as a
	// contraction; curly quote marks and any other right apostrophe stay unsafe.
	text = curlyContraction.ReplaceAllString(text, "I'll")
	return unsafe.MatchString(text)
}
