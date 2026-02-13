package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const defaultEndpoint = "https://api.usetempo.dev"

// Config holds the Tempo CLI configuration stored at ~/.tempo/config.json.
type Config struct {
	APIToken string `json:"api_token"`
	Endpoint string `json:"endpoint"`
}

func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tempo")
}

func configPath() string {
	return filepath.Join(configDir(), "config.json")
}

// Load reads the config from ~/.tempo/config.json.
// Returns a default config if the file does not exist.
// The TEMPO_API_ENDPOINT env var overrides the configured endpoint.
func Load() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Endpoint: defaultEndpoint}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = defaultEndpoint
	}
	if ep := os.Getenv("TEMPO_API_ENDPOINT"); ep != "" {
		cfg.Endpoint = ep
	}
	return &cfg, nil
}

// Save writes the config to ~/.tempo/config.json with mode 0600.
func Save(cfg *Config) error {
	if err := os.MkdirAll(configDir(), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0600)
}
