package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Voicemail struct {
	ID              string
	CallerNumber    string
	CallerName      string
	RecipientNumber string
	EncryptedAudio  []byte
	Format          string
	DurationSeconds int32
	Listened        bool
	CreatedAt       time.Time
}

// StoreVoicemail saves an encrypted voicemail
func (s *Store) StoreVoicemail(recipientNumber, callerNumber, callerName string, audio []byte, format string, duration int32) (string, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(
		`INSERT INTO voicemail (id, caller_number, caller_name, recipient_number, encrypted_audio, format, duration_seconds, listened, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		id, callerNumber, callerName, recipientNumber, audio, format, duration, time.Now().Unix(),
	)
	if err != nil {
		return "", fmt.Errorf("storing voicemail: %w", err)
	}
	return id, nil
}

// GetVoicemail retrieves a voicemail by ID (must match recipient)
func (s *Store) GetVoicemail(id, recipientNumber string) (*Voicemail, error) {
	var v Voicemail
	var createdAt int64
	var listened int

	err := s.db.QueryRow(
		`SELECT id, caller_number, caller_name, recipient_number, encrypted_audio, format, duration_seconds, listened, created_at
		 FROM voicemail WHERE id = ? AND recipient_number = ?`, id, recipientNumber,
	).Scan(&v.ID, &v.CallerNumber, &v.CallerName, &v.RecipientNumber,
		&v.EncryptedAudio, &v.Format, &v.DurationSeconds, &listened, &createdAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	v.Listened = listened != 0
	v.CreatedAt = time.Unix(createdAt, 0)
	return &v, nil
}

// ListVoicemails returns all voicemails for a recipient
func (s *Store) ListVoicemails(recipientNumber string) ([]*Voicemail, error) {
	rows, err := s.db.Query(
		`SELECT id, caller_number, caller_name, recipient_number, encrypted_audio, format, duration_seconds, listened, created_at
		 FROM voicemail WHERE recipient_number = ? ORDER BY created_at DESC`, recipientNumber,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var voicemails []*Voicemail
	for rows.Next() {
		var v Voicemail
		var createdAt int64
		var listened int

		if err := rows.Scan(&v.ID, &v.CallerNumber, &v.CallerName, &v.RecipientNumber,
			&v.EncryptedAudio, &v.Format, &v.DurationSeconds, &listened, &createdAt); err != nil {
			return nil, err
		}

		v.Listened = listened != 0
		v.CreatedAt = time.Unix(createdAt, 0)
		voicemails = append(voicemails, &v)
	}

	return voicemails, nil
}

// VoicemailMeta is a voicemail without the audio blob, for list views.
type VoicemailMeta struct {
	ID              string
	CallerNumber    string
	CallerName      string
	RecipientNumber string
	Format          string
	DurationSeconds int32
	Listened        bool
	CreatedAt       time.Time
}

// ListVoicemailMeta lists a recipient's messages newest first without audio.
func (s *Store) ListVoicemailMeta(recipientNumber string) ([]*VoicemailMeta, error) {
	rows, err := s.db.Query(
		`SELECT id, caller_number, caller_name, recipient_number, format, duration_seconds, listened, created_at
		 FROM voicemail WHERE recipient_number = ? ORDER BY created_at DESC`, recipientNumber,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*VoicemailMeta
	for rows.Next() {
		var v VoicemailMeta
		var listened int
		var createdAt int64
		if err := rows.Scan(&v.ID, &v.CallerNumber, &v.CallerName, &v.RecipientNumber,
			&v.Format, &v.DurationSeconds, &listened, &createdAt); err != nil {
			return nil, err
		}
		v.Listened = listened != 0
		v.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, &v)
	}
	return out, nil
}

// MarkListened marks a voicemail as listened
func (s *Store) MarkListened(id, recipientNumber string) error {
	_, err := s.db.Exec(
		`UPDATE voicemail SET listened = 1 WHERE id = ? AND recipient_number = ?`,
		id, recipientNumber,
	)
	return err
}

// DeleteVoicemail removes a voicemail
func (s *Store) DeleteVoicemail(id, recipientNumber string) error {
	_, err := s.db.Exec(
		`DELETE FROM voicemail WHERE id = ? AND recipient_number = ?`,
		id, recipientNumber,
	)
	return err
}

// CountUnlistened returns count of unlistened voicemails
func (s *Store) CountUnlistened(recipientNumber string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM voicemail WHERE recipient_number = ? AND listened = 0`,
		recipientNumber,
	).Scan(&count)
	return count, err
}
