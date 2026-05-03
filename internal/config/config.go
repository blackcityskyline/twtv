package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the top-level application configuration.
type Config struct {
	Auth    Auth    `json:"auth"`
	Chat    Chat    `json:"chat"`
	Player  Player  `json:"player"`
	History History `json:"history"`
}

type Auth struct {
	ClientID    string `json:"client_id"`
	AccessToken string `json:"access_token"`
}

type Chat struct {
	Terminal string `json:"terminal"` // override auto-detected terminal emulator
	Command  string `json:"command"`  // supports <channel> placeholder
}

type Player struct {
	Quality string `json:"quality"`
}

type History struct {
	File  string `json:"file"`
	Limit int    `json:"limit"`
}

// Load reads the config file, creating it with defaults if absent.
func Load() (*Config, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	p := filepath.Join(dir, "config.json")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		cfg := defaultConfig(dir)
		return cfg, writeJSON(p, cfg)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	cfg := defaultConfig(dir)
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	applyDefaults(cfg, dir)
	return cfg, nil
}

func defaultConfig(dir string) *Config {
	return &Config{
		Auth: Auth{},
		Chat: Chat{Command: "twt -c"},
		Player: Player{Quality: "best"},
		History: History{
			File:  filepath.Join(dir, "history.log"),
			Limit: 500,
		},
	}
}

// applyDefaults fills in zero-value fields that must always have a value.
func applyDefaults(cfg *Config, dir string) {
	if cfg.History.File == "" {
		cfg.History.File = filepath.Join(dir, "history.log")
	}
	if cfg.History.Limit == 0 {
		cfg.History.Limit = 500
	}
	if cfg.Chat.Command == "" {
		cfg.Chat.Command = "twt -c"
	}
}

func configDir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "twtv"), nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
