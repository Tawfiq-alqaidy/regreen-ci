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

// The raw structs hold string-valued keys as yaml.Node so that the YAML type
// of each value can be checked: the library would otherwise stringify ints,
// bools and floats, and silently drop null list elements. A zero Node means
// the key was omitted.
type rawRepo struct {
	Checks []rawCheck `yaml:"checks"`
	Policy *rawPolicy `yaml:"policy"`
}

type rawCheck struct {
	Name yaml.Node `yaml:"name"`
	Run  yaml.Node `yaml:"run"`
	Fix  yaml.Node `yaml:"fix"`
}

type rawPolicy struct {
	ProtectedFiles    []yaml.Node `yaml:"protected_files"`
	ForbiddenPatterns []yaml.Node `yaml:"forbidden_patterns"`
	MaxChangedLines   *int        `yaml:"max_changed_lines"`
	MaxDeletedLines   *int        `yaml:"max_deleted_lines"`
	MaxAttempts       *int        `yaml:"max_attempts"`
	EditScope         yaml.Node   `yaml:"edit_scope"`
}

// stringState is how a yaml.Node relates to a string-valued key.
type stringState int

const (
	stringAbsent stringState = iota // key omitted
	stringNull                      // explicit null
	stringOK                        // a YAML string scalar
	stringBad                       // anything else
)

// stringValue classifies n and returns its text when it is a string scalar,
// that is, a quoted or block scalar, or a plain scalar YAML resolves to !!str.
func stringValue(n *yaml.Node) (string, stringState) {
	switch {
	case n.Kind == 0:
		return "", stringAbsent
	case n.Kind != yaml.ScalarNode:
		return "", stringBad
	case n.ShortTag() == "!!null":
		return "", stringNull
	case n.ShortTag() == "!!str":
		return n.Value, stringOK
	}
	return "", stringBad
}

// requireString returns the text of n, recording a problem at path when n is
// present but not a string. Null counts as present only when nullIsBad.
func requireString(n *yaml.Node, path string, nullIsBad bool, problems *[]error) (string, stringState) {
	v, st := stringValue(n)
	if st == stringBad || (st == stringNull && nullIsBad) {
		*problems = append(*problems, fmt.Errorf("%s: must be a string (quote the value)", path))
		return "", stringBad
	}
	return v, st
}

// stringList converts a list of nodes, reporting non-strings, nulls and any
// problem check finds in a string. Every problem names the element's index in
// the file's own list, so a bad earlier element cannot shift later indexes.
func stringList(nodes []yaml.Node, path string, check func(string) error, problems *[]error) []string {
	var out []string
	for i := range nodes {
		at := fmt.Sprintf("%s[%d]", path, i)
		v, st := requireString(&nodes[i], at, true, problems)
		if st != stringOK {
			continue
		}
		if err := check(v); err != nil {
			*problems = append(*problems, fmt.Errorf("%s: %w", at, err))
		}
		out = append(out, v)
	}
	return out
}

// checkProtectedPattern validates one protected_files entry.
func checkProtectedPattern(p string) error {
	if err := policy.ValidatePattern(p); err != nil {
		return fmt.Errorf("%q: %w", p, err)
	}
	return nil
}

// checkForbiddenPattern validates one forbidden_patterns entry.
func checkForbiddenPattern(p string) error {
	switch {
	case p == "":
		return errors.New("must not be empty")
	case strings.ContainsAny(p, "\r\n"):
		return fmt.Errorf("%q must not contain a newline", p)
	}
	return nil
}

// rejectAnchors reports any anchor, alias, or merge key in the node tree.
func rejectAnchors(n *yaml.Node) bool {
	if n.Anchor != "" || n.Kind == yaml.AliasNode || n.ShortTag() == "!!merge" {
		return true
	}
	for _, c := range n.Content {
		if rejectAnchors(c) {
			return true
		}
	}
	return false
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

	var root yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&root); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if rejectAnchors(&root) {
		return nil, fmt.Errorf("%s: anchors and aliases are not allowed", what)
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
	return parseRepo(data, RepoFileName)
}

// parseRepo is ParseRepo with the name used in error messages: the file name,
// or the path when loading from disk.
func parseRepo(data []byte, what string) (*Repo, error) {
	raw, err := decodeStrict[rawRepo](data, what)
	if errors.Is(err, errEmptyDocument) {
		return nil, errors.New(what + " is empty")
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
		for i, p := range problems {
			problems[i] = fmt.Errorf("%s: %w", what, p)
		}
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
	for i := range raw {
		rc := &raw[i]
		var c Check
		name, st := requireString(&rc.Name, fmt.Sprintf("checks[%d].name", i), true, &problems)
		switch {
		case st == stringBad:
		case st != stringOK || strings.TrimSpace(name) == "":
			problems = append(problems, fmt.Errorf("checks[%d].name: required", i))
		default:
			c.Name = name
			switch {
			case !checkNamePattern.MatchString(c.Name):
				problems = append(problems, fmt.Errorf(
					"checks[%d].name: %q must start with a letter or digit and contain only letters, digits, '.', '_' and '-'", i, c.Name))
			case seen[c.Name]:
				problems = append(problems, fmt.Errorf("checks[%d].name: %q is used by more than one check", i, c.Name))
			}
			seen[c.Name] = true
		}
		run, st := requireString(&rc.Run, fmt.Sprintf("checks[%d].run", i), true, &problems)
		switch {
		case st == stringBad:
		case st != stringOK || strings.TrimSpace(run) == "":
			problems = append(problems, fmt.Errorf("checks[%d].run: required", i))
		default:
			c.Run = run
			if strings.ContainsRune(run, 0) {
				problems = append(problems, fmt.Errorf("checks[%d].run: must not contain a NUL byte", i))
			}
		}
		fix, st := requireString(&rc.Fix, fmt.Sprintf("checks[%d].fix", i), false, &problems)
		if st == stringOK {
			c.Fix = fix
			if strings.TrimSpace(fix) == "" {
				problems = append(problems, fmt.Errorf("checks[%d].fix: must not be empty when present", i))
			}
			if strings.ContainsRune(fix, 0) {
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
	rules.ProtectedFiles = stringList(raw.ProtectedFiles, "policy.protected_files", checkProtectedPattern, &problems)
	rules.ForbiddenPatterns = stringList(raw.ForbiddenPatterns, "policy.forbidden_patterns", checkForbiddenPattern, &problems)

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

	if scope, st := requireString(&raw.EditScope, "policy.edit_scope", false, &problems); st == stringOK {
		switch es := policy.EditScope(scope); es {
		case policy.ScopeFilesInErrors, policy.ScopeAny:
			rules.EditScope = es
		default:
			problems = append(problems, fmt.Errorf("policy.edit_scope: %q must be %q or %q",
				scope, policy.ScopeFilesInErrors, policy.ScopeAny))
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
	return parseRepo(data, path)
}
