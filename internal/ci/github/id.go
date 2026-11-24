package github

import (
	"fmt"
	"regexp"
	"strconv"
)

// maxIDBytes bounds a failure ID. The longest ID encodeID can produce,
// "gha-" and three 19-digit numbers with two dashes, is 63 bytes.
const maxIDBytes = 64

var idPattern = regexp.MustCompile(`^gha-(\d{1,19})-(\d{1,19})-(\d{1,19})$`)

// encodeID builds the provider-scoped failure ID "gha-<run>-<job>-<step>".
// parseID accepts every ID it builds from non-negative numbers.
func encodeID(runID, jobID int64, step int) string {
	return fmt.Sprintf("gha-%d-%d-%d", runID, jobID, step)
}

// parseID is the inverse of encodeID. It accepts nothing else: IDs come
// from callers and are never trusted. Errors quote at most maxIDBytes of the
// input.
func parseID(id string) (runID, jobID int64, step int, err error) {
	if len(id) > maxIDBytes {
		return 0, 0, 0, fmt.Errorf("invalid failure id: longer than %d bytes", maxIDBytes)
	}
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
	n, err := strconv.ParseInt(m[3], 10, strconv.IntSize)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid failure id %q: bad step number", id)
	}
	return runID, jobID, int(n), nil
}
