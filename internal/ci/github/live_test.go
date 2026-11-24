package github

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tawfiq-alqaidy/regreen-ci/internal/ci"
)

// TestLive talks to the real GitHub API. It never runs in CI: set
// REGREEN_LIVE_REPO (owner/name) and REGREEN_LIVE_COMMIT to opt in.
func TestLive(t *testing.T) {
	repoName := os.Getenv("REGREEN_LIVE_REPO")
	commit := os.Getenv("REGREEN_LIVE_COMMIT")
	if repoName == "" || commit == "" {
		t.Skip("set REGREEN_LIVE_REPO and REGREEN_LIVE_COMMIT to run against GitHub")
	}
	owner, name, ok := strings.Cut(repoName, "/")
	if !ok {
		t.Fatalf("REGREEN_LIVE_REPO must be owner/name, got %q", repoName)
	}
	repo := ci.Repo{Host: hostGitHub, Owner: owner, Name: name}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tok, err := Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(tok)
	if err != nil {
		t.Fatal(err)
	}
	st, err := p.CommitStatus(ctx, repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("status of %s: %s, %d failed step(s)", commit, st.Status, len(st.Failed))
	for _, f := range st.Failed {
		log, err := p.FailureLog(ctx, repo, f.ID)
		if err != nil {
			t.Errorf("%s: %v", f.ID, err)
			continue
		}
		lines := strings.SplitAfter(log, "\n")
		if len(lines) > 20 {
			lines = lines[:20]
		}
		t.Logf("%s (%s / %s / %s), first lines:\n%s", f.ID, f.Workflow, f.Job, f.Step, strings.Join(lines, ""))
	}
}
