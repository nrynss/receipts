package auth

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
)

func (s *Service) accountInfo(w http.ResponseWriter, r *http.Request) {
	a, err := s.Current(r)
	if err != nil {
		http.Error(w, "Sign in required", 401)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a)
}

func (s *Service) listDevices(w http.ResponseWriter, r *http.Request) {
	a, err := s.Current(r)
	if err != nil {
		http.Error(w, "Sign in required", 401)
		return
	}
	rows, err := s.db.Reader().QueryContext(r.Context(), `SELECT id,created_at FROM device_sessions WHERE account_id=? AND channel_id=? AND revoked_at IS NULL ORDER BY created_at`, a.ID, a.ChannelID)
	if err != nil {
		http.Error(w, "Unavailable", 500)
		return
	}
	defer rows.Close()
	type device struct {
		ID        string `json:"id"`
		CreatedAt int64  `json:"created_at"`
	}
	devices := []device{}
	for rows.Next() {
		var d device
		if rows.Scan(&d.ID, &d.CreatedAt) != nil {
			http.Error(w, "Unavailable", 500)
			return
		}
		devices = append(devices, d)
	}
	if rows.Err() != nil {
		http.Error(w, "Unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(devices)
}

func (s *Service) revokeDevice(w http.ResponseWriter, r *http.Request) {
	a, hash, err := s.authenticate(r)
	if err != nil || !s.validOrigin(r) || !s.validCSRF(r, hash) {
		http.Error(w, "Request refused", 403)
		return
	}
	owner := ChannelContext{AccountID: a.ID, ChannelID: a.ChannelID, BrowserSessionID: hex.EncodeToString(hash)}
	if err := s.RevokeDeviceCredential(r.Context(), owner, r.PathValue("id")); err != nil {
		http.Error(w, "Device unavailable", 404)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) deviceInfo(w http.ResponseWriter, r *http.Request) {
	_, _, cancel, unwatch, err := s.authenticateDevice(r)
	if err != nil {
		http.Error(w, "Pairing required", 401)
		return
	}
	defer cancel()
	defer unwatch()
	w.WriteHeader(http.StatusNoContent)
}
