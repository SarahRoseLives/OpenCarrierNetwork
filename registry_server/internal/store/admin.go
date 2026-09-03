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

var ErrBadLogin = errors.New("invalid username or password")

// EnsureDefaultAdmin seeds admin/admin on first boot.
func (s *Store) EnsureDefaultAdmin() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM admin_accounts`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO admin_accounts (username, password_hash, created_at, last_login, must_change)
		VALUES ('admin', ?, ?, 0, 1)`, hash, time.Now().Unix())
	return err
}

// VerifyAdminLogin checks credentials; returns mustChange on first login.
func (s *Store) VerifyAdminLogin(username, password string) (bool, error) {
	var hash string
	var must int
	err := s.db.QueryRow(`SELECT password_hash, must_change FROM admin_accounts WHERE username = ?`, username).
		Scan(&hash, &must)
	if err != nil {
		return false, ErrBadLogin
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return false, ErrBadLogin
	}
	s.db.Exec(`UPDATE admin_accounts SET last_login = ? WHERE username = ?`, time.Now().Unix(), username)
	return must == 1, nil
}

// ChangeAdminPassword verifies current then sets a new password.
func (s *Store) ChangeAdminPassword(username, current, new string) error {
	var hash string
	if err := s.db.QueryRow(`SELECT password_hash FROM admin_accounts WHERE username = ?`, username).Scan(&hash); err != nil {
		return ErrBadLogin
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return ErrBadLogin
	}
	if len(new) < 4 {
		return errors.New("new password must be at least 4 characters")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(new), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE admin_accounts SET password_hash = ?, must_change = 0 WHERE username = ?`, h, username)
	return err
}

// AdminMustChange reports whether a password change is required.
func (s *Store) AdminMustChange(username string) (bool, error) {
	var must int
	err := s.db.QueryRow(`SELECT must_change FROM admin_accounts WHERE username = ?`, username).Scan(&must)
	return must == 1, err
}

const adminSessionTTL = 12 * time.Hour

func (s *Store) CreateSession(username string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now()
	_, err := s.db.Exec(`INSERT INTO admin_sessions (token_hash, username, created_at, expires_at)
		VALUES (?, ?, ?, ?)`, HashToken(token), username, now.Unix(), now.Add(adminSessionTTL).Unix())
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) SessionUsername(token string) (string, error) {
	if token == "" {
		return "", ErrBadLogin
	}
	var u string
	var exp int64
	err := s.db.QueryRow(`SELECT username, expires_at FROM admin_sessions WHERE token_hash = ?`, HashToken(token)).
		Scan(&u, &exp)
	if err != nil {
		return "", ErrBadLogin
	}
	if time.Now().Unix() > exp {
		s.db.Exec(`DELETE FROM admin_sessions WHERE token_hash = ?`, HashToken(token))
		return "", ErrBadLogin
	}
	return u, nil
}

func (s *Store) DeleteSession(token string) error {
	if token == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM admin_sessions WHERE token_hash = ?`, HashToken(token))
	return err
}

// HashToken is the sha-256 hex of a value (session/service tokens).
func HashToken(v string) string {
	sum := sha256.Sum256([]byte(v))
	return fmt.Sprintf("%x", sum[:])
}
