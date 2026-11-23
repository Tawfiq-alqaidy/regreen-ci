// Package config reads and strictly validates regreen.yml and the per-user
// config, and turns the repo config into a policy.Rules.
package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/Tawfiq-alqaidy/regreen-ci/internal/policy"
)

// RepoFileName is the name of the repo config file at the repository root.
const RepoFileName = "regreen.yml"

// maxConfigBytes caps the size of any config file (1 MiB).
const maxConfigBytes = 1 << 20

// maxPolicyLimit is the largest value accepted for any numeric policy key.
const maxPolicyLimit = 1_000_000

// Check is one named check of the repository.
type Check struct {
	Name string // unique within the file
	Run  string // command that performs the check
	Fix  string // optional deterministic fix command; "" when absent
}

// Repo is a parsed and validated regreen.yml.
type Repo struct {
	Checks []Check
	Policy policy.Rules // defaults applied, validated
}

type rawRepo struct {
	Checks []rawCheck `yaml:"checks"`
	Policy *rawPolicy `yaml:"policy"`
}

type rawCheck struct {
	Name *string `yaml:"name"`
	Run  *string `yaml:"run"`
	Fix  *string `yaml:"fix"`
}

type rawPolicy struct {
	ProtectedFiles    []string `yaml:"protected_files"`
	ForbiddenPatterns []string `yaml:"forbidden_patterns"`
	MaxChangedLines   *int     `yaml:"max_changed_lines"`
	MaxDeletedLines   *int     `yaml:"max_deleted_lines"`
	MaxAttempts       *int     `yaml:"max_attempts"`
	EditScope         *string  `yaml:"edit_scope"`
}

var checkNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// errEmptyDocument means the input held no YAML document, or a null one.
var errEmptyDocument = errors.New("empty document")

// decodeStrict decodes exactly one YAML document into T, rejecting unknown
// keys, input over maxConfigBytes, and any second document. what names the
// file in error messages. Empty or null input yields errEmptyDocument.
func decodeStrict[T any](data []byte, what string) (*T, error) {
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("%s is too large (limit %d bytes)", what, maxConfigBytes)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc *T
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errEmptyDocument
		}
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if doc == nil {
		return nil, errEmptyDocument
	}
	var extra any
	switch err := dec.Decode(&extra); {
	case err == nil:
		return nil, fmt.Errorf("%s must contain exactly one YAML document", what)
	case !errors.Is(err, io.EOF):
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return doc, nil
}

// readLimited reads at most maxConfigBytes+1 bytes of path, so an oversized
// file is detected without loading all of it.
func readLimited(path string) ([]byte, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
}

// ParseRepo parses and validates the contents of a regreen.yml.
// Callers that must not trust the working tree (regreen verify) pass bytes
// read from the base branch. The run and fix commands are stored as written
// and never executed or interpreted here.
func ParseRepo(data []byte) (*Repo, error) {
	raw, err := decodeStrict[rawRepo](data, RepoFileName)
	if errors.Is(err, errEmptyDocument) {
		return nil, errors.New(RepoFileName + " is empty")
	}
	if err != nil {
		return nil, err
	}

	var problems []error
	checks, checkProblems := validateChecks(raw.Checks)
	problems = append(problems, checkProblems...)
	rules, policyProblems := buildRules(raw.Policy)
	problems = append(problems, policyProblems...)
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return &Repo{Checks: checks, Policy: rules}, nil
}

func validateChecks(raw []rawCheck) ([]Check, []error) {
	if len(raw) == 0 {
		return nil, []error{errors.New("checks: at least one check is required")}
	}
	var problems []error
	checks := make([]Check, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for i, rc := range raw {
		var c Check
		if rc.Name == nil || strings.TrimSpace(*rc.Name) == "" {
			problems = append(problems, fmt.Errorf("checks[%d].name: required", i))
		} else {
			c.Name = *rc.Name
			switch {
			case !checkNamePattern.MatchString(c.Name):
				problems = append(problems, fmt.Errorf(
					"checks[%d].name: %q must start with a letter or digit and contain only letters, digits, '.', '_' and '-'", i, c.Name))
			case seen[c.Name]:
				problems = append(problems, fmt.Errorf("checks[%d].name: %q is used by more than one check", i, c.Name))
			}
			seen[c.Name] = true
		}
		if rc.Run == nil || strings.TrimSpace(*rc.Run) == "" {
			problems = append(problems, fmt.Errorf("checks[%d].run: required", i))
		} else {
			c.Run = *rc.Run
			if strings.ContainsRune(c.Run, 0) {
				problems = append(problems, fmt.Errorf("checks[%d].run: must not contain a NUL byte", i))
			}
		}
		if rc.Fix != nil {
			c.Fix = *rc.Fix
			if strings.TrimSpace(c.Fix) == "" {
				problems = append(problems, fmt.Errorf("checks[%d].fix: must not be empty when present", i))
			}
			if strings.ContainsRune(c.Fix, 0) {
				problems = append(problems, fmt.Errorf("checks[%d].fix: must not contain a NUL byte", i))
			}
		}
		checks = append(checks, c)
	}
	return checks, problems
}

func buildRules(raw *rawPolicy) (policy.Rules, []error) {
	rules := policy.DefaultRules()
	if raw == nil {
		return rules, nil
	}
	var problems []error
	for i, p := range raw.ProtectedFiles {
		if err := policy.ValidatePattern(p); err != nil {
			problems = append(problems, fmt.Errorf("policy.protected_files[%d]: %q: %w", i, p, err))
		}
	}
	for i, p := range raw.ForbiddenPatterns {
		switch {
		case p == "":
			problems = append(problems, fmt.Errorf("policy.forbidden_patterns[%d]: must not be empty", i))
		case strings.ContainsAny(p, "\r\n"):
			problems = append(problems, fmt.Errorf("policy.forbidden_patterns[%d]: %q must not contain a newline", i, p))
		}
	}
	rules.ProtectedFiles = raw.ProtectedFiles
	rules.ForbiddenPatterns = raw.ForbiddenPatterns

	setLimit := func(field string, v *int, lowest int, dst *int) {
		if v == nil {
			return
		}
		if *v < lowest || *v > maxPolicyLimit {
			problems = append(problems, fmt.Errorf("policy.%s: %d must be between %d and %d", field, *v, lowest, maxPolicyLimit))
			return
		}
		*dst = *v
	}
	setLimit("max_changed_lines", raw.MaxChangedLines, 1, &rules.MaxChangedLines)
	setLimit("max_deleted_lines", raw.MaxDeletedLines, 0, &rules.MaxDeletedLines)
	setLimit("max_attempts", raw.MaxAttempts, 1, &rules.MaxAttempts)

	if raw.EditScope != nil {
		switch scope := policy.EditScope(*raw.EditScope); scope {
		case policy.ScopeFilesInErrors, policy.ScopeAny:
			rules.EditScope = scope
		default:
			problems = append(problems, fmt.Errorf("policy.edit_scope: %q must be %q or %q",
				*raw.EditScope, policy.ScopeFilesInErrors, policy.ScopeAny))
		}
	}
	return rules, problems
}

// LoadRepo reads path and calls ParseRepo.
func LoadRepo(ctx context.Context, path string) (*Repo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := readLimited(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	repo, err := ParseRepo(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return repo, nil
}
