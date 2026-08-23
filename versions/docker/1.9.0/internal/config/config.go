package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen             string `json:"listen"`
	DataDir            string `json:"data_dir"`
	AdminUser          string `json:"admin_user"`
	AdminPassword      string `json:"admin_password"`
	AdminPasswordHash  string `json:"admin_password_hash"`
	SecretKey          string `json:"secret_key"`
	SessionTTLHours    int    `json:"session_ttl_hours"`
	SearchTimeoutSec   int    `json:"search_timeout_sec"`
	SearchCacheTTLMin  int    `json:"search_cache_ttl_min"`
	PublicURL          string `json:"public_url"`
	MagnetLogDir       string `json:"magnet_log_dir"`
	MagnetLogMaxFileMB int    `json:"magnet_log_max_file_mb"`
	MagnetLogMaxFiles  int    `json:"magnet_log_max_files"`
}

func Default() Config {
	return Config{
		Listen:             ":8080",
		DataDir:            "./data",
		AdminUser:          "admin",
		AdminPassword:      "change-me",
		SessionTTLHours:    24,
		SearchTimeoutSec:   90,
		SearchCacheTTLMin:  5,
		MagnetLogMaxFileMB: 5,
		MagnetLogMaxFiles:  20,
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	exists := false
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return cfg, err
			}
		} else if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config: %w", err)
		} else {
			exists = true
		}
	}
	cfg.applyEnv()
	if cfg.SessionTTLHours <= 0 {
		cfg.SessionTTLHours = 24
	}
	if cfg.SearchTimeoutSec <= 0 {
		cfg.SearchTimeoutSec = 90
	}
	if cfg.SearchCacheTTLMin <= 0 {
		cfg.SearchCacheTTLMin = 5
	}
	if cfg.MagnetLogMaxFileMB <= 0 {
		cfg.MagnetLogMaxFileMB = 5
	}
	if cfg.MagnetLogMaxFiles < 2 {
		cfg.MagnetLogMaxFiles = 2
	}
	if err := cfg.ensureSecret(); err != nil {
		return cfg, err
	}
	if path != "" && !exists {
		if err := cfg.writeDefault(path); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func (c *Config) applyEnv() {
	if v := os.Getenv("SEARCHTERM_LISTEN"); v != "" {
		c.Listen = v
	}
	if v := os.Getenv("SEARCHTERM_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv("SEARCHTERM_ADMIN_USER"); v != "" {
		c.AdminUser = v
	}
	if v := os.Getenv("SEARCHTERM_ADMIN_PASSWORD"); v != "" {
		c.AdminPassword = v
	}
	if v := os.Getenv("SEARCHTERM_SECRET_KEY"); v != "" {
		c.SecretKey = v
	}
	if v := os.Getenv("SEARCHTERM_PUBLIC_URL"); v != "" {
		c.PublicURL = v
	}
	if v := os.Getenv("SEARCHTERM_MAGNET_LOG_DIR"); v != "" {
		c.MagnetLogDir = v
	}
	if v := os.Getenv("SEARCHTERM_MAGNET_LOG_MAX_FILE_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.MagnetLogMaxFileMB = n
		}
	}
	if v := os.Getenv("SEARCHTERM_MAGNET_LOG_MAX_FILES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.MagnetLogMaxFiles = n
		}
	}
}

func (c *Config) ensureSecret() error {
	if c.SecretKey != "" {
		return nil
	}
	_ = os.MkdirAll(c.DataDir, 0o755)
	keyFile := filepath.Join(c.DataDir, "secret.key")
	if data, err := os.ReadFile(keyFile); err == nil {
		c.SecretKey = strings.TrimSpace(string(data))
		return nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	c.SecretKey = hex.EncodeToString(buf)
	if err := os.WriteFile(keyFile, []byte(c.SecretKey), 0o600); err != nil {
		return err
	}
	return nil
}

func (c Config) writeDefault(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (c Config) SessionTTL() time.Duration {
	return time.Duration(c.SessionTTLHours) * time.Hour
}

func (c Config) SearchTimeout() time.Duration {
	return time.Duration(c.SearchTimeoutSec) * time.Second
}

func (c Config) SearchCacheTTL() time.Duration {
	return time.Duration(c.SearchCacheTTLMin) * time.Minute
}

func (c Config) MagnetLogPath() string {
	if c.MagnetLogDir != "" {
		return c.MagnetLogDir
	}
	return filepath.Join(c.DataDir, "magnet_log")
}

func (c Config) MagnetLogMaxBytes() int64 {
	return int64(c.MagnetLogMaxFileMB) * 1024 * 1024
}
