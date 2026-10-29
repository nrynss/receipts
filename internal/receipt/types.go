// Package receipt defines the durable ledger records shared by the application
// and its adapters. It contains no provider-specific behavior.
package receipt

import "time"

type Validity string

const (
	Valid   Validity = "valid"
	Void    Validity = "void"
	Expired Validity = "expired"
)

type ClipStatus string

const (
	ClipPending     ClipStatus = "pending"
	ClipReady       ClipStatus = "ready"
	ClipUnavailable ClipStatus = "unavailable"
)

type Verdict string

const (
	Undecided      Verdict = "undecided"
	Admitted       Verdict = "admitted"
	Upheld         Verdict = "upheld"
	Overturned     Verdict = "overturned"
	ContextChanged Verdict = "context_changed"
	Inconclusive   Verdict = "inconclusive"
)

type DetectionOutcome string

const (
	NeedsReview DetectionOutcome = "review"
	Stamp       DetectionOutcome = "stamp"
	Dismissed   DetectionOutcome = "dismissed"
)

type VoteChoice string

const (
	VoteUphold   VoteChoice = "uphold"
	VoteOverturn VoteChoice = "overturn"
)

type Channel struct {
	ID, PlatformID, Login string
	CreatedAt             time.Time
}
type Session struct {
	ID, ChannelID string
	StartedAt     time.Time
	EndedAt       *time.Time
	State         string
}
type Segment struct {
	ID, SessionID                string
	Start, End                   time.Duration
	Text, SpeakerID, Attribution string
	CreatedAt                    time.Time
}
type Evidence struct {
	ID, ReceiptID, SourceID, Text string
	Start, End                    time.Duration
	Attribution                   string
}
type Receipt struct {
	ID, ChannelID, SessionID string
	CreatedAt                time.Time
	ExpiresAt                *time.Time
	Claim, ClippedBy         string
	Validity                 Validity
	Sensitive                bool
	ClipStatus               ClipStatus
	ClipID, ClipURL          string
	Verdict                  Verdict
	Version                  int
}
type Detection struct {
	ID, ChannelID, ReceiptID, SessionID, SourceID, EvidenceText, Attribution string
	Start, End                                                               time.Duration
	Outcome                                                                  DetectionOutcome
	CreatedAt                                                                time.Time
}
type Review struct {
	ID, DetectionID, State string
	CreatedAt              time.Time
	DecidedAt              *time.Time
}
type Appeal struct {
	ID, ReceiptID, State string
	OpenedAt, ClosesAt   time.Time
	Decision             string
	DecidedAt            *time.Time
}
type Vote struct {
	ID, AppealID, VoterID string
	Choice                VoteChoice
	CreatedAt             time.Time
}
type VerdictRecord struct {
	ID, ReceiptID, AppealID, DecisionKey string
	Outcome                              Verdict
	Note                                 string
	CreatedAt                            time.Time
}

type EvidenceInput struct {
	SourceID, Text, Attribution string
	Start, End                  time.Duration
}
type DetectionInput struct {
	ChannelID, ReceiptID string
	Evidence             EvidenceInput
	Outcome              DetectionOutcome
}
type ReceiptInput struct {
	ChannelID, SessionID, Claim, ClippedBy string
	CreatedAt                              time.Time
	ExpiresAt                              *time.Time
	Sensitive                              bool
	Evidence                               []EvidenceInput
}
