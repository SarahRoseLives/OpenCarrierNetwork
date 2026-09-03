package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ServiceNumber is an 800/900 network service assigned to a hosting exchange.
type ServiceNumber struct {
	FullNumber  string // 10 digits, starts with 800 or 900
	Vanity      string // display alias, e.g. ROO-M001
	Name        string
	Description string
	HostArea    string // hosting exchange area code
	Status      string // ACTIVE | SUSPENDED
	CreatedAt   time.Time
}

var ErrServiceInvalid = errors.New("service number must be 10 digits starting with 800 or 900")
var ErrServiceTaken = errors.New("service number already in use")
var ErrServiceNotFound = errors.New("service number not found")

// AllowedServiceFull validates an 800/900 service number.
func AllowedServiceFull(full string) bool {
	if len(full) != 10 {
		return false
	}
	for _, c := range full {
		if c < '0' || c > '9' {
			return false
		}
	}
	return strings.HasPrefix(full, "800") || strings.HasPrefix(full, "900")
}

// ClaimServiceNumber registers a service number hosted by the given exchange.
func (s *Store) ClaimServiceNumber(sn *ServiceNumber) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if !AllowedServiceFull(sn.FullNumber) {
		return ErrServiceInvalid
	}
	// Host must be an active exchange.
	var host string
	err = tx.QueryRow(`SELECT area_code FROM ocn_servers WHERE area_code = ? AND status = 'ACTIVE'`, sn.HostArea).Scan(&host)
	if err == sql.ErrNoRows {
		return ErrServerNotFound
	}
	if err != nil {
		return err
	}

	_, err = tx.Exec(`INSERT INTO service_numbers
		(full_number, vanity, name, description, host_area, status, created_at)
		VALUES (?, ?, ?, ?, ?, 'ACTIVE', ?)`,
		sn.FullNumber, sn.Vanity, sn.Name, sn.Description, sn.HostArea, time.Now().Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrServiceTaken
		}
		return err
	}
	return tx.Commit()
}

// ListServiceNumbers returns all 800/900 service numbers.
func (s *Store) ListServiceNumbers() ([]*ServiceNumber, error) {
	rows, err := s.db.Query(`SELECT full_number, vanity, name, description, host_area, status, created_at
		FROM service_numbers ORDER BY full_number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ServiceNumber
	for rows.Next() {
		sn, err := scanServiceNumber(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

// GetServiceNumber returns one service number.
func (s *Store) GetServiceNumber(full string) (*ServiceNumber, error) {
	row := s.db.QueryRow(`SELECT full_number, vanity, name, description, host_area, status, created_at
		FROM service_numbers WHERE full_number = ?`, full)
	sn, err := scanServiceNumber(row)
	if err == sql.ErrNoRows {
		return nil, ErrServiceNotFound
	}
	if err != nil {
		return nil, err
	}
	return sn, nil
}

// SetServiceStatus suspends/activates a service number.
func (s *Store) SetServiceStatus(full, status string) error {
	if status != "ACTIVE" && status != "SUSPENDED" {
		return errors.New("invalid status")
	}
	res, err := s.db.Exec(`UPDATE service_numbers SET status = ? WHERE full_number = ?`, status, full)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrServiceNotFound
	}
	return nil
}

// DeleteServiceNumber removes a service number.
func (s *Store) DeleteServiceNumber(full string) error {
	res, err := s.db.Exec(`DELETE FROM service_numbers WHERE full_number = ?`, full)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrServiceNotFound
	}
	return nil
}

// ResolveService returns the hosting exchange for a service number, or nil.
func (s *Store) ResolveService(full string) (*ServiceNumber, *ServerInfo, error) {
	if !AllowedServiceFull(full) {
		return nil, nil, ErrServiceNotFound
	}
	sn, err := s.GetServiceNumber(full)
	if err != nil {
		return nil, nil, err
	}
	if sn.Status != "ACTIVE" {
		return nil, nil, ErrServiceNotFound
	}
	host, err := s.GetRoute(sn.HostArea)
	if err != nil {
		return nil, nil, err
	}
	return sn, host, nil
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanServiceNumber(r scanner) (*ServiceNumber, error) {
	var (
		sn ServiceNumber
		ts int64
	)
	if err := r.Scan(&sn.FullNumber, &sn.Vanity, &sn.Name, &sn.Description, &sn.HostArea, &sn.Status, &ts); err != nil {
		return nil, err
	}
	sn.CreatedAt = time.Unix(ts, 0)
	return &sn, nil
}
