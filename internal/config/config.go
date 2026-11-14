// Package config loads local configuration without external services.
package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

type Config struct {
	Listen        string `json:"listen"`
	Database      string `json:"database"`
	SessionTTL    string `json:"session_ttl"`
	SecureCookies bool   `json:"secure_cookies"`
}

func Default() Config {
	return Config{Listen: "127.0.0.1:8080", Database: "../werkstatt-hub-data/werkstatt-hub.db", SessionTTL: "12h"}
}

// Load uses defaults when path is empty; an explicitly named file must exist.
func Load(path string) (Config, error) {
	c := Default()
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return c, fmt.Errorf("Konfiguration öffnen: %w", err)
		}
		defer f.Close()
		dec := json.NewDecoder(io.LimitReader(f, 65537))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return c, fmt.Errorf("Konfiguration lesen: %w", err)
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			return c, fmt.Errorf("Konfiguration muss genau ein JSON-Objekt enthalten")
		}
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return c, fmt.Errorf("listen muss Host:Port sein: %w", err)
	}
	if strings.TrimSpace(c.Database) == "" || c.Database == ":memory:" || strings.HasPrefix(c.Database, "file:") {
		return c, fmt.Errorf("database muss ein lokaler Dateipfad sein")
	}
	ttl, err := time.ParseDuration(c.SessionTTL)
	if err != nil || ttl <= 0 || ttl > 7*24*time.Hour {
		return c, fmt.Errorf("session_ttl muss zwischen 0 und 168h liegen")
	}
	return c, nil
}
