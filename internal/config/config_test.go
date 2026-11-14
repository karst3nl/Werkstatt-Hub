package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguration(t *testing.T) {
	if _, err := Load(""); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"session_ttl":"0s"}`, `{"database":":memory:"}`, `{"listen":"8080"}`, `{"unknown":true}`, `{} {}`, `{"session_ttl":"169h"}`, `{"priority_min":0}`, `{"priority_max":1}`, `{"priority_max":101}`, `{"priority_min":5,"priority_max":4}`} {
		t.Run(body, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(p, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(p); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing explicit file accepted")
	}
}

func TestPriorityScaleBackwardCompatibleAndConfigurable(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		min, max   int
	}{
		{"legacy configuration", `{"listen":"127.0.0.1:8080","session_ttl":"12h"}`, 1, 5},
		{"shared custom scale", `{"priority_min":2,"priority_max":8}`, 2, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.PriorityMin != tc.min || cfg.PriorityMax != tc.max {
				t.Fatalf("priority scale = %d..%d, want %d..%d", cfg.PriorityMin, cfg.PriorityMax, tc.min, tc.max)
			}
		})
	}
}
