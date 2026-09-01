package filter

import (
	"github.com/sriharip316/mtools-go/internal/logevent"
)

// SlowFilter accepts log events with duration >= threshold (--slow).
type SlowFilter struct {
	ThresholdMs int
}

// Accept returns true if the event's duration meets or exceeds the threshold.
func (f *SlowFilter) Accept(ev *logevent.LogEvent) bool {
	return ev.Duration != nil && *ev.Duration >= f.ThresholdMs
}

// SkipRemaining always returns false — slow queries can appear anywhere.
func (f *SlowFilter) SkipRemaining() bool { return false }

// HighlightSpans returns spans for durationMillis in line.
func (f *SlowFilter) HighlightSpans(line string, ev *logevent.LogEvent) []Span {
	return FindDurationSpans(line)
}
