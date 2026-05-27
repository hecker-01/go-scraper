package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds all user-configurable settings.
type Config struct {
	OutputDir      string `toml:"output_dir"`
	DownloadMedia  bool   `toml:"download_media"`
	MaxMediaSizeMB int    `toml:"max_media_size_mb"` // 0 = no cap
	DomainDepth    int    `toml:"domain_depth"`      // 0 = stay on starting domain only, 1 = one domain away, etc.
	MaxDepth       int    `toml:"max_depth"`         // 0 = unlimited page depth
}

// DefaultConfig returns sensible out-of-the-box settings.
func DefaultConfig() Config {
	return Config{
		OutputDir:      "~/Downloads/go-scraper",
		DownloadMedia:  true,
		MaxMediaSizeMB: 100,
		DomainDepth:    0,
		MaxDepth:       0,
	}
}

// FilePath returns the OS config dir path for the config file.
func (c Config) FilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "go-scraper", "config.toml"), nil
}

// LoadConfig reads the config file.
// Returns (cfg, existed, err). If the file does not exist, existed=false and
// cfg is a zero value - caller should use DefaultConfig().
func LoadConfig() (Config, bool, error) {
	var cfg Config
	path, err := cfg.FilePath()
	if err != nil {
		return cfg, false, err
	}

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return cfg, false, nil
	}

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, true, err
	}
	return cfg, true, nil
}

// Save writes the config to the OS config dir, creating dirs as needed.
func (c Config) Save() error {
	path, err := c.FilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}
