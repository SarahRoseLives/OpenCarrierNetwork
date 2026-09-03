package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrLineNotFound = errors.New("line not found")

// ListLines returns lines (users) matching search, newest last seen first.
func (s *Store) ListLines(search string, offset, limit int) ([]*User, int, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if search == "" {
		rows, err = s.db.Query(
			`SELECT ksim_public_key, area_code, number, display_name, fcm_token, registered_at, last_seen
			 FROM users ORDER BY last_seen DESC LIMIT ? OFFSET ?`, limit, offset)
	} else {
		like := "%" + search + "%"
		rows, err = s.db.Query(
			`SELECT ksim_public_key, area_code, number, display_name, fcm_token, registered_at, last_seen
			 FROM users WHERE number LIKE ? OR display_name LIKE ?
			 ORDER BY last_seen DESC LIMIT ? OFFSET ?`, like, like, limit, offset)
	}
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	if search == "" {
		err = s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&total)
	} else {
		like := "%" + search + "%"
		err = s.db.QueryRow(
			`SELECT COUNT(*) FROM users WHERE number LIKE ? OR display_name LIKE ?`, like, like,
		).Scan(&total)
	}
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// DeleteLineByNumber releases a line, freeing its number.
func (s *Store) DeleteLineByNumber(number string) error {
	res, err := s.db.Exec(`DELETE FROM users WHERE number = ?`, number)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrLineNotFound
	}
	return nil
}

// UpdateLine renames a line and/or changes its number.
func (s *Store) UpdateLine(number string, newName, newNumber *string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Resolve current display name.
	var currentName string
	err = tx.QueryRow(`SELECT display_name FROM users WHERE number = ?`, number).Scan(&currentName)
	if err == sql.ErrNoRows {
		return ErrLineNotFound
	}
	if err != nil {
		return err
	}

	if newName == nil {
		newName = &currentName
	}

	target := number
	if newNumber != nil && *newNumber != number {
		// The new number must be free and not reserved by an issued token.
		var c int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE number = ?`, *newNumber).Scan(&c); err != nil {
			return err
		}
		if c > 0 {
			return ErrNumberTaken
		}
		if err := tx.QueryRow(
			`SELECT COUNT(*) FROM provisioning_tokens WHERE status='issued' AND number = ?`, *newNumber,
		).Scan(&c); err != nil {
			return err
		}
		if c > 0 {
			return ErrNumberTaken
		}
		target = *newNumber
	}

	if _, err := tx.Exec(
		`UPDATE users SET display_name = ?, number = ? WHERE number = ?`,
		*newName, target, number,
	); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrNumberTaken
		}
		return err
	}
	return tx.Commit()
}

// NumberAvailable reports whether a number is free to provision/assign.
func (s *Store) NumberAvailable(number string) (bool, error) {
	var c int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE number = ?`, number).Scan(&c); err != nil {
		return false, err
	}
	if c > 0 {
		return false, nil
	}
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM provisioning_tokens WHERE status='issued' AND number = ?`, number,
	).Scan(&c); err != nil {
		return false, err
	}
	return c == 0, nil
}

// LinesTotal is the total number of provisioned lines.
func (s *Store) LinesTotal() (int, error) {
	return s.CountUsers()
}

// FreeNumberEstimate approximates the free pool. Numbers are contiguous
// 1,000,000..9,999,999 minus active lines.
func (s *Store) FreeNumberEstimate() (int, error) {
	total, err := s.CountUsers()
	if err != nil {
		return 0, err
	}
	const pool = 9999999 - 1000000 + 1
	if total > pool {
		total = pool
	}
	return pool - total, nil
}

// UpdateAreaCodeForUsers stamps the server's area code onto any locally
// hosted line that was created before this server federated (area_code empty).
func (s *Store) UpdateAreaCodeForUsers(area string) error {
	if area == "" {
		return nil
	}
	_, err := s.db.Exec(
		`UPDATE users SET area_code = ? WHERE area_code = '' OR area_code IS NULL`,
		area,
	)
	return err
}

func scanUser(rows interface {
	Scan(dest ...interface{}) error
}) (*User, error) {
	var (
		u         User
		pub       []byte
		reg, last int64
	)
	if err := rows.Scan(&pub, &u.AreaCode, &u.Number, &u.DisplayName, &u.FCMToken, &reg, &last); err != nil {
		return nil, fmt.Errorf("scanning line: %w", err)
	}
	u.KSimPublicKey = pub
	u.RegisteredAt = timeFromUnix(reg)
	u.LastSeen = timeFromUnix(last)
	return &u, nil
}
