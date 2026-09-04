package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	Host string `json:"host"`
	Port int    `json:"port"`

	AreaCode      string `json:"area_code"` // Empty until federated with registry
	ServerName    string `json:"server_name"`
	Description   string `json:"description"`
	ServerKeyPath string `json:"server_key_path"`
	DatabasePath  string `json:"database_path"`

	TLSCertFile string `json:"tls_cert_file,omitempty"`
	TLSKeyFile  string `json:"tls_key_file,omitempty"`

	VoicemailPath        string `json:"voicemail_path"`
	MaxVoicemailDuration int    `json:"max_voicemail_duration_seconds"`

	// RingTimeoutSeconds is how long an online callee may ring before an
	// unanswered/declined call is routed into voicemail.
	RingTimeoutSeconds int `json:"ring_timeout_seconds,omitempty"`
	// PendingCallTimeoutSeconds is how long an offline (pushed) callee's call
	// may wait before it is routed into voicemail.
	PendingCallTimeoutSeconds int `json:"pending_call_timeout_seconds,omitempty"`

	AdminHost     string `json:"admin_host,omitempty"`     // admin web panel bind host
	AdminPort     int    `json:"admin_port,omitempty"`     // admin web panel port (default 8080)
	PublicAddress string `json:"public_address,omitempty"` // e.g. "192.168.1.240:9100" used in provisioning QR/links

	RegistryAddress  string `json:"registry_address,omitempty"`   // OCN registry gRPC host:port ("" = standalone)
	RegistryAreaCode string `json:"registry_area_code,omitempty"` // requested area code when federating ("" = auto)
	RegistryInsecure bool   `json:"registry_insecure,omitempty"`  // plaintext to registry (dev only)

	FedAddr       string `json:"federation_addr,omitempty"`           // inter-server gRPC listen address
	FedPublicAddr string `json:"federation_public_address,omitempty"` // reachable host:port advertised to registry
	FedInsecure   bool   `json:"federation_insecure,omitempty"`       // plaintext inter-server gRPC (dev only)

	// ServiceNumbers lists 800/900 numbers this server hosts.
	ServiceNumbers map[string]ServiceNumberConfig `json:"service_numbers,omitempty"`
}

// ServiceNumberConfig describes a hosted 800/900 service.
type ServiceNumberConfig struct {
	Name       string `json:"name"`
	Phrase     string `json:"phrase"` // announcement text (TTS)
	Conference bool   `json:"conference"`
}

func DefaultConfig() *Config {
	return &Config{
		Host:                      "0.0.0.0",
		Port:                      9100,
		AreaCode:                  "", // No area code until federated
		ServerName:                "OCN Server",
		Description:               "Default OCN server",
		ServerKeyPath:             "server.key",
		DatabasePath:              "ocnserver.db",
		VoicemailPath:             "voicemail/",
		MaxVoicemailDuration:      120,
		RingTimeoutSeconds:        15,
		PendingCallTimeoutSeconds: 15,
		AdminHost:                 "0.0.0.0",
		AdminPort:                 8080,
		FedAddr:                   ":9110",
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

func (c *Config) AdminAddress() string {
	return fmt.Sprintf("%s:%d", c.AdminHost, c.AdminPort)
}
