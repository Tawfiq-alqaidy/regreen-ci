package github

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
)

var idPattern = regexp.MustCompile(`^gha-(\d{1,19})-(\d{1,19})-(\d{1,9})$`)

// encodeID builds the provider-scoped failure ID "gha-<run>-<job>-<step>".
func encodeID(runID, jobID int64, step int) string {
	return fmt.Sprintf("gha-%d-%d-%d", runID, jobID, step)
}

// parseID is the inverse of encodeID. It accepts nothing else: IDs come
// from callers and are never trusted.
func parseID(id string) (runID, jobID int64, step int, err error) {
	m := idPattern.FindStringSubmatch(id)
	if m == nil {
		return 0, 0, 0, fmt.Errorf("invalid failure id %q", id)
	}
	runID, err = strconv.ParseInt(m[1], 10, 64)
	if err != nil || runID <= 0 {
		return 0, 0, 0, fmt.Errorf("invalid failure id %q: bad run id", id)
	}
	jobID, err = strconv.ParseInt(m[2], 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid failure id %q: bad job id", id)
	}
	n, err := strconv.ParseInt(m[3], 10, 32)
	if err != nil || n > math.MaxInt32 {
		return 0, 0, 0, fmt.Errorf("invalid failure id %q: bad step number", id)
	}
	return runID, jobID, int(n), nil
}
