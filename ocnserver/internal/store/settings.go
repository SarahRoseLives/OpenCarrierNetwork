package store

import (
	"database/sql"
	"strconv"
)

// SetSetting persists a key/value (used e.g. for the assigned area code).
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

// GetSetting returns a stored value, or "" when absent.
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

// FederationSettings is what the admin panel persists for registry joining.
type FederationSettings struct {
	RegistryAddress      string
	RegistryInsecure     bool
	RequestedAreaCode    string
	FederationPublicAddr string
}

const (
	keyRegistryAddress  = "registry_address"
	keyRegistryInsecure = "registry_insecure"
	keyRegistryAreaCode = "registry_area_code"
	keyFedPublic        = "federation_public_address"
)

// GetFederationSettings loads the panel-saved federation settings.
func (s *Store) GetFederationSettings() (*FederationSettings, error) {
	fs := &FederationSettings{}
	reg, err := s.GetSetting(keyRegistryAddress)
	if err != nil {
		return nil, err
	}
	insec, err := s.GetSetting(keyRegistryInsecure)
	if err != nil {
		return nil, err
	}
	area, err := s.GetSetting(keyRegistryAreaCode)
	if err != nil {
		return nil, err
	}
	fed, err := s.GetSetting(keyFedPublic)
	if err != nil {
		return nil, err
	}
	fs.RegistryAddress = reg
	fs.RegistryInsecure = insec == "true"
	fs.RequestedAreaCode = area
	fs.FederationPublicAddr = fed
	return fs, nil
}

// SaveFederationSettings writes the panel-saved federation settings.
func (s *Store) SaveFederationSettings(fs *FederationSettings) error {
	if err := s.SetSetting(keyRegistryAddress, fs.RegistryAddress); err != nil {
		return err
	}
	if err := s.SetSetting(keyRegistryInsecure, strconv.FormatBool(fs.RegistryInsecure)); err != nil {
		return err
	}
	if err := s.SetSetting(keyRegistryAreaCode, fs.RequestedAreaCode); err != nil {
		return err
	}
	return s.SetSetting(keyFedPublic, fs.FederationPublicAddr)
}

// ClearFederationSettings disables panel-configured federation on next start.
func (s *Store) ClearFederationSettings() error {
	for _, k := range []string{keyRegistryAddress, keyRegistryInsecure, keyRegistryAreaCode, keyFedPublic} {
		if _, err := s.db.Exec(`DELETE FROM settings WHERE key = ?`, k); err != nil {
			return err
		}
	}
	return nil
}
