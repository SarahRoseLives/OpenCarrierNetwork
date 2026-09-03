package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	Host string `json:"host"`
	Port int    `json:"port"`

	AreaCode       string `json:"area_code"`       // Empty until federated with registry
	ServerName     string `json:"server_name"`
	Description    string `json:"description"`
	ServerKeyPath  string `json:"server_key_path"`
	DatabasePath   string `json:"database_path"`
	RegistryAddress string `json:"registry_address,omitempty"` // Set when federating

	TLSCertFile string `json:"tls_cert_file,omitempty"`
	TLSKeyFile  string `json:"tls_key_file,omitempty"`

	VoicemailPath        string `json:"voicemail_path"`
	MaxVoicemailDuration int    `json:"max_voicemail_duration_seconds"`
}

func DefaultConfig() *Config {
	return &Config{
		Host:         "0.0.0.0",
		Port:         9100,
		AreaCode:     "", // No area code until federated
		ServerName:   "OCN Server",
		Description:  "Default OCN server",
		ServerKeyPath: "server.key",
		DatabasePath: "ocnserver.db",
		VoicemailPath: "voicemail/",
		MaxVoicemailDuration: 120,
	}
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (c *Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
