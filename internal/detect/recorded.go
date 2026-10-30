package detect

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"receipts/internal/receipt"
)

// RecordedJudge is an offline adapter keyed by the source segment ID. Missing
// or invalid records fail closed; it makes no provider or network calls.
type RecordedJudge map[string]Verdict

func LoadRecorded(r io.Reader) (RecordedJudge, error) {
	var values RecordedJudge
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&values); err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, errors.New("detect: empty recording")
	}
	for _, v := range values {
		if v != Contradicts && v != DoesNot && v != Unsure {
			return nil, errors.New("detect: invalid recording")
		}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("detect: trailing recording data")
	}
	return values, nil
}
func (r RecordedJudge) Judge(ctx context.Context, _ receipt.Receipt, source receipt.Segment) (Verdict, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, ok := r[source.ID]
	if !ok {
		return "", errors.New("detect: recording missing")
	}
	return value, nil
}
