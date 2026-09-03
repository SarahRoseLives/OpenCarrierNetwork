package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// AdminAccount is a panel login.
type AdminAccount struct {
	Username   string
	MustChange bool
	LastLogin  time.Time
}

var (
	ErrBadCredentials = errors.New("invalid username or password")
)

// ensureDefaultAdmin seeds the default admin/admin account on first run.
func (s *Store) ensureDefaultAdmin() error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM admin_accounts`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO admin_accounts (username, password_hash, created_at, last_login, must_change)
		 VALUES ('admin', ?, ?, 0, 1)`,
		hash, time.Now().Unix(),
	)
	return err
}

// VerifyAdminLogin checks credentials. Returns mustChange=true on first login.
func (s *Store) VerifyAdminLogin(username, password string) (mustChange bool, err error) {
	var hash string
	var must int
	err = s.db.QueryRow(
		`SELECT password_hash, must_change FROM admin_accounts WHERE username = ?`,
		username,
	).Scan(&hash, &must)
	if err != nil {
		return false, ErrBadCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return false, ErrBadCredentials
	}
	s.db.Exec(`UPDATE admin_accounts SET last_login = ? WHERE username = ?`, time.Now().Unix(), username)
	return must == 1, nil
}

// AdminMustChange reports whether a password change is required.
func (s *Store) AdminMustChange(username string) (bool, error) {
	var must int
	err := s.db.QueryRow(`SELECT must_change FROM admin_accounts WHERE username = ?`, username).Scan(&must)
	return must == 1, err
}

// ChangeAdminPassword verifies the current password, then sets a new one.
func (s *Store) ChangeAdminPassword(username, current, new string) error {
	var hash string
	if err := s.db.QueryRow(
		`SELECT password_hash FROM admin_accounts WHERE username = ?`, username,
	).Scan(&hash); err != nil {
		return ErrBadCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return ErrBadCredentials
	}
	if len(new) < 6 {
		return errors.New("new password must be at least 4 characters")
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(new), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`UPDATE admin_accounts SET password_hash = ?, must_change = 0 WHERE username = ?`,
		newHash, username,
	)
	return err
}

// SetAdminPassword unconditionally sets a new password (used to clear
// must_change after the forced first-login flow via ChangeAdminPassword).
func (s *Store) SetAdminPassword(username, new string) error {
	if len(new) < 6 {
		return errors.New("new password must be at least 4 characters")
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(new), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`UPDATE admin_accounts SET password_hash = ?, must_change = 0 WHERE username = ?`,
		newHash, username,
	)
	return err
}

// ---- Sessions (opaque bearer tokens, stored hashed) ----

const sessionTTL = 12 * time.Hour

// CreateSession issues a new opaque session token and stores only its hash.
func (s *Store) CreateSession(username string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := HashToken(token)
	now := time.Now()
	_, err := s.db.Exec(
		`INSERT INTO admin_sessions (token_hash, username, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		hash, username, now.Unix(), now.Add(sessionTTL).Unix(),
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// SessionUsername returns the username for a session token, if valid.
func (s *Store) SessionUsername(token string) (string, error) {
	if token == "" {
		return "", ErrBadCredentials
	}
	var username string
	var expires int64
	err := s.db.QueryRow(
		`SELECT username, expires_at FROM admin_sessions WHERE token_hash = ?`,
		HashToken(token),
	).Scan(&username, &expires)
	if err != nil {
		return "", ErrBadCredentials
	}
	if time.Now().Unix() > expires {
		s.db.Exec(`DELETE FROM admin_sessions WHERE token_hash = ?`, HashToken(token))
		return "", ErrBadCredentials
	}
	return username, nil
}

// DeleteSession revokes a session token.
func (s *Store) DeleteSession(token string) error {
	if token == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM admin_sessions WHERE token_hash = ?`, HashToken(token))
	return err
}

// HashToken returns the hex of the sha-256 of the given value. Used to store
// provisioning and session tokens so the plaintext is never persisted.
func HashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}
