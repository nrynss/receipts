package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const pairCookie = "receipts_pair"
const pairLifetime = 2 * time.Minute
const pairAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
const pairCodeLength = 4
const maxPendingPairs = 32

type pairLimit struct {
	count int
	until time.Time
}

type pairRequest struct {
	browserHash string
	accountID   string
	expires     time.Time
	credential  string
}

// startPair binds a temporary request to the OBS browser, not its URL.
func (s *Service) startPair(w http.ResponseWriter, r *http.Request) {
	if !s.validOrigin(r) {
		http.Error(w, "Request refused", http.StatusForbidden)
		return
	}
	browser, err := randomToken()
	if err != nil {
		http.Error(w, "Unavailable", 500)
		return
	}
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	now := s.now().UTC()
	for k, p := range s.pairs {
		if !p.expires.After(now) {
			delete(s.pairs, k)
		}
	}
	if len(s.pairs) >= maxPendingPairs {
		http.Error(w, "Try again later", http.StatusTooManyRequests)
		return
	}
	code, err := uniquePairCode(s.pairs, randomPairCode)
	if err != nil {
		http.Error(w, "Try again", http.StatusServiceUnavailable)
		return
	}

	s.pairs[code] = &pairRequest{browserHash: hex.EncodeToString(digest(browser)), expires: now.Add(pairLifetime)}
	http.SetCookie(w, s.cookie(pairCookie, browser, pairLifetime, true))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
}

func (s *Service) confirmPair(w http.ResponseWriter, r *http.Request) {
	account, hash, err := s.authenticate(r)
	if err != nil || !s.validOrigin(r) || !s.validCSRF(r, hash) {
		http.Error(w, "Request refused", http.StatusForbidden)
		return
	}
	var input struct {
		Code string `json:"code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		http.Error(w, "Invalid code", 400)
		return
	}
	input.Code = strings.ToUpper(strings.TrimSpace(input.Code))
	if len(input.Code) != pairCodeLength || strings.Trim(input.Code, pairAlphabet) != "" {
		http.Error(w, "Invalid code", http.StatusBadRequest)
		return
	}
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	now := s.now().UTC()
	key := account.ID
	for k, limit := range s.pairAttempts {
		if !limit.until.After(now) {
			delete(s.pairAttempts, k)
		}
	}
	limit := s.pairAttempts[key]
	global := s.pairAttempts[""]
	if limit.count >= 5 || global.count >= 20 {
		http.Error(w, "Try again later", http.StatusTooManyRequests)
		return
	}
	if limit.count == 0 {
		limit.until = now.Add(time.Minute)
	}
	limit.count++
	s.pairAttempts[key] = limit
	if global.count == 0 {
		global.until = now.Add(time.Minute)
	}
	global.count++
	s.pairAttempts[""] = global
	p := s.pairs[input.Code]
	if p == nil || !p.expires.After(s.now().UTC()) || p.credential != "" {
		http.Error(w, "Code expired or unavailable", 404)
		return
	}
	owner := ChannelContext{AccountID: account.ID, ChannelID: account.ChannelID, BrowserSessionID: hex.EncodeToString(hash)}
	_, credential, err := s.IssueDeviceCredential(r.Context(), owner)
	if err != nil {
		http.Error(w, "Pairing unavailable", 500)
		return
	}
	p.credential = credential
	p.accountID = account.ID
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) pairStatus(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(pairCookie)
	if err != nil {
		http.Error(w, "Request refused", 403)
		return
	}
	hash := hex.EncodeToString(digest(cookie.Value))
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	for code, p := range s.pairs {
		if p.browserHash != hash {
			continue
		}
		if !p.expires.After(s.now().UTC()) {
			delete(s.pairs, code)
			http.Error(w, "Code expired", 410)
			return
		}
		if p.credential == "" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		s.deviceMu.Lock()
		var active int
		err := s.db.Reader().QueryRowContext(r.Context(), `SELECT count(*) FROM device_sessions d JOIN accounts a ON a.id=d.account_id WHERE d.token_hash=? AND d.revoked_at IS NULL AND a.status='connected'`, digest(p.credential)).Scan(&active)
		if err != nil || active != 1 {
			s.deviceMu.Unlock()
			delete(s.pairs, code)
			http.Error(w, "Pairing unavailable", http.StatusGone)
			return
		}
		http.SetCookie(w, s.cookie(DeviceCookie, p.credential, 365*24*time.Hour, true))
		s.deviceMu.Unlock()
		http.SetCookie(w, s.expiredCookie(pairCookie, true))
		delete(s.pairs, code)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Error(w, "Code expired", 410)
}

// The 32-symbol alphabet maps five random bits per character without bias.
func randomPairCode() (string, error) {
	var raw [pairCodeLength]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	for i := range raw {
		raw[i] = pairAlphabet[raw[i]&31]
	}
	return string(raw[:]), nil
}

func uniquePairCode(pairs map[string]*pairRequest, generate func() (string, error)) (string, error) {
	for attempt := 0; attempt < 32; attempt++ {
		code, err := generate()
		if err != nil {
			return "", err
		}
		if _, exists := pairs[code]; !exists {
			return code, nil
		}
	}
	return "", errors.New("auth: pairing code allocation exhausted")
}
