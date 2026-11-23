package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Delivery says how a watch hands a failure to the agent.
type Delivery string

// The supported delivery modes.
const (
	DeliveryNotify   Delivery = "notify"   // default
	DeliverySession  Delivery = "session"  // push to a running agent session
	DeliveryHeadless Delivery = "headless" // start a new headless run
)

// User is the per-user configuration.
type User struct {
	Agent string // "", "claude-code", "codex", or "other"
	Watch struct {
		Delivery Delivery
	}
}

type rawUser struct {
	Agent *string       `yaml:"agent"`
	Watch *rawUserWatch `yaml:"watch"`
}

type rawUserWatch struct {
	Delivery *string `yaml:"delivery"`
}

// DefaultUser returns the config used when no user config exists.
func DefaultUser() User {
	var u User
	u.Watch.Delivery = DeliveryNotify
	return u
}

// UserConfigPath returns <os.UserConfigDir()>/regreen/config.yml.
func UserConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "regreen", "config.yml"), nil
}

// LoadUser reads the user config at path. A missing file is not an error:
// it returns DefaultUser().
func LoadUser(ctx context.Context, path string) (*User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	u := DefaultUser()
	data, err := readLimited(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &u, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	raw, err := decodeStrict[rawUser](data, "config.yml")
	if errors.Is(err, errEmptyDocument) {
		return &u, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if raw.Agent != nil {
		switch *raw.Agent {
		case "claude-code", "codex", "other":
			u.Agent = *raw.Agent
		default:
			return nil, fmt.Errorf("%s: agent: %q must be one of claude-code, codex, other", path, *raw.Agent)
		}
	}
	if raw.Watch != nil && raw.Watch.Delivery != nil {
		switch d := Delivery(*raw.Watch.Delivery); d {
		case DeliveryNotify, DeliverySession, DeliveryHeadless:
			u.Watch.Delivery = d
		default:
			return nil, fmt.Errorf("%s: watch.delivery: %q must be one of notify, session, headless", path, d)
		}
	}
	return &u, nil
}
