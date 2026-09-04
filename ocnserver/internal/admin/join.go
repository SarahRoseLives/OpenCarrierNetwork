package admin

import (
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/open-carrier-network/ocn/internal/store"
	qrcode "github.com/skip2/go-qrcode"
)

// handleJoinPage serves the public "get a number" page. It is intentionally
// NOT behind requireAuth: it is the self-service entry point an operator can
// expose (e.g. proxied through the registry website) to mint provisioning
// links for this exchange.
func (s *Server) handleJoinPage(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/join.html")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "join page unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// handleJoinMint issues a provisioning token with no reserved number, so the
// first free number is auto-assigned when the user claims it in the softphone.
func (s *Server) handleJoinMint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName string `json:"display_name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if len(req.DisplayName) > 60 {
		req.DisplayName = req.DisplayName[:60]
	}

	secret, err := newToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	ttl := 24 * time.Hour
	if err := s.store.NewProvisionToken(
		store.HashToken(secret), "", req.DisplayName, "web join", "web", ttl,
	); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create provisioning token")
		return
	}

	uri, host, err := s.provisionURI(r, secret, req.DisplayName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	png, err := qrcode.Encode(uri, qrcode.Medium, 320)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to render QR code")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"token":      secret,
		"url":        uri,
		"host":       host,
		"qr_data":    "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		"expires_at": time.Now().Add(ttl).Format(time.RFC3339),
	})
}
