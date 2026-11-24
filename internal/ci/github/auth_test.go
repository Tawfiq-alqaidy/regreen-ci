package github

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func ghReturning(tok string, err error) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return tok, err }
}

func ghMustNotRun(t *testing.T) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		t.Helper()
		t.Error("gh must not be consulted")
		return "", nil
	}
}

func TestTokenLookupOrder(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		env  map[string]string
		gh   func(context.Context) (string, error)
		want string
	}{
		{"GH_TOKEN wins", map[string]string{"GH_TOKEN": "test-gh", "GITHUB_TOKEN": "test-github"}, ghMustNotRun(t), "test-gh"},
		{"GITHUB_TOKEN when GH_TOKEN empty", map[string]string{"GH_TOKEN": "", "GITHUB_TOKEN": "test-github"}, ghMustNotRun(t), "test-github"},
		{"GITHUB_TOKEN when GH_TOKEN whitespace", map[string]string{"GH_TOKEN": " \t\n", "GITHUB_TOKEN": "test-github"}, ghMustNotRun(t), "test-github"},
		{"value is trimmed", map[string]string{"GH_TOKEN": " test-gh\t"}, ghMustNotRun(t), "test-gh"},
		{"gh fallback", nil, ghReturning("test-from-gh\n", nil), "test-from-gh"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := token(ctx, envOf(tc.env), tc.gh)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("token = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTokenGhErrors(t *testing.T) {
	ctx := context.Background()
	t.Run("gh error is ErrNoToken", func(t *testing.T) {
		_, err := token(ctx, envOf(nil), ghReturning("", ErrNoToken))
		if !errors.Is(err, ErrNoToken) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("empty gh output is ErrNoToken", func(t *testing.T) {
		_, err := token(ctx, envOf(nil), ghReturning("  \n", nil))
		if !errors.Is(err, ErrNoToken) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("invalid gh output names gh, not the value", func(t *testing.T) {
		_, err := token(ctx, envOf(nil), ghReturning("test-bad token", nil))
		if err == nil || !strings.Contains(err.Error(), "gh auth token") || strings.Contains(err.Error(), "test-bad") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestTokenInvalidValue(t *testing.T) {
	for _, v := range []string{"test-has space", "test-has\ttab", "test-in\x00ner", "test-in\nner"} {
		_, err := token(context.Background(), envOf(map[string]string{"GH_TOKEN": v}), ghMustNotRun(t))
		if err == nil {
			t.Fatalf("value %q accepted", v)
		}
		if !strings.Contains(err.Error(), "invalid token in GH_TOKEN") {
			t.Fatalf("error does not name the source: %v", err)
		}
		if strings.Contains(err.Error(), "test-") {
			t.Fatalf("error leaks the value: %v", err)
		}
	}
	_, err := token(context.Background(), envOf(map[string]string{"GITHUB_TOKEN": "a b"}), ghMustNotRun(t))
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("err = %v", err)
	}
}

func writeFakeGh(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script gh stub is not portable to Windows")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o700); err != nil { //nolint:gosec // executable test stub
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestTokenRealGhRunner(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	writeFakeGh(t, "#!/bin/sh\n[ \"$1 $2 $3 $4\" = \"auth token --hostname github.com\" ] || exit 2\necho '  test-fake-gh-token  '\n")
	got, err := Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "test-fake-gh-token" {
		t.Fatalf("Token = %q", got)
	}
}

func TestTokenGhNonZeroExit(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	writeFakeGh(t, "#!/bin/sh\necho 'test-secret-output'\nexit 1\n")
	_, err := Token(context.Background())
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "test-secret-output") || !strings.Contains(err.Error(), "gh auth token failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenNoGhOnPath(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", "")
	_, err := Token(context.Background())
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("err = %v", err)
	}
}
