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
	for _, body := range []string{`{"session_ttl":"0s"}`, `{"database":":memory:"}`, `{"listen":"8080"}`, `{"unknown":true}`, `{} {}`, `{"session_ttl":"169h"}`} {
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
