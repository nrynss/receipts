// Package commands implements chat-triggered receipt capture. The chat message
// only requests work; claim text and evidence always come from transcript data.
package commands

import (
	"context"
	"errors"
	"strings"
	"time"

	"receipts/internal/ports"
	"receipts/internal/receipt"
)

var (
	ErrUnavailable = errors.New("receipt capture unavailable")
	ErrDuplicate   = receipt.ErrCommandDuplicate
	ErrThrottled   = receipt.ErrCommandThrottled
)

type Authority struct{ ChannelID, SessionID string }
type Extraction struct {
	Claim     string
	SourceIDs []string
	Sensitive bool
}

// Extractor is an application-owned extraction port. Production model calls
// are wired later; fixture implementations return recorded responses.
type Extractor interface {
	Extract(context.Context, []receipt.Segment) (Extraction, error)
}
type SensitiveClassifier interface {
	Sensitive(context.Context, string) (bool, error)
}
type ClipQueue interface {
	Enqueue(context.Context, string, string, time.Time) error
}
type Ledger interface {
	Transcript(context.Context, string) ([]receipt.Segment, error)
	BeginReceiptCommand(context.Context, string, string, string, string, time.Time) (string, error)
	FinishReceiptCommand(context.Context, string, string, string) error
	CaptureReceipt(context.Context, receipt.CaptureInput) (receipt.Receipt, error)
}
type ReplyCode string

const (
	ReplyCaptured              ReplyCode = "captured"
	ReplyNoClaim               ReplyCode = "no_eligible_claim"
	ReplyTranscriptUnavailable ReplyCode = "transcript_unavailable"
	ReplySuppressed            ReplyCode = "suppressed"
	ReplyDuplicate             ReplyCode = "duplicate"
	ReplyThrottled             ReplyCode = "throttled"
)

type Result struct {
	Code    ReplyCode
	Receipt *receipt.Receipt
}
type Service struct {
	Ledger    Ledger
	Extractor Extractor
	Sensitive SensitiveClassifier
	Clips     ClipQueue
	Now       func() time.Time
	Lookback  time.Duration
}

// Handle accepts only the receipt command. Authority comes from the trusted
// runtime boundary; command.ChannelID and SessionID are deliberately ignored.
func (s *Service) Handle(ctx context.Context, authority Authority, cmd ports.ChatCommand) (Result, error) {
	if cmd.Command != "!receipt" || authority.ChannelID == "" || authority.SessionID == "" || cmd.ID == "" || cmd.UserID == "" {
		return Result{}, errors.New("invalid receipt command")
	}
	if s.Ledger == nil || s.Extractor == nil || s.Sensitive == nil {
		return Result{Code: ReplyTranscriptUnavailable}, ErrUnavailable
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	window := s.Lookback
	if window <= 0 {
		window = 30 * time.Second
	}
	state, err := s.Ledger.BeginReceiptCommand(ctx, authority.ChannelID, authority.SessionID, cmd.ID, cmd.UserID, cmd.ReceivedAt)
	if err != nil {
		return Result{Code: ReplyTranscriptUnavailable}, err
	}
	if state == "duplicate" {
		return Result{Code: ReplyDuplicate}, nil
	}
	if state == "throttled" {
		return Result{Code: ReplyThrottled}, nil
	}
	finish := func(status string) { _ = s.Ledger.FinishReceiptCommand(ctx, authority.ChannelID, cmd.ID, status) }
	segments, err := s.Ledger.Transcript(ctx, authority.SessionID)
	if err != nil {
		finish("unavailable")
		return Result{Code: ReplyTranscriptUnavailable}, ErrUnavailable
	}
	cutoff := time.Duration(0)
	latestEnd := time.Duration(0)
	for _, seg := range segments {
		if seg.End > latestEnd {
			latestEnd = seg.End
		}
	}
	cutoff = latestEnd - window
	bounded := make([]receipt.Segment, 0, len(segments))
	for _, seg := range segments {
		if seg.SessionID == authority.SessionID && seg.End >= cutoff && seg.Attribution == string(ports.AttributionBroadcaster) {
			bounded = append(bounded, seg)
		}
	}
	if len(bounded) == 0 {
		finish("no_claim")
		return Result{Code: ReplyNoClaim}, nil
	}
	extracted, err := s.Extractor.Extract(ctx, bounded)
	if err != nil {
		finish("unavailable")
		return Result{Code: ReplyTranscriptUnavailable}, ErrUnavailable
	}
	claim := strings.TrimSpace(extracted.Claim)
	if claim == "" || len(extracted.SourceIDs) == 0 {
		finish("no_claim")
		return Result{Code: ReplyNoClaim}, nil
	}
	sensitive := extracted.Sensitive
	classified, e := s.Sensitive.Sensitive(ctx, claim)
	if e != nil {
		finish("unavailable")
		return Result{Code: ReplyTranscriptUnavailable}, ErrUnavailable
	}
	sensitive = sensitive || classified
	if sensitive {
		finish("suppressed")
		return Result{Code: ReplySuppressed}, nil
	}
	byID := make(map[string]receipt.Segment, len(bounded))
	for _, seg := range bounded {
		byID[seg.ID] = seg
	}
	evidence := make([]receipt.EvidenceInput, 0, len(extracted.SourceIDs))
	seen := map[string]bool{}
	for _, id := range extracted.SourceIDs {
		seg, ok := byID[id]
		if !ok || seen[id] {
			finish("no_claim")
			return Result{Code: ReplyNoClaim}, nil
		}
		seen[id] = true
		evidence = append(evidence, receipt.EvidenceInput{SourceID: seg.ID, Text: seg.Text, Start: seg.Start, End: seg.End, Attribution: seg.Attribution})
	}
	captured, err := s.Ledger.CaptureReceipt(ctx, receipt.CaptureInput{CommandID: cmd.ID, ChannelID: authority.ChannelID, SessionID: authority.SessionID, UserID: cmd.UserID, Login: cmd.Login, Claim: claim, CreatedAt: now, Window: window, Evidence: evidence})
	if errors.Is(err, ErrDuplicate) {
		return Result{Code: ReplyDuplicate}, nil
	}
	if errors.Is(err, ErrThrottled) {
		return Result{Code: ReplyThrottled}, nil
	}
	if err != nil {
		return Result{}, err
	}
	// Clip work is independent and best-effort. Failure leaves the text receipt.
	if s.Clips != nil {
		_ = s.Clips.Enqueue(ctx, authority.ChannelID, captured.ID, now)
	}
	return Result{Code: ReplyCaptured, Receipt: &captured}, nil
}
