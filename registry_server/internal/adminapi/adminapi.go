// Package adminapi serves the registry's web admin (login, exchanges, and
// 800/900 service management) under /admin on the registry's HTTPS site.
package adminapi

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/open-carrier-network/ocn/registry/internal/store"
)

//go:embed webadmin
var webFS embed.FS

// Server provides the admin HTTP handlers backed by the registry store.
type Server struct {
	store *store.Store
}

func New(st *store.Store) *Server { return &Server{store: st} }

// Handler returns the admin mux (mounted by the caller under /admin).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	sub, _ := fs.Sub(webFS, "webadmin")
	mux.Handle("GET /admin/", http.StripPrefix("/admin/", http.FileServer(http.FS(sub))))

	mux.HandleFunc("POST /admin/api/login", s.handleLogin)
	mux.HandleFunc("GET /admin/api/me", s.require(s.handleMe))
	mux.HandleFunc("POST /admin/api/logout", s.require(s.handleLogout))
	mux.HandleFunc("POST /admin/api/password", s.require(s.handlePassword))

	mux.HandleFunc("GET /admin/api/exchanges", s.require(s.handleExchanges))
	mux.HandleFunc("GET /admin/api/services", s.require(s.handleServices))
	mux.HandleFunc("POST /admin/api/services", s.require(s.handleClaimService))
	mux.HandleFunc("POST /admin/api/services/{number}/status", s.require(s.handleServiceStatus))
	mux.HandleFunc("DELETE /admin/api/services/{number}", s.require(s.handleDeleteService))
	return mux
}

type ctxKey int

const ctxUser ctxKey = 0

func (s *Server) require(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.store.SessionUsername(bearer(r))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxUser, u)))
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

func userOf(r *http.Request) string {
	if v, ok := r.Context().Value(ctxUser).(string); ok {
		return v
	}
	return ""
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// ---- auth ----

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	must, err := s.store.VerifyAdminLogin(req.Username, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}
	token, err := s.store.CreateSession(req.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"token": token, "must_change": must})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"username": userOf(r)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.store.DeleteSession(bearer(r))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if err := s.store.ChangeAdminPassword(userOf(r), req.OldPassword, req.NewPassword); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- data ----

func (s *Server) handleExchanges(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListServers(500)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]map[string]interface{}, 0, len(list))
	for _, sv := range list {
		out = append(out, map[string]interface{}{
			"area_code":      sv.AreaCode,
			"name":           sv.Name,
			"server_address": sv.ServerAddr,
			"status":         sv.Status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"servers": out})
}

func svToMap(sn *store.ServiceNumber) map[string]interface{} {
	return map[string]interface{}{
		"full_number": sn.FullNumber,
		"vanity":      sn.Vanity,
		"name":        sn.Name,
		"description": sn.Description,
		"host_area":   sn.HostArea,
		"status":      sn.Status,
	}
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListServiceNumbers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]map[string]interface{}, 0, len(list))
	for _, sn := range list {
		out = append(out, svToMap(sn))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"services": out})
}

func (s *Server) handleClaimService(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FullNumber  string `json:"full_number"`
		Vanity      string `json:"vanity"`
		Name        string `json:"name"`
		Description string `json:"description"`
		HostArea    string `json:"host_area"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if req.Name == "" || req.HostArea == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and host_area are required"})
		return
	}
	err := s.store.ClaimServiceNumber(&store.ServiceNumber{
		FullNumber:  req.FullNumber,
		Vanity:      req.Vanity,
		Name:        req.Name,
		Description: req.Description,
		HostArea:    req.HostArea,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (s *Server) handleServiceStatus(w http.ResponseWriter, r *http.Request) {
	num := r.PathValue("number")
	var req struct {
		Status string `json:"status"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if err := s.store.SetServiceStatus(num, req.Status); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteServiceNumber(r.PathValue("number")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
