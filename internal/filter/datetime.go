package filter

import (
	"time"

	"github.com/sriharip316/mtools-go/internal/hci"
	"github.com/sriharip316/mtools-go/internal/logevent"
)

// DateTimeFilter filters log events by time range (--from / --to).
type DateTimeFilter struct {
	FromDT      time.Time
	ToDT        time.Time
	fromReached bool
	toReached   bool
}

// NewDateTimeFilter creates a DateTimeFilter from --from/--to strings.
// logStart and logEnd are the time bounds of the log file(s).
// isStdin indicates whether input is from stdin (affects default bounds).
func NewDateTimeFilter(fromStr, toStr string, logStart, logEnd time.Time, isStdin bool) (*DateTimeFilter, error) {
	if isStdin {
		now := time.Now().UTC()
		logStart = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		logEnd = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	}

	dtb := hci.New(logStart, logEnd)
	fromDT, toDT, err := dtb.Resolve(fromStr, toStr)
	if err != nil {
		return nil, err
	}

	return &DateTimeFilter{
		FromDT: fromDT,
		ToDT:   toDT,
	}, nil
}

// Accept returns true if the log event falls within the time range.
func (f *DateTimeFilter) Accept(ev *logevent.LogEvent) bool {
	dt := ev.DateTime
	if dt.IsZero() {
		// Accept dateless lines if we've already entered the range
		return f.fromReached
	}

	if !dt.Before(f.FromDT) && !dt.After(f.ToDT) {
		f.fromReached = true
		f.toReached = false
		return true
	}

	if dt.After(f.ToDT) {
		f.toReached = true
		return false
	}

	return false
}

// SkipRemaining returns true if all subsequent lines are past the --to boundary.
func (f *DateTimeFilter) SkipRemaining() bool {
	return f.toReached
}

// StartLimit returns the --from datetime for fast-forwarding.
func (f *DateTimeFilter) StartLimit() time.Time {
	return f.FromDT
}

// HighlightSpans returns spans for timestamp occurrences in line.
func (f *DateTimeFilter) HighlightSpans(line string, ev *logevent.LogEvent) []Span {
	return FindTimestampSpans(line, ev)
}
