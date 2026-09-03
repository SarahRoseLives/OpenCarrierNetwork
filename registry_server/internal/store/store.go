package store

import (
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ServerInfo is a registered OCN server / exchange.
type ServerInfo struct {
	AreaCode     string
	Name         string
	Description  string
	ServerAddr   string // gRPC endpoint
	PublicKey    ed25519.PublicKey
	RegisteredAt time.Time
	Status       string // ACTIVE | SUSPENDED
}

var (
	ErrAreaInvalid    = errors.New("area code must be a 3-digit number in 200-999 (800 and 900 are reserved)")
	ErrAreaTaken      = errors.New("area code already in use")
	ErrNoFreeAreas    = errors.New("no free area codes")
	ErrServerNotFound = errors.New("server not found")
	ErrBadSignature   = errors.New("invalid signature")
	ErrBadTimestamp   = errors.New("timestamp out of range")
)

const (
	reservedA  = "800"
	reservedB  = "900"
	authWindow = 5 * time.Minute
)

// Store persists OCN server registrations.
type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS ocn_servers (
		area_code TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		server_address TEXT NOT NULL,
		public_key BLOB NOT NULL,
		registered_at INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'ACTIVE'
	)`)
	return err
}

// AllowedArea reports whether a 3-digit code may be assigned.
func AllowedArea(area string) bool {
	if len(area) != 3 {
		return false
	}
	if area == reservedA || area == reservedB {
		return false
	}
	for _, c := range area {
		if c < '0' || c > '9' {
			return false
		}
	}
	n := (int(area[0]-'0')*10+int(area[1]-'0'))*10 + int(area[2]-'0')
	return n >= 200 && n <= 999
}

// RegisterServer reserves an area code (requested, or auto-assigned) for a
// server and stores its details. Returns the assigned code.
func (s *Store) RegisterServer(info *ServerInfo) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	code := strings.TrimSpace(info.AreaCode)
	if code == "" {
		code, err = s.pickFree(tx)
		if err != nil {
			return "", err
		}
	} else if !AllowedArea(code) {
		return "", ErrAreaInvalid
	} else if exists, _ := s.exists(tx, code); exists {
		// Idempotent re-registration: if the same server (same key) re-joins,
		// keep its area code and just refresh its details.
		var storedKey []byte
		if err := tx.QueryRow(`SELECT public_key FROM ocn_servers WHERE area_code = ?`, code).Scan(&storedKey); err == nil &&
			ed25519.PublicKey(storedKey).Equal(info.PublicKey) {
			if _, err := tx.Exec(`UPDATE ocn_servers
				SET name = ?, description = ?, server_address = ?, status = 'ACTIVE' WHERE area_code = ?`,
				info.Name, info.Description, info.ServerAddr, code); err != nil {
				return "", err
			}
			if err := tx.Commit(); err != nil {
				return "", err
			}
			return code, nil
		}
		return "", ErrAreaTaken
	}

	_, err = tx.Exec(`INSERT INTO ocn_servers
		(area_code, name, description, server_address, public_key, registered_at, status)
		VALUES (?, ?, ?, ?, ?, ?, 'ACTIVE')`,
		code, info.Name, info.Description, info.ServerAddr, []byte(info.PublicKey), time.Now().Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return "", ErrAreaTaken
		}
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return code, nil
}

func (s *Store) exists(tx *sql.Tx, code string) (bool, error) {
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM ocn_servers WHERE area_code = ?`, code).Scan(&n)
	return n > 0, err
}

func (s *Store) pickFree(tx *sql.Tx) (string, error) {
	used := map[string]bool{}
	rows, err := tx.Query(`SELECT area_code FROM ocn_servers`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return "", err
		}
		used[c] = true
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	// Random attempts first.
	for i := 0; i < 500; i++ {
		n := 200 + rand.Intn(800-200) // 200..799
		c := fmt.Sprintf("%03d", n)
		if !used[c] {
			return c, nil
		}
	}
	for n := 200; n <= 999; n++ {
		c := fmt.Sprintf("%03d", n)
		if !used[c] && AllowedArea(c) {
			return c, nil
		}
	}
	return "", ErrNoFreeAreas
}

// GetRoute returns the server hosting an area code, if ACTIVE.
func (s *Store) GetRoute(area string) (*ServerInfo, error) {
	info, err := s.get(area)
	if err != nil {
		return nil, err
	}
	if info.Status != "ACTIVE" {
		return nil, ErrServerNotFound
	}
	return info, nil
}

func (s *Store) get(area string) (*ServerInfo, error) {
	var (
		info ServerInfo
		pk   []byte
		reg  int64
	)
	err := s.db.QueryRow(`SELECT area_code, name, description, server_address, public_key, registered_at, status
		FROM ocn_servers WHERE area_code = ?`, area).
		Scan(&info.AreaCode, &info.Name, &info.Description, &info.ServerAddr, &pk, &reg, &info.Status)
	if err == sql.ErrNoRows {
		return nil, ErrServerNotFound
	}
	if err != nil {
		return nil, err
	}
	info.PublicKey = ed25519.PublicKey(pk)
	info.RegisteredAt = time.Unix(reg, 0)
	return &info, nil
}

// ListServers returns all servers ordered by area code.
func (s *Store) ListServers(limit int) ([]*ServerInfo, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.db.Query(`SELECT area_code, name, description, server_address, public_key, registered_at, status
		FROM ocn_servers ORDER BY area_code LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ServerInfo
	for rows.Next() {
		var (
			info ServerInfo
			pk   []byte
			reg  int64
		)
		if err := rows.Scan(&info.AreaCode, &info.Name, &info.Description, &info.ServerAddr, &pk, &reg, &info.Status); err != nil {
			return nil, err
		}
		info.PublicKey = ed25519.PublicKey(pk)
		info.RegisteredAt = time.Unix(reg, 0)
		out = append(out, &info)
	}
	return out, rows.Err()
}

// DeregisterServer removes a server. Signature must be over
// area_code|timestamp|"deregister" using the server's stored public key.
func (s *Store) DeregisterServer(area string, ts int64, sig []byte) error {
	info, err := s.get(area)
	if err != nil {
		return ErrServerNotFound
	}
	if !ValidAuth(info.PublicKey, area, ts, sig) {
		return ErrBadSignature
	}
	if _, err := s.db.Exec(`DELETE FROM ocn_servers WHERE area_code = ?`, area); err != nil {
		return err
	}
	return nil
}

// VerifyPushAuth checks a server's signature for PushDevice.
func (s *Store) VerifyPushAuth(area string, ts int64, message []byte, sig []byte) error {
	info, err := s.get(area)
	if err != nil {
		return ErrServerNotFound
	}
	if info.Status != "ACTIVE" {
		return ErrServerNotFound
	}
	if !ValidAuth(info.PublicKey, area, ts, sig) {
		return ErrBadSignature
	}
	return nil
}

// ValidAuth verifies an Ed25519 signature of area_code|timestamp|payload.
func ValidAuth(pub ed25519.PublicKey, area string, ts int64, sig []byte) bool {
	if len(sig) != ed25519.SignatureSize {
		return false
	}
	now := time.Now().Unix()
	if ts > now+int64(authWindow/time.Second) || ts < now-int64(authWindow/time.Second) {
		return false
	}
	data := []byte(fmt.Sprintf("%s|%d", area, ts))
	return ed25519.Verify(pub, data, sig)
}

// SignAuth is used by tests/clients to produce a valid auth signature.
func SignAuth(priv ed25519.PrivateKey, area string, ts int64) []byte {
	data := []byte(fmt.Sprintf("%s|%d", area, ts))
	return ed25519.Sign(priv, data)
}
