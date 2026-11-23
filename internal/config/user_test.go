package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeUser(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadUserMissingFileIsDefaults(t *testing.T) {
	got, err := LoadUser(context.Background(), filepath.Join(t.TempDir(), "absent.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if *got != DefaultUser() {
		t.Errorf("got %+v, want defaults", *got)
	}
}

func TestLoadUserEmptyFileIsDefaults(t *testing.T) {
	for _, content := range []string{"", "# only a comment\n", "null\n"} {
		got, err := LoadUser(context.Background(), writeUser(t, content))
		if err != nil {
			t.Fatalf("%q: %v", content, err)
		}
		if *got != DefaultUser() {
			t.Errorf("%q: got %+v, want defaults", content, *got)
		}
	}
}

func TestLoadUserCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadUser(ctx, writeUser(t, "")); err == nil {
		t.Error("expected context error")
	}
}

func TestLoadUser(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		agent    string
		delivery Delivery
	}{
		{"full", "agent: claude-code\nwatch:\n  delivery: session\n", "claude-code", DeliverySession},
		{"only agent", "agent: codex\n", "codex", DeliveryNotify},
		{"only delivery", "watch:\n  delivery: headless\n", "", DeliveryHeadless},
		{"agent other", "agent: other\n", "other", DeliveryNotify},
		{"delivery notify", "watch:\n  delivery: notify\n", "", DeliveryNotify},
		{"empty watch", "watch: {}\n", "", DeliveryNotify},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadUser(context.Background(), writeUser(t, tt.yaml))
			if err != nil {
				t.Fatal(err)
			}
			if got.Agent != tt.agent || got.Watch.Delivery != tt.delivery {
				t.Errorf("got %+v", *got)
			}
		})
	}
}

func TestLoadUserInvalid(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"unknown key", "colour: red\n", "colour"},
		{"unknown nested key", "watch:\n  mode: x\n", "mode"},
		{"bad agent", "agent: gemini\n", "claude-code, codex, other"},
		{"bad delivery", "watch:\n  delivery: email\n", "notify, session, headless"},
		{"second document", "agent: codex\n---\nagent: other\n", "document"},
		{"malformed", "agent: [\n", "config.yml"},
		{"oversized", "#" + strings.Repeat("a", maxConfigBytes), "too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadUser(context.Background(), writeUser(t, tt.yaml))
			if err == nil {
				t.Fatalf("expected error, got %+v", got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestUserConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	got, err := UserConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) || !strings.HasSuffix(got, filepath.Join("regreen", "config.yml")) {
		t.Errorf("path = %q", got)
	}
}
