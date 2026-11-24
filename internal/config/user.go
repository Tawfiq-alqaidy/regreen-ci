package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v4"
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
	Agent yaml.Node     `yaml:"agent"`
	Watch *rawUserWatch `yaml:"watch"`
}

type rawUserWatch struct {
	Delivery yaml.Node `yaml:"delivery"`
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
	raw, err := decodeStrict[rawUser](data, path)
	if errors.Is(err, errEmptyDocument) {
		return &u, nil
	}
	if err != nil {
		return nil, err
	}

	var problems []error
	if agent, st := requireString(&raw.Agent, "agent", false, &problems); st == stringOK {
		switch agent {
		case "claude-code", "codex", "other":
			u.Agent = agent
		default:
			problems = append(problems, fmt.Errorf("agent: %q must be one of claude-code, codex, other", agent))
		}
	}
	if raw.Watch != nil {
		if v, st := requireString(&raw.Watch.Delivery, "watch.delivery", false, &problems); st == stringOK {
			switch d := Delivery(v); d {
			case DeliveryNotify, DeliverySession, DeliveryHeadless:
				u.Watch.Delivery = d
			default:
				problems = append(problems, fmt.Errorf("watch.delivery: %q must be one of notify, session, headless", d))
			}
		}
	}
	if len(problems) > 0 {
		for i, p := range problems {
			problems[i] = fmt.Errorf("%s: %w", path, p)
		}
		return nil, errors.Join(problems...)
	}
	return &u, nil
}
