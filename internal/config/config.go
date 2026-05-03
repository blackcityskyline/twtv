package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const defaultConfigName = "config.json"

var ConfigDir = filepath.Join(os.Getenv("HOME"), ".config", "twtv")

type Auth struct {
	ClientID    string `json:"client_id"`
	AccessToken string `json:"access_token"`
}

type Chat struct {
	Terminal string `json:"terminal"` // empty = auto-detect
	Command  string `json:"command"`  // e.g. "twt -c"
}

type Player struct {
	Quality string `json:"quality"` // best, 720p, etc.
}

type History struct {
	Limit int `json:"limit"`
}

type Fzf struct {
	ShowOffline bool   `json:"show_offline"`
	ExtraArgs   string `json:"extra_args"`
}

type Config struct {
	Auth    Auth    `json:"auth"`
	Chat    Chat    `json:"chat"`
	Player  Player  `json:"player"`
	History History `json:"history"`
	Fzf     Fzf     `json:"fzf"`
}

func defaults() Config {
	return Config{
		Chat:    Chat{Command: "twt -c"},
		Player:  Player{Quality: "best"},
		History: History{Limit: 500},
		Fzf:     Fzf{ShowOffline: true},
	}
}

func path() string {
	return filepath.Join(ConfigDir, defaultConfigName)
}

// Load reads the config file, creating it with defaults if missing.
func Load() (*Config, error) {
	cfg := defaults()

	if err := os.MkdirAll(ConfigDir, 0755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	p := path()
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		// first run — write the skeleton and return defaults
		if err := writeDefaults(p, cfg); err != nil {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "created default config at %s\n", p)
		return &cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func writeDefaults(p string, cfg Config) error {
	f, err := os.Create(p)
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}
