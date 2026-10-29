package commands

import (
	"context"
	"errors"

	"receipts/internal/ports"
)

// ChatSink adapts recorded chat triggers to the exact production Service path.
type ChatSink struct {
	Service   *Service
	Authority Authority
	OnResult  func(Result)
}

func (s ChatSink) AcceptChatCommand(ctx context.Context, cmd ports.ChatCommand) error {
	if s.Service == nil {
		return errors.New("receipt command service is required")
	}
	result, err := s.Service.Handle(ctx, s.Authority, cmd)
	if err != nil {
		return err
	}
	if s.OnResult != nil {
		s.OnResult(result)
	}
	return nil
}
