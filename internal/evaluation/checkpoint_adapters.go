package evaluation

import (
	"context"
	"errors"

	"github.com/nrynss/keel/cost"
	"receipts/internal/commands"
	"receipts/internal/judge"
	"receipts/internal/providerusage"
	"receipts/internal/receipt"
)

// CheckpointAdapters writes a reservation before a provider call and records
// reported usage immediately, before persisting its sanitized structured output.
func CheckpointAdapters(base SessionAdapters, c *Checkpoint) SessionAdapters {
	run := func(ctx context.Context, e SessionEvent, reserve int64, work func(context.Context) error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		i, err := c.Begin(e, reserve)
		if err != nil {
			return err
		}
		e.State = "reserved"
		e.ReservationNanoUSD = reserve
		var writeErr error
		ctx = context.WithValue(ctx, requestDigestKey{}, func(v string) { e.RequestSHA256 = v })
		ctx = providerusage.WithObserver(ctx, func(u cost.Usage) {
			e.Measured = u.Measured
			e.SpendNanoUSD = int64(u.Price)
			if u.Measured {
				e.State = "measured"
			}
			if x := c.Update(i, e); x != nil {
				writeErr = x
			}
		})
		err = work(ctx)
		if writeErr != nil {
			return errors.New("usage checkpoint failed")
		}
		e.Failed = err != nil

		if x := c.Update(i, e); x != nil {
			return errors.New("event checkpoint failed")
		}
		if err != nil {
			return err
		}
		if !e.Measured {
			return errors.New("provider usage unavailable")
		}
		if e.SpendNanoUSD > reserve {
			return errors.New("provider usage exceeds reservation")
		}
		return nil
	}
	return SessionAdapters{Dimensions: base.Dimensions, Budget: base.Budget,
		Extractor: sessionExtract(func(ctx context.Context, s []receipt.Segment) (commands.Extraction, error) {
			e := SessionEvent{Kind: "extract", InputSHA256: extractionKey(s)}
			var out commands.Extraction
			err := run(ctx, e, 25_000_000, func(c context.Context) error { var x error; out, x = base.Extractor.Extract(c, s); return x })
			if err == nil {
				i := len(c.Bundle.Sessions[c.caseIndex].Trace.Events) - 1
				ev := c.Bundle.Sessions[c.caseIndex].Trace.Events[i]
				ev.Extraction = out
				ev.State = "complete"
				if x := c.Update(i, ev); x != nil {
					return commands.Extraction{}, x
				}
			}
			return out, err
		}),
		Sensitive: sessionSensitive(func(ctx context.Context, text string) (bool, error) {
			e := SessionEvent{Kind: "sensitive", InputSHA256: inputHash(text)}
			var out bool
			err := run(ctx, e, 25_000_000, func(c context.Context) error { var x error; out, x = base.Sensitive.Sensitive(c, text); return x })
			if err == nil {
				i := len(c.Bundle.Sessions[c.caseIndex].Trace.Events) - 1
				ev := c.Bundle.Sessions[c.caseIndex].Trace.Events[i]
				ev.Sensitive = out
				ev.State = "complete"
				if x := c.Update(i, ev); x != nil {
					return false, x
				}
			}
			return out, err
		}),
		Embedder: sessionEmbed(func(ctx context.Context, text string) ([]float64, error) {
			e := SessionEvent{Kind: "embed", InputSHA256: inputHash(text)}
			var out []float64
			err := run(ctx, e, 1_000_000, func(c context.Context) error { var x error; out, x = base.Embedder.Embed(c, text); return x })
			if err == nil {
				i := len(c.Bundle.Sessions[c.caseIndex].Trace.Events) - 1
				ev := c.Bundle.Sessions[c.caseIndex].Trace.Events[i]
				ev.Vector = out
				ev.State = "complete"
				if x := c.Update(i, ev); x != nil {
					return nil, x
				}
			}
			return out, err
		}),
		Judge: sessionJudge(func(ctx context.Context, r receipt.Receipt, ev receipt.Evidence, l receipt.Segment) (judge.Result, error) {
			e := SessionEvent{Kind: "judge", InputSHA256: judgmentKey(r, ev, l)}
			var out judge.Result
			err := run(ctx, e, 25_000_000, func(c context.Context) error { var x error; out, x = base.Judge.Judge(c, r, ev, l); return x })
			if err == nil {
				i := len(c.Bundle.Sessions[c.caseIndex].Trace.Events) - 1
				event := c.Bundle.Sessions[c.caseIndex].Trace.Events[i]
				event.Judgment = out
				event.State = "complete"
				if x := c.Update(i, event); x != nil {
					return judge.Result{}, x
				}
			}
			return out, err
		}),
	}
}

// ResumeAdapters replays previously persisted responses before allowing new
// attempts. If independent calls reorder such that a new call precedes a saved
// one, it stops rather than risk repeating a paid operation.
func ResumeAdapters(base SessionAdapters, c *Checkpoint) (SessionAdapters, func() error) {
	saved := append([]SessionEvent(nil), c.Bundle.Sessions[c.caseIndex].Trace.Events...)
	used := make([]bool, len(saved))
	live := CheckpointAdapters(base, c)
	find := func(ctx context.Context, kind, key string) (SessionEvent, bool, error) {
		for i, e := range saved {
			if !used[i] && e.Kind == kind && e.InputSHA256 == key {
				used[i] = true
				if e.State != "complete" || !e.Measured || e.Failed {
					return e, true, errors.New("unresolved saved attempt")
				}
				providerusage.Report(ctx, cost.Usage{Measured: true, Price: cost.Price(e.SpendNanoUSD)})
				return e, true, nil
			}
		}
		for _, v := range used {
			if !v {
				return SessionEvent{}, false, errors.New("saved event order cannot be safely resumed")
			}
		}
		return SessionEvent{}, false, nil
	}
	check := func() error {
		for _, v := range used {
			if !v {
				return errors.New("unused saved events")
			}
		}
		return nil
	}
	return SessionAdapters{Dimensions: base.Dimensions, Budget: base.Budget,
		Extractor: sessionExtract(func(ctx context.Context, s []receipt.Segment) (commands.Extraction, error) {
			e, ok, err := find(ctx, "extract", extractionKey(s))
			if err != nil {
				return commands.Extraction{}, err
			}
			if ok {
				return e.Extraction, nil
			}
			return live.Extractor.Extract(ctx, s)
		}),
		Sensitive: sessionSensitive(func(ctx context.Context, text string) (bool, error) {
			e, ok, err := find(ctx, "sensitive", inputHash(text))
			if err != nil {
				return false, err
			}
			if ok {
				return e.Sensitive, nil
			}
			return live.Sensitive.Sensitive(ctx, text)
		}),
		Embedder: sessionEmbed(func(ctx context.Context, text string) ([]float64, error) {
			e, ok, err := find(ctx, "embed", inputHash(text))
			if err != nil {
				return nil, err
			}
			if ok {
				return e.Vector, nil
			}
			return live.Embedder.Embed(ctx, text)
		}),
		Judge: sessionJudge(func(ctx context.Context, r receipt.Receipt, ev receipt.Evidence, l receipt.Segment) (judge.Result, error) {
			e, ok, err := find(ctx, "judge", judgmentKey(r, ev, l))
			if err != nil {
				return judge.Result{}, err
			}
			if ok {
				return e.Judgment, nil
			}
			return live.Judge.Judge(ctx, r, ev, l)
		}),
	}, check
}
