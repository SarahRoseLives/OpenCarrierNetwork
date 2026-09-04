package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DMMessage is one undelivered direct message queued on the recipient's home
// server (encrypted envelope) until the device fetches/acks it.
type DMMessage struct {
	MessageID string
	Recipient string // 7-digit local number
	Envelope  []byte // encrypted JSON envelope
	CreatedAt time.Time
}

// EnqueueDM queues an encrypted message envelope for a recipient.
func (s *Store) EnqueueDM(recipient string, envelope []byte) (string, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(
		`INSERT INTO dm_outbox (message_id, recipient, envelope, created_at)
		 VALUES (?, ?, ?, ?)`,
		id, recipient, envelope, time.Now().Unix(),
	)
	if err != nil {
		return "", fmt.Errorf("enqueue dm: %w", err)
	}
	return id, nil
}

// StoreDMPreassigned stores an envelope under a specific message id (used when
// relaying a message created on another server).
func (s *Store) StoreDMPreassigned(messageID, recipient string, envelope []byte) error {
	_, err := s.db.Exec(
		`INSERT INTO dm_outbox (message_id, recipient, envelope, created_at)
		 VALUES (?, ?, ?, ?)`,
		messageID, recipient, envelope, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("store dm: %w", err)
	}
	return nil
}

// ListPendingDM returns all undelivered envelopes for a recipient.
func (s *Store) ListPendingDM(recipient string) ([]*DMMessage, error) {
	rows, err := s.db.Query(
		`SELECT message_id, recipient, envelope, created_at
		 FROM dm_outbox WHERE recipient = ? ORDER BY created_at ASC`, recipient,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*DMMessage
	for rows.Next() {
		var m DMMessage
		var createdAt int64
		if err := rows.Scan(&m.MessageID, &m.Recipient, &m.Envelope, &createdAt); err != nil {
			return nil, err
		}
		m.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, &m)
	}
	return out, nil
}

// GetDM fetches one queued message (scoped to a recipient).
func (s *Store) GetDM(messageID, recipient string) (*DMMessage, error) {
	var m DMMessage
	var createdAt int64
	err := s.db.QueryRow(
		`SELECT message_id, recipient, envelope, created_at
		 FROM dm_outbox WHERE message_id = ? AND recipient = ?`,
		messageID, recipient,
	).Scan(&m.MessageID, &m.Recipient, &m.Envelope, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.CreatedAt = time.Unix(createdAt, 0)
	return &m, nil
}

// DeleteDM removes a queued message after the device acknowledges it.
func (s *Store) DeleteDM(messageID, recipient string) error {
	_, err := s.db.Exec(
		`DELETE FROM dm_outbox WHERE message_id = ? AND recipient = ?`,
		messageID, recipient,
	)
	return err
}

// PruneDM removes undelivered messages older than before.
func (s *Store) PruneDM(before time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM dm_outbox WHERE created_at < ?`, before.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
