package store

import (
	"crypto/ed25519"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type User struct {
	KSimPublicKey ed25519.PublicKey
	AreaCode      string
	Number        string // 7-digit local number
	DisplayName   string
	FCMToken      string
	RegisteredAt  time.Time
	LastSeen      time.Time
}

type Store struct {
	db *sql.DB
}

func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	if err := s.ensureDefaultAdmin(); err != nil {
		return nil, fmt.Errorf("seeding default admin: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func timeFromUnix(sec int64) time.Time {
	return time.Unix(sec, 0)
}

func (s *Store) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS users (
			ksim_public_key BLOB PRIMARY KEY,
			area_code TEXT NOT NULL,
			number TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL DEFAULT '',
			fcm_token TEXT NOT NULL DEFAULT '',
			registered_at INTEGER NOT NULL,
			last_seen INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_users_number ON users(number)`,
		`CREATE TABLE IF NOT EXISTS voicemail (
			id TEXT PRIMARY KEY,
			caller_number TEXT NOT NULL,
			caller_name TEXT NOT NULL DEFAULT '',
			recipient_number TEXT NOT NULL,
			encrypted_audio BLOB NOT NULL,
			format TEXT NOT NULL DEFAULT 'opus',
			duration_seconds INTEGER NOT NULL,
			listened INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_voicemail_recipient ON voicemail(recipient_number)`,
		// Add fcm_token to pre-existing users tables (created before the column existed)
		`ALTER TABLE users ADD COLUMN fcm_token TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS admin_accounts (
			username TEXT PRIMARY KEY,
			password_hash TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			last_login INTEGER NOT NULL DEFAULT 0,
			must_change INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS admin_sessions (
			token_hash TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_sessions_username ON admin_sessions(username)`,
		`CREATE TABLE IF NOT EXISTS provisioning_tokens (
			token_hash TEXT PRIMARY KEY,
			number TEXT,
			display_name TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'issued',
			claimed_pubkey BLOB,
			claimed_at INTEGER
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_prov_tokens_number
			ON provisioning_tokens(number) WHERE number IS NOT NULL AND status = 'issued'`,
		`CREATE INDEX IF NOT EXISTS idx_prov_tokens_status ON provisioning_tokens(status, created_at)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS dm_outbox (
			message_id TEXT PRIMARY KEY,
			recipient TEXT NOT NULL,
			envelope BLOB NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dm_outbox_recipient ON dm_outbox(recipient, created_at)`,
	}

	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			// Ignore duplicate column errors from ALTER TABLE on already-migrated DBs
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("migration: %w", err)
			}
		}
	}

	return nil
}

// CreateUser registers a new user with a phone number
func (s *Store) CreateUser(u *User) error {
	_, err := s.db.Exec(
		`INSERT INTO users (ksim_public_key, area_code, number, display_name, fcm_token, registered_at, last_seen)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.KSimPublicKey, u.AreaCode, u.Number, u.DisplayName, u.FCMToken,
		u.RegisteredAt.Unix(), u.LastSeen.Unix(),
	)
	return err
}

// GetUserByPublicKey looks up a user by their kSIM public key
func (s *Store) GetUserByPublicKey(pubKey ed25519.PublicKey) (*User, error) {
	var u User
	var pubKeyBytes []byte
	var regAt, lastSeen int64

	err := s.db.QueryRow(
		`SELECT ksim_public_key, area_code, number, display_name, fcm_token, registered_at, last_seen
		 FROM users WHERE ksim_public_key = ?`, pubKey,
	).Scan(&pubKeyBytes, &u.AreaCode, &u.Number, &u.DisplayName, &u.FCMToken, &regAt, &lastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	u.KSimPublicKey = ed25519.PublicKey(pubKeyBytes)
	u.RegisteredAt = time.Unix(regAt, 0)
	u.LastSeen = time.Unix(lastSeen, 0)
	return &u, nil
}

// GetUserByNumber looks up a user by their phone number
func (s *Store) GetUserByNumber(number string) (*User, error) {
	var u User
	var pubKeyBytes []byte
	var regAt, lastSeen int64

	err := s.db.QueryRow(
		`SELECT ksim_public_key, area_code, number, display_name, fcm_token, registered_at, last_seen
		 FROM users WHERE number = ?`, number,
	).Scan(&pubKeyBytes, &u.AreaCode, &u.Number, &u.DisplayName, &u.FCMToken, &regAt, &lastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	u.KSimPublicKey = ed25519.PublicKey(pubKeyBytes)
	u.RegisteredAt = time.Unix(regAt, 0)
	u.LastSeen = time.Unix(lastSeen, 0)
	return &u, nil
}

// UpdateFCMToken updates the user's FCM push notification token
func (s *Store) UpdateFCMToken(pubKey ed25519.PublicKey, token string) error {
	_, err := s.db.Exec(
		`UPDATE users SET fcm_token = ? WHERE ksim_public_key = ?`,
		token, pubKey,
	)
	return err
}

// UpdateLastSeen updates the user's last seen timestamp
func (s *Store) UpdateLastSeen(pubKey ed25519.PublicKey) error {
	_, err := s.db.Exec(
		`UPDATE users SET last_seen = ? WHERE ksim_public_key = ?`,
		time.Now().Unix(), pubKey,
	)
	return err
}

// UpdateDisplayName updates the user's display name
func (s *Store) UpdateDisplayName(pubKey ed25519.PublicKey, name string) error {
	_, err := s.db.Exec(
		`UPDATE users SET display_name = ? WHERE ksim_public_key = ?`,
		name, pubKey,
	)
	return err
}

// NumberExists checks if a number is already taken
func (s *Store) NumberExists(number string) (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE number = ?`, number).Scan(&count)
	return count > 0, err
}

// CountUsers returns total registered users
func (s *Store) CountUsers() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}
