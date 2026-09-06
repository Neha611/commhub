// Package config loads and saves the non-secret half of CommHub's state.
// Credentials never appear here; they live behind internal/secrets.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/Neha611/commhub/internal/safe"
)

// Version is the config schema version. Bump it alongside a migration in Load.
const Version = 1

type Config struct {
	Version   int        `toml:"version"`
	Providers []Provider `toml:"provider"`
	Priority  Priority   `toml:"priority"`
	UI        UI         `toml:"ui"`
}

// Provider is one connected account. It records what CommHub is allowed to do,
// never how it authenticates.
type Provider struct {
	ID       string   `toml:"id"`       // "google:personal"
	Kind     string   `toml:"kind"`     // "google" | "fake"
	Label    string   `toml:"label"`    // "neha@gmail.com"
	Scopes   []string `toml:"scopes"`   // exactly what we hold
	Features []string `toml:"features"` // enabled features, drives Scopes
}

// Priority holds the scoring weights from SPEC §08. They live in config so
// users can tune the ranking that is the product's whole differentiator.
type Priority struct {
	MeetingImminent   float64 `toml:"meeting_imminent"` // <= 15 min
	MeetingSoon       float64 `toml:"meeting_soon"`     // <= 60 min
	UnreadDirect      float64 `toml:"unread_direct"`    // addressed only to me
	UnreadTo          float64 `toml:"unread_to"`        // my address in To:
	Starred           float64 `toml:"starred"`
	ThreadParticipant float64 `toml:"thread_participant"`
	UnreadCC          float64 `toml:"unread_cc"`
	UnreadBulk        float64 `toml:"unread_bulk"`
	RecencyWeight     float64 `toml:"recency_weight"`      // multiplier on exp decay
	RecencyHalfLife   float64 `toml:"recency_half_life_h"` // hours
	PenaltyBot        float64 `toml:"penalty_bot"`
	PenaltyMuted      float64 `toml:"penalty_muted"`
	PenaltyList       float64 `toml:"penalty_list"`
}

type UI struct {
	Theme           string `toml:"theme"`
	RetentionDays   int    `toml:"retention_days"` // body cache window, SEC-05
	PollSeconds     int    `toml:"poll_seconds"`
	InitialSyncDays int    `toml:"initial_sync_days"`
}

func Defaults() Config {
	return Config{
		Version: Version,
		Priority: Priority{
			MeetingImminent: 120, MeetingSoon: 90,
			UnreadDirect: 100, UnreadTo: 80,
			Starred: 70, ThreadParticipant: 60,
			UnreadCC: 45, UnreadBulk: 20,
			RecencyWeight: 40, RecencyHalfLife: 6,
			PenaltyBot: 30, PenaltyMuted: 25, PenaltyList: 10,
		},
		UI: UI{Theme: "auto", RetentionDays: 7, PollSeconds: 45, InitialSyncDays: 30},
	}
}

// Provider lookup helpers.

func (c *Config) Find(id string) (Provider, bool) {
	for _, p := range c.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

func (c *Config) Upsert(p Provider) {
	for i := range c.Providers {
		if c.Providers[i].ID == p.ID {
			c.Providers[i] = p
			return
		}
	}
	c.Providers = append(c.Providers, p)
}

func (c *Config) Remove(id string) bool {
	for i := range c.Providers {
		if c.Providers[i].ID == id {
			c.Providers = append(c.Providers[:i], c.Providers[i+1:]...)
			return true
		}
	}
	return false
}

// Paths.

func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "commhub"), nil
}

func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.toml"), nil
}

func DataDir() (string, error) {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "commhub"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "commhub"), nil
}

func StateDir() (string, error) {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "commhub"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "commhub"), nil
}

func DBPath() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "commhub.db"), nil
}

// Load reads the config, returning defaults when none exists yet. A missing
// config is the normal first-run state, not an error.
func Load() (Config, error) {
	cfg := Defaults()
	path, err := Path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Version > Version {
		return cfg, fmt.Errorf("config version %d is newer than this build supports (%d)", cfg.Version, Version)
	}
	// Fill any weight left at zero by an older or partial config.
	d := Defaults()
	if cfg.Priority.RecencyHalfLife == 0 {
		cfg.Priority = d.Priority
	}
	if cfg.UI.PollSeconds == 0 {
		cfg.UI = d.UI
	}
	cfg.Version = Version
	return cfg, nil
}

// Save writes the config with 0600 applied at creation (SEC-04).
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	cfg.Version = Version
	var buf []byte
	b := &tomlBuffer{}
	if err := toml.NewEncoder(b).Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	buf = b.Bytes()
	return safe.WriteFileSecure(path, buf)
}

type tomlBuffer struct{ b []byte }

func (t *tomlBuffer) Write(p []byte) (int, error) { t.b = append(t.b, p...); return len(p), nil }
func (t *tomlBuffer) Bytes() []byte               { return t.b }
