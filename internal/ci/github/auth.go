// Package github is the GitHub Actions adapter for ci.Provider.
package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

// ErrNoToken means no token was found in the environment or via gh.
var ErrNoToken = errors.New("no GitHub token found: set GH_TOKEN or GITHUB_TOKEN, or run `gh auth login`")

const ghTimeout = 10 * time.Second

// Token returns a GitHub token from GH_TOKEN, then GITHUB_TOKEN, then
// `gh auth token --hostname github.com`. It never logs the token.
func Token(ctx context.Context) (string, error) {
	return token(ctx, os.Getenv, runGh)
}

func token(ctx context.Context, getenv func(string) string, gh func(context.Context) (string, error)) (string, error) {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		v := strings.TrimSpace(getenv(name))
		if v == "" {
			continue
		}
		if !validToken(v) {
			return "", fmt.Errorf("invalid token in %s: contains whitespace or control characters", name)
		}
		return v, nil
	}
	out, err := gh(ctx)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(out)
	if v == "" {
		return "", fmt.Errorf("gh auth token returned nothing: %w", ErrNoToken)
	}
	if !validToken(v) {
		return "", errors.New("invalid token from gh auth token: contains whitespace or control characters")
	}
	return v, nil
}

func validToken(v string) bool {
	for _, r := range v {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func runGh(ctx context.Context) (string, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", ErrNoToken
	}
	ctx, cancel := context.WithTimeout(ctx, ghTimeout)
	defer cancel()
	var stdout bytes.Buffer
	// path comes from LookPath and every argument is a constant.
	cmd := exec.CommandContext(ctx, path, "auth", "token", "--hostname", "github.com") //nolint:gosec // G204: fixed arguments, binary resolved by LookPath
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh auth token failed: %w", ErrNoToken)
	}
	return stdout.String(), nil
}
