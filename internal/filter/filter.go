// Package filter provides the filter interface and implementations for
// filtering MongoDB LogV2 log events.
package filter

import (
	"time"

	"github.com/sriharip316/mtools-go/internal/logevent"
)

// Filter is the interface all log filters must implement.
type Filter interface {
	// Accept returns true if the log event passes this filter.
	Accept(ev *logevent.LogEvent) bool
	// SkipRemaining returns true if no more lines can possibly match.
	SkipRemaining() bool
}

// StartLimiter is optionally implemented by filters that can provide
// a start datetime for fast-forwarding log files.
type StartLimiter interface {
	StartLimit() time.Time
}

// Span represents a byte offset substring [Start, End] in a line to highlight.
type Span struct {
	Start int
	End   int
}

// Highlighter is optionally implemented by filters that can identify
// matching substrings in output lines for terminal colorization.
type Highlighter interface {
	HighlightSpans(line string, ev *logevent.LogEvent) []Span
}
