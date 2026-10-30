package auth

import (
	"context"
	"database/sql"
	"errors"
)

// ChannelAccount is non-secret Twitch authorization metadata for a validated
// runtime authority. It deliberately omits encrypted and plaintext credentials.
type ChannelAccount struct {
	AccountID         string
	ClientID          string
	SubjectID         string
	ChannelPlatformID string
	Scopes            []string
}

// AccountForChannel verifies the account/channel relationship against storage
// before returning metadata needed to bind Helix and EventSub operations.
func (s *Service) AccountForChannel(ctx context.Context, authority ChannelContext) (ChannelAccount, error) {
	if authority.AccountID == "" || authority.ChannelID == "" {
		return ChannelAccount{}, ErrNoChannelContext
	}
	var got ChannelAccount
	var rawScopes string
	err := s.db.Reader().QueryRowContext(ctx, `SELECT a.id,a.provider_subject,c.platform_id,a.scopes FROM accounts a JOIN channels c ON c.id=a.channel_id WHERE a.id=? AND a.channel_id=? AND a.provider='twitch' AND a.status='connected'`, authority.AccountID, authority.ChannelID).Scan(&got.AccountID, &got.SubjectID, &got.ChannelPlatformID, &rawScopes)
	if errors.Is(err, sql.ErrNoRows) {
		return ChannelAccount{}, ErrReconnect
	}
	if err != nil {
		return ChannelAccount{}, errors.New("auth: channel authorization unavailable")
	}
	if got.AccountID != authority.AccountID || got.SubjectID == "" || got.SubjectID != got.ChannelPlatformID {
		return ChannelAccount{}, errors.Join(ErrReconnect, ErrForbidden)
	}
	got.ClientID = s.clientID
	got.Scopes = decodeScopes(rawScopes)
	return got, nil
}

// DisconnectProviderAuthorization revokes Twitch's current access token using
// the protected credential internally, then disconnects the local account and
// cancels its sessions/runtime. It never returns credential material.
func (s *Service) DisconnectProviderAuthorization(ctx context.Context, accountID string) error {
	subject, err := s.accountSubject(ctx, accountID)
	if err != nil {
		return err
	}
	lock := s.accountLock(subject)
	lock.Lock()
	defer lock.Unlock()
	row, tokens, loadErr := s.loadAccountTokens(ctx, accountID)
	var revokeErr error
	if loadErr == nil && row.Status == "connected" {
		if revoker, ok := s.provider.(Revoker); ok {
			revokeCtx, cancel := s.detachedOperationContext(ctx)
			revokeErr = revoker.Revoke(revokeCtx, tokens.Access)
			cancel()
		} else {
			revokeErr = errors.New("auth: provider revoke unavailable")
		}
	}
	stateErr := s.markReconnectLocked(ctx, accountID)
	if stateErr != nil {
		return stateErr
	}
	if loadErr != nil || revokeErr != nil {
		return errors.New("auth: provider disconnect could not be confirmed")
	}
	return nil
}
