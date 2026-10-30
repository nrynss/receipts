package evaluation

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

var ErrCheckpointBudget = errors.New("checkpoint budget exhausted")

// Checkpoint persists sanitized state before and after every paid attempt.
// A pending attempt or response-missing event always blocks automatic resume.
type Checkpoint struct {
	lockPath  string
	Path      string
	Bundle    SessionBundle
	caseIndex int
}

func OpenCheckpoint(path string, expected SessionBundle, resume bool) (*Checkpoint, error) {
	c := &Checkpoint{Path: path, caseIndex: -1}
	if path == "" || expected.Requested <= 0 || expected.BudgetNanoUSD <= 0 {
		return nil, errors.New("invalid checkpoint configuration")
	}
	lockPath := path + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, errors.New("checkpoint is locked; verify no collector is running before reconciliation")
	}
	if err = lock.Close(); err != nil {
		os.Remove(lockPath)
		return nil, err
	}
	c.lockPath = lockPath
	success := false
	defer func() {
		if !success {
			os.Remove(lockPath)
		}
	}()

	if resume {
		info, e := os.Lstat(path)
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("unsafe checkpoint file")
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &c.Bundle); e != nil {
			return nil, e
		}
		b := c.Bundle
		if b.CorpusKind != expected.CorpusKind || b.DatasetSHA256 != expected.DatasetSHA256 || b.Split != expected.Split || b.FreezeSHA256 != expected.FreezeSHA256 || b.JudgeModel != expected.JudgeModel || b.EmbeddingModel != expected.EmbeddingModel || b.Provenance != expected.Provenance || b.Requested != expected.Requested || b.BudgetNanoUSD != expected.BudgetNanoUSD || !reflect.DeepEqual(b.SourceHashes, expected.SourceHashes) {
			return nil, errors.New("checkpoint manifest mismatch")
		}
		if e = c.validate(); e != nil {
			return nil, e
		}
		success = true
		return c, nil
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	if e = f.Close(); e != nil {
		return nil, e
	}
	c.Bundle = expected
	c.Bundle.RunStatus = "incomplete"
	if e = c.Save(); e != nil {
		return nil, e
	}
	success = true
	return c, nil
}

// Close releases the exclusive collector lock after a graceful exit. A stale
// lock after a crash requires an operator to verify that no collector remains.
func (c *Checkpoint) Close() error {
	if c.lockPath == "" {
		return nil
	}
	p := c.lockPath
	c.lockPath = ""
	return os.Remove(p)
}
func (c *Checkpoint) validate() error {
	ids := map[string]bool{}
	incomplete := false
	for i, r := range c.Bundle.Sessions {
		if r.ID == "" || r.Trace.SessionID != r.ID || ids[r.ID] {
			return errors.New("invalid checkpoint cases")
		}
		ids[r.ID] = true
		if r.ErrorStage != "" {
			if r.ErrorStage != "in_progress" || incomplete || i != len(c.Bundle.Sessions)-1 {
				return errors.New("checkpoint requires reconciliation")
			}
			incomplete = true
		}
		for _, ev := range r.Trace.Events {
			if c.Bundle.Provenance == "live-provider-sanitized" {
				req, e := hex.DecodeString(ev.RequestSHA256)
				if e != nil || len(req) != 32 {
					return errors.New("invalid request digest")
				}
				input, e := hex.DecodeString(ev.InputSHA256)
				if e != nil || len(input) != 32 {
					return errors.New("invalid input digest")
				}
			}
			if ev.State != "complete" || !ev.Measured || ev.Failed || ev.SpendNanoUSD < 0 || ev.ReservationNanoUSD <= 0 || ev.SpendNanoUSD > ev.ReservationNanoUSD {
				return errors.New("unresolved provider attempt; reconcile before resuming")
			}
		}
	}
	if len(c.Bundle.Sessions) > c.Bundle.Requested {
		return errors.New("too many checkpoint cases")
	}
	spent, e := c.Spent()
	if e != nil {
		return e
	}
	if spent > c.Bundle.BudgetNanoUSD {
		return errors.New("checkpoint budget exceeded")
	}
	return nil
}
func (c *Checkpoint) Spent() (int64, error) {
	var n int64
	for _, r := range c.Bundle.Sessions {
		for _, ev := range r.Trace.Events {
			if ev.SpendNanoUSD < 0 || n > c.Bundle.BudgetNanoUSD-ev.SpendNanoUSD {
				return 0, errors.New("invalid checkpoint usage")
			}
			n += ev.SpendNanoUSD
		}
	}
	return n, nil
}
func (c *Checkpoint) Save() error {
	raw, e := json.Marshal(c.Bundle)
	if e != nil {
		return e
	}
	dir := filepath.Dir(c.Path)
	f, e := os.CreateTemp(dir, ".receipts-checkpoint-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = os.Rename(name, c.Path); e != nil {
		return e
	}
	d, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func (c *Checkpoint) StartCase(id string) (int, error) {
	if c.caseIndex != -1 {
		return 0, errors.New("case already active")
	}
	for i, r := range c.Bundle.Sessions {
		if r.ID == id {
			if r.ErrorStage != "in_progress" {
				return 0, errors.New("case already finished")
			}
			c.caseIndex = i
			return i, nil
		}
	}
	if len(c.Bundle.Sessions) >= c.Bundle.Requested {
		return 0, errors.New("case limit reached")
	}
	c.Bundle.Sessions = append(c.Bundle.Sessions, SessionRecordedCase{ID: id, Trace: SessionTrace{SessionID: id}, ErrorStage: "in_progress"})
	c.caseIndex = len(c.Bundle.Sessions) - 1
	if e := c.Save(); e != nil {
		return 0, e
	}
	return c.caseIndex, nil
}
func (c *Checkpoint) Begin(e SessionEvent, reserve int64) (int, error) {
	if c.caseIndex < 0 || reserve <= 0 {
		return 0, errors.New("no active case")
	}
	for _, r := range c.Bundle.Sessions {
		for _, prev := range r.Trace.Events {
			if prev.State != "complete" || !prev.Measured || prev.Failed {
				return 0, errors.New("unresolved prior attempt")
			}
		}
	}
	spent, err := c.Spent()
	if err != nil {
		return 0, err
	}
	if spent > c.Bundle.BudgetNanoUSD-reserve {
		return 0, ErrCheckpointBudget
	}
	e.State = "reserved"
	e.ReservationNanoUSD = reserve
	trace := &c.Bundle.Sessions[c.caseIndex].Trace
	trace.Events = append(trace.Events, e)
	idx := len(trace.Events) - 1
	if err = c.Save(); err != nil {
		return 0, err
	}
	return idx, nil
}
func (c *Checkpoint) Update(i int, e SessionEvent) error {
	if c.caseIndex < 0 || i < 0 || i >= len(c.Bundle.Sessions[c.caseIndex].Trace.Events) {
		return errors.New("invalid attempt")
	}
	old := c.Bundle.Sessions[c.caseIndex].Trace.Events[i]
	if old.Kind != e.Kind || old.InputSHA256 != e.InputSHA256 {
		return errors.New("attempt mismatch")
	}
	e.ReservationNanoUSD = old.ReservationNanoUSD
	c.Bundle.Sessions[c.caseIndex].Trace.Events[i] = e
	return c.Save()
}
func (c *Checkpoint) FinishCase(result SessionResult, elapsed int64) error {
	if c.caseIndex < 0 {
		return errors.New("no active case")
	}
	r := &c.Bundle.Sessions[c.caseIndex]
	r.Result = result
	r.LatencyMS += elapsed
	r.ErrorStage = ""
	c.caseIndex = -1
	return c.Save()
}
func (c *Checkpoint) Stop(stage string) error {
	if c.caseIndex >= 0 {
		r := &c.Bundle.Sessions[c.caseIndex]
		if stage != "deadline" {
			r.ErrorStage = stage
		}
	}
	c.Bundle.RunStatus = stage
	return c.Save()
}
func (c *Checkpoint) Complete() error {
	if c.caseIndex >= 0 || len(c.Bundle.Sessions) != c.Bundle.Requested {
		return fmt.Errorf("incomplete run")
	}
	c.Bundle.RunStatus = "complete"
	return c.Save()
}

// Remaining validates the fixed case sequence and omits completed cases.
func (c *Checkpoint) Remaining(cases []SessionCase) ([]SessionCase, error) {
	if len(cases) != c.Bundle.Requested || len(c.Bundle.Sessions) > len(cases) {
		return nil, errors.New("checkpoint case count mismatch")
	}
	for i, r := range c.Bundle.Sessions {
		if r.ID != cases[i].ID {
			return nil, errors.New("checkpoint case sequence mismatch")
		}
	}
	var pending []SessionCase
	for i, sc := range cases {
		if i < len(c.Bundle.Sessions) && c.Bundle.Sessions[i].ErrorStage == "" {
			continue
		}
		pending = append(pending, sc)
	}
	return pending, nil
}
