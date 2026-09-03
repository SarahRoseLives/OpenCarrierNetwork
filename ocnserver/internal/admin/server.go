// Package admin provides the operator web panel for an OCN server. It is an
// embedded single-page app served on its own port (default 8080). Operators
// authenticate with an account stored in the OCN SQLite database and use it to
// provision phone lines via QR codes / ocn_ksim:// URLs, and to manage lines.
package admin

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/open-carrier-network/ocn/internal/numbers"
	"github.com/open-carrier-network/ocn/internal/store"
	qrcode "github.com/skip2/go-qrcode"
)

//go:embed static
var staticFS embed.FS

type Options struct {
	Store         *store.Store
	Online        func() map[string]bool
	SignalingPort int
	PublicAddress string // optional public host:port of the signaling server
	AreaCode      string
	ServerName    string
	ServerKeyPath string // path to this server's kSIM key (registry signing)

	// Area returns the live server area code (updates on hot federation).
	Area func() string
	// OnFederated is called after a successful registration so the running
	// server can hot-join (attach registry, set area code, enable push)
	// without a restart. Return an error to signal live activation failed.
	OnFederated func(fs *store.FederationSettings, area string) error
}

type Server struct {
	store *store.Store
	opts  Options
	mux   *http.ServeMux
}

func New(opts Options) *Server {
	s := &Server{store: opts.Store, opts: opts, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

type ctxKey int

const ctxUsername ctxKey = 0

func (s *Server) routes() {
	// Static SPA
	staticSub, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /", http.FileServer(http.FS(staticSub)))

	// Auth
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/logout", s.requireAuth(s.handleLogout))
	s.mux.HandleFunc("GET /api/me", s.requireAuth(s.handleMe))
	s.mux.HandleFunc("POST /api/password", s.requireAuth(s.handlePassword))

	// Data
	s.mux.HandleFunc("GET /api/stats", s.requireAuth(s.handleStats))
	s.mux.HandleFunc("GET /api/lines", s.requireAuth(s.handleListLines))
	s.mux.HandleFunc("PUT /api/lines/{number}", s.requireAuth(s.handleUpdateLine))
	s.mux.HandleFunc("DELETE /api/lines/{number}", s.requireAuth(s.handleDeleteLine))
	s.mux.HandleFunc("GET /api/numbers/free", s.requireAuth(s.handleFreeNumbers))

	// Provisioning
	s.mux.HandleFunc("GET /api/provisions", s.requireAuth(s.handleListProvisions))
	s.mux.HandleFunc("POST /api/provisions", s.requireAuth(s.handleCreateProvision))
	s.mux.HandleFunc("POST /api/provisions/{id}/revoke", s.requireAuth(s.handleRevokeProvision))

	// Federation
	s.mux.HandleFunc("GET /api/federation/status", s.requireAuth(s.handleFederationStatus))
	s.mux.HandleFunc("POST /api/federation/register", s.requireAuth(s.handleFederationRegister))
	s.mux.HandleFunc("POST /api/federation/clear", s.requireAuth(s.handleFederationClear))
}

// ---- helpers ----

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		username, err := s.store.SessionUsername(token)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUsername, username)
		next(w, r.WithContext(ctx))
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

func usernameOf(r *http.Request) string {
	if v, ok := r.Context().Value(ctxUsername).(string); ok {
		return v
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// ---- auth handlers ----

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	mustChange, err := s.store.VerifyAdminLogin(req.Username, req.Password)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	token, err := s.store.CreateSession(req.Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":       token,
		"username":    req.Username,
		"must_change": mustChange,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.store.DeleteSession(bearerToken(r))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := usernameOf(r)
	must, _ := s.store.AdminMustChange(user)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"username":    user,
		"must_change": must,
	})
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := s.store.ChangeAdminPassword(usernameOf(r), req.OldPassword, req.NewPassword); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- dashboard / lines ----

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	lines, err := s.store.LinesTotal()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	free, err := s.store.FreeNumberEstimate()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	issued, used, err := s.store.ProvisionTokenCounts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	online := 0
	if s.opts.Online != nil {
		online = len(s.opts.Online())
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"lines_total":   lines,
		"lines_online":  online,
		"free_estimate": free,
		"tokens_issued": issued,
		"tokens_used":   used,
		"server_name":   s.opts.ServerName,
		"area_code":     s.opts.AreaCode,
	})
}

type lineDTO struct {
	Number        string `json:"number"`
	DisplayNumber string `json:"display_number"`
	DisplayName   string `json:"display_name"`
	Online        bool   `json:"online"`
	FCM           bool   `json:"fcm"`
	RegisteredAt  int64  `json:"registered_at"`
	LastSeen      int64  `json:"last_seen"`
}

func (s *Server) handleListLines(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	users, total, err := s.store.ListLines(search, offset, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var online map[string]bool
	if s.opts.Online != nil {
		online = s.opts.Online()
	}

	out := make([]lineDTO, 0, len(users))
	for _, u := range users {
		out = append(out, lineDTO{
			Number:        u.Number,
			DisplayNumber: numbers.FormatNumber(u.AreaCode, u.Number),
			DisplayName:   u.DisplayName,
			Online:        online[u.Number],
			FCM:           u.FCMToken != "",
			RegisteredAt:  u.RegisteredAt.Unix(),
			LastSeen:      u.LastSeen.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"lines": out, "total": total})
}

func (s *Server) handleUpdateLine(w http.ResponseWriter, r *http.Request) {
	number := r.PathValue("number")
	var req struct {
		DisplayName *string `json:"display_name"`
		Number      *string `json:"number"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := s.store.UpdateLine(number, req.DisplayName, req.Number); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteLine(w http.ResponseWriter, r *http.Request) {
	number := r.PathValue("number")
	if err := s.store.DeleteLineByNumber(number); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("Admin %s released line %s", usernameOf(r), number)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleFreeNumbers(w http.ResponseWriter, r *http.Request) {
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count <= 0 || count > 50 {
		count = 12
	}
	nums, err := s.store.RandomFreeNumbers(count)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"numbers": nums})
}

// ---- provisioning ----

func (s *Server) handleListProvisions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	tokens, err := s.store.ListProvisionTokens(limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"tokens": tokens})
}

func (s *Server) handleCreateProvision(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Number      string `json:"number"`
		DisplayName string `json:"display_name"`
		Notes       string `json:"notes"`
		ValidHours  int    `json:"valid_hours"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if req.ValidHours <= 0 {
		req.ValidHours = 24
	}
	if req.ValidHours > 24*30 {
		req.ValidHours = 24 * 30
	}

	number := strings.TrimSpace(req.Number)
	if number != "" {
		ok, err := s.store.NumberAvailable(number)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			writeErr(w, http.StatusConflict, "that number is already in use or reserved")
			return
		}
	}

	secret, err := newToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ttl := time.Duration(req.ValidHours) * time.Hour
	if err := s.store.NewProvisionToken(
		store.HashToken(secret), number, req.DisplayName, req.Notes, usernameOf(r), ttl,
	); err != nil {
		if err == store.ErrNumberTaken {
			writeErr(w, http.StatusConflict, "that number is already in use or reserved")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	expires := time.Now().Add(ttl)
	uri, host, err := s.provisionURI(r, secret, req.DisplayName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	png, err := qrcode.Encode(uri, qrcode.Medium, 280)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to render QR code")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"token":      secret,
		"url":        uri,
		"host":       host,
		"qr_data":    "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		"expires_at": expires.Format(time.RFC3339),
		"number":     number,
	})
}

func (s *Server) handleRevokeProvision(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.RevokeProvisionToken(id); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("Admin %s revoked provision token %s", usernameOf(r), id[:min(8, len(id))])
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// provisionURI builds the ocn_ksim:// deep link that the QR encodes and the
// phone parses.
func (s *Server) provisionURI(r *http.Request, token, name string) (string, string, error) {
	wsAddr := strings.TrimSpace(s.opts.PublicAddress)
	if wsAddr == "" {
		host := r.Host
		if i := strings.LastIndexByte(host, ':'); i >= 0 {
			host = host[:i]
		}
		if host == "" {
			host = "localhost"
		}
		port := s.opts.SignalingPort
		if port <= 0 {
			port = 9100
		}
		wsAddr = fmt.Sprintf("%s:%d", host, port)
	}

	scheme := "ws"
	if r.TLS != nil {
		scheme = "wss"
	}
	server := fmt.Sprintf("%s://%s/ws", scheme, wsAddr)

	q := url.Values{}
	q.Set("server", server)
	q.Set("token", token)
	if name != "" {
		q.Set("name", name)
	}
	host := wsAddr
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	// Scheme has no underscore: URI schemes may only contain letters, digits,
	// '+', '-' and '.', so "ocn_ksim://" would not parse. We use ocnksim://.
	return fmt.Sprintf("ocnksim://%s/?%s", host, q.Encode()), host, nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
