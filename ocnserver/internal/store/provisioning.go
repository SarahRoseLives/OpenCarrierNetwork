package store

import (
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// ProvisionToken is a one-time activation code issued by the admin panel.
type ProvisionToken struct {
	TokenHash     string
	Number        string // empty => auto-assign at claim
	DisplayName   string
	Notes         string
	CreatedBy     string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	Status        string // issued | used | revoked
	ClaimedPubKey []byte
	ClaimedAt     time.Time
}

// ProvisionTokenView is a token as returned to the panel (hash is the lookup
// id, not the secret — the plaintext secret is never stored or returned).
type ProvisionTokenView struct {
	TokenHash   string `json:"token_hash"`
	Number      string `json:"number"`
	DisplayName string `json:"display_name"`
	Notes       string `json:"notes"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   int64  `json:"created_at"`
	ExpiresAt   int64  `json:"expires_at"`
	Status      string `json:"status"`
}

var (
	ErrTokenNotFound = errors.New("activation token not found")
	ErrTokenUsed     = errors.New("activation token already used")
	ErrTokenExpired  = errors.New("activation token expired")
	ErrTokenRevoked  = errors.New("activation token revoked")
	ErrNumberTaken   = errors.New("number already in use")
	ErrTokenRequired = errors.New("provisioning token required")
)

// NewProvisionToken issues a token for a fixed number (if number != "") or for
// auto-assignment. tokenHash must be HashToken(plaintext).
func (s *Store) NewProvisionToken(tokenHash, number, displayName, notes, createdBy string, ttl time.Duration) error {
	_, err := s.db.Exec(
		`INSERT INTO provisioning_tokens
			(token_hash, number, display_name, notes, created_by, created_at, expires_at, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'issued')`,
		tokenHash, nullIfEmpty(number), displayName, notes, createdBy,
		time.Now().Unix(), time.Now().Add(ttl).Unix(),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrNumberTaken
		}
		return err
	}
	return nil
}

// ListProvisionTokens returns issued/used/revoked tokens, newest first.
func (s *Store) ListProvisionTokens(limit int) ([]ProvisionTokenView, error) {
	rows, err := s.db.Query(
		`SELECT token_hash, number, display_name, notes, created_by, created_at, expires_at, status
		 FROM provisioning_tokens ORDER BY created_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProvisionTokenView
	for rows.Next() {
		var t ProvisionTokenView
		var number sql.NullString
		var created, expires int64
		if err := rows.Scan(&t.TokenHash, &number, &t.DisplayName, &t.Notes, &t.CreatedBy, &created, &expires, &t.Status); err != nil {
			return nil, err
		}
		t.Number = number.String
		t.CreatedAt = created
		t.ExpiresAt = expires
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeProvisionToken marks an issued token as revoked.
func (s *Store) RevokeProvisionToken(tokenHash string) error {
	res, err := s.db.Exec(
		`UPDATE provisioning_tokens SET status = 'revoked' WHERE token_hash = ? AND status = 'issued'`,
		tokenHash,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTokenNotFound
	}
	return nil
}

// ProvisionUser atomically validates a token and creates the line.
//
// Called during registration: the client presents a plaintext activation
// token. If valid and unused, we allocate the number (the token's fixed number
// if set, otherwise a fresh one), create the user row, and consume the token.
// Returns the allocated number.
func (s *Store) ProvisionUser(tokenHash string, pubKey ed25519.PublicKey, areaCode, displayName string) (string, error) {
	if tokenHash == "" {
		return "", ErrTokenRequired
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		num, err := s.provisionUserOnce(tokenHash, pubKey, areaCode, displayName)
		if err == nil {
			return num, nil
		}
		lastErr = err
		// A lost number race on an auto-assign token is retryable.
		if errors.Is(err, ErrNumberTaken) {
			continue
		}
		break
	}
	return "", lastErr
}

func (s *Store) provisionUserOnce(tokenHash string, pubKey ed25519.PublicKey, areaCode, displayName string) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var number sql.NullString
	var status string
	var expires int64
	err = tx.QueryRow(
		`SELECT number, status, expires_at FROM provisioning_tokens WHERE token_hash = ?`,
		tokenHash,
	).Scan(&number, &status, &expires)
	if err == sql.ErrNoRows {
		return "", ErrTokenNotFound
	}
	if err != nil {
		return "", err
	}
	switch {
	case status == "used":
		return "", ErrTokenUsed
	case status == "revoked":
		return "", ErrTokenRevoked
	case time.Now().Unix() > expires:
		return "", ErrTokenExpired
	case status != "issued":
		return "", ErrTokenNotFound
	}

	var num string
	if number.Valid && number.String != "" {
		num = number.String
		// Confirm it is still free (someone may have taken it another way).
		var c int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE number = ?`, num).Scan(&c); err != nil {
			return "", err
		}
		if c > 0 {
			return "", ErrNumberTaken
		}
	} else {
		used, err := takenNumberSet(tx)
		if err != nil {
			return "", err
		}
		num, err = pickFreeNumber(used)
		if err != nil {
			return "", err
		}
	}

	now := time.Now().Unix()
	_, err = tx.Exec(
		`INSERT INTO users (ksim_public_key, area_code, number, display_name, fcm_token, registered_at, last_seen)
		 VALUES (?, ?, ?, ?, '', ?, ?)`,
		pubKey, areaCode, num, displayName, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return "", ErrNumberTaken
		}
		return "", err
	}

	if _, err := tx.Exec(
		`UPDATE provisioning_tokens SET status = 'used', claimed_pubkey = ?, claimed_at = ? WHERE token_hash = ?`,
		pubKey, now, tokenHash,
	); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return num, nil
}

// TakenNumbers returns the set of numbers that are currently unavailable
// (in use by a line, or reserved by an outstanding issued token).
func (s *Store) TakenNumbers() (map[string]bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return takenNumberSet(tx)
}

// RandomFreeNumbers returns up to n numbers that are currently free, or fewer.
func (s *Store) RandomFreeNumbers(n int) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	used, err := takenNumberSet(tx)
	if err != nil {
		return nil, err
	}
	return pickFreeNumbers(n, used)
}

// ProvisionTokenCounts returns counts per status.
func (s *Store) ProvisionTokenCounts() (issued, used int, err error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM provisioning_tokens GROUP BY status`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var c int
		if err := rows.Scan(&st, &c); err != nil {
			return 0, 0, err
		}
		switch st {
		case "issued":
			issued = c
		case "used":
			used = c
		}
	}
	return issued, used, rows.Err()
}

func takenNumberSet(tx *sql.Tx) (map[string]bool, error) {
	used := make(map[string]bool)
	rows, err := tx.Query(`SELECT number FROM users`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		used[n] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = tx.Query(`SELECT number FROM provisioning_tokens WHERE status = 'issued' AND number IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		used[n] = true
	}
	rows.Close()
	return used, rows.Err()
}

const (
	minNumber = 1000000
	maxNumber = 9999999
)

func pickFreeNumbers(n int, used map[string]bool) ([]string, error) {
	var out []string
	for i := 0; i < 2000 && len(out) < n; i++ {
		num := minNumber + rand.Intn(maxNumber-minNumber+1)
		ns := fmt.Sprintf("%07d", num)
		if !used[ns] {
			used[ns] = true
			out = append(out, ns)
		}
	}
	// Fallback: linear scan to fill any remaining slots.
	for num := minNumber; num <= maxNumber && len(out) < n; num++ {
		ns := fmt.Sprintf("%07d", num)
		if !used[ns] {
			used[ns] = true
			out = append(out, ns)
		}
	}
	return out, nil
}

func pickFreeNumber(used map[string]bool) (string, error) {
	free, err := pickFreeNumbers(1, used)
	if err != nil {
		return "", err
	}
	if len(free) == 0 {
		return "", errors.New("no available numbers")
	}
	return free[0], nil
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
