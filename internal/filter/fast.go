package filter

import (
	"github.com/sriharip316/mtools-go/internal/logevent"
)

// FastFilter accepts log events with duration <= threshold (--fast).
type FastFilter struct {
	ThresholdMs int
}

// Accept returns true if the event's duration is at or below the threshold.
func (f *FastFilter) Accept(ev *logevent.LogEvent) bool {
	return ev.Duration != nil && *ev.Duration <= f.ThresholdMs
}

// SkipRemaining always returns false — fast queries can appear anywhere.
func (f *FastFilter) SkipRemaining() bool { return false }

// HighlightSpans returns spans for durationMillis in line.
func (f *FastFilter) HighlightSpans(line string, ev *logevent.LogEvent) []Span {
	return FindDurationSpans(line)
}
