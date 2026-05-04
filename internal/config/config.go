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
	Notify  Notify  `json:"notify"`
}

type Auth struct {
	ClientID    string `json:"client_id"`
	AccessToken string `json:"access_token"`
}

type Chat struct {
	Terminal string `json:"terminal"` // overrides auto-detected terminal emulator
	Command  string `json:"command"`  // supports <channel> placeholder
}

type Player struct {
	Quality string `json:"quality"`
}

type History struct {
	File  string `json:"file"`
	Limit int    `json:"limit"`
}

type Notify struct {
	IntervalSec int    `json:"interval_sec"` // polling interval for twtv-notify daemon
	Command     string `json:"command"`       // notification command, e.g. "notify-send"
}

// Load reads the config file, creating it with defaults if absent.
func Load() (*Config, error) {
	d, err := dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return nil, err
	}

	p := filepath.Join(d, "config.json")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		cfg := defaults(d)
		return cfg, writeJSON(p, cfg)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	cfg := defaults(d)
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	fill(cfg, d)
	return cfg, nil
}

// Dir returns the twtv config directory.
func Dir() (string, error) { return dir() }

func dir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "twtv"), nil
}

func defaults(d string) *Config {
	return &Config{
		Auth:   Auth{},
		Chat:   Chat{Command: "twt -c"},
		Player: Player{Quality: "best"},
		History: History{
			File:  filepath.Join(d, "history.log"),
			Limit: 500,
		},
		Notify: Notify{
			IntervalSec: 120,
			Command:     "notify-send",
		},
	}
}

// fill sets zero-value fields that must always have a value.
func fill(cfg *Config, d string) {
	if cfg.History.File == "" {
		cfg.History.File = filepath.Join(d, "history.log")
	}
	if cfg.History.Limit == 0 {
		cfg.History.Limit = 500
	}
	if cfg.Chat.Command == "" {
		cfg.Chat.Command = "twt -c"
	}
	if cfg.Notify.IntervalSec == 0 {
		cfg.Notify.IntervalSec = 120
	}
	if cfg.Notify.Command == "" {
		cfg.Notify.Command = "notify-send"
	}
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
