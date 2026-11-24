// Package failure is the normalized model of a failed CI step: it sanitizes
// and caps untrusted log text, extracts the known files the text mentions,
// and maps the step to a configured check. It does no I/O.
//
// Log text is attacker-influenced. Nothing here executes or interprets it as
// an instruction; it is only compared, trimmed and returned as data.
package failure

// Limits caps the size of what is kept from a log.
type Limits struct{ MaxLines, MaxBytes, MaxLineBytes, MaxFiles int }

// DefaultLimits returns the limits used when a field is not positive.
func DefaultLimits() Limits {
	return Limits{MaxLines: 200, MaxBytes: 16384, MaxLineBytes: 2000, MaxFiles: 100}
}
