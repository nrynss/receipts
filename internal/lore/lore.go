// Package lore renders a bounded chat summary of the durable ledger.
package lore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"receipts/internal/ports"
	"receipts/internal/receipt"
)

type Ledger interface {
	LoreReceipts(context.Context, string) ([]receipt.Receipt, error)
}
type Service struct{ Ledger Ledger }

// Render counts every eligible current receipt once: admitted and upheld are
// broken; overturned, context changed and inconclusive are separate outcomes;
// undecided receipts remain open. The preview is bounded in UTF-8 bytes.
func (s Service) Render(ctx context.Context, channel string) (string, error) {
	if s.Ledger == nil || strings.TrimSpace(channel) == "" {
		return "", errors.New("lore: ledger and channel required")
	}
	rows, err := s.Ledger.LoreReceipts(ctx, channel)
	if err != nil {
		return "", err
	}
	var open, broken, overturned, contextChanged, inconclusive int
	for _, r := range rows {
		switch r.Verdict {
		case receipt.Undecided:
			open++
		case receipt.Admitted, receipt.Upheld:
			broken++
		case receipt.Overturned:
			overturned++
		case receipt.ContextChanged:
			contextChanged++
		case receipt.Inconclusive:
			inconclusive++
		default:
			return "", fmt.Errorf("lore: unknown verdict %q", r.Verdict)
		}
	}
	result := fmt.Sprintf("Lore: %d total | %d open, %d broken, %d overturned, %d context changed, %d inconclusive.", len(rows), open, broken, overturned, contextChanged, inconclusive)
	for _, r := range rows {
		claim := strings.Join(strings.Fields(r.Claim), " ")
		prefix := " "
		if !strings.Contains(result, " Claims:") {
			prefix = " Claims: "
		} else {
			prefix = "; "
		}
		remaining := 450 - len(result) - len(prefix)
		if remaining <= 0 {
			break
		}
		if len(claim) > remaining {
			claim = claim[:remaining]
			for !utf8.ValidString(claim) {
				claim = claim[:len(claim)-1]
			}
			claim = strings.TrimSpace(claim)
			if claim == "" {
				break
			}
			result += prefix + claim
			break
		}
		result += prefix + claim
	}
	return result, nil
}

// Handle accepts a trusted channel-bound chat command. Delivery remains the
// responsibility of the verified chat adapter.
func (s Service) Handle(ctx context.Context, channel string, cmd ports.ChatCommand) (string, error) {
	if cmd.Command != "!lore" || cmd.ChannelID != channel || strings.TrimSpace(cmd.Arguments) != "" {
		return "", errors.New("lore: invalid command")
	}
	return s.Render(ctx, channel)
}
