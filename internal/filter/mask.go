package filter

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/sriharip316/mtools-go/internal/logevent"
	"github.com/sriharip316/mtools-go/internal/logfile"
)

// TimeInterval represents a time window [Start, End].
type TimeInterval struct {
	Start time.Time
	End   time.Time
}

// MaskFilter accepts log events falling within time windows centered
// around events from a secondary mask log file (--mask).
type MaskFilter struct {
	intervals      []TimeInterval
	maskEndReached bool
}

// NewMaskFilter loads events from maskPath, creates padded time windows
// around each event, and merges overlapping windows into disjoint intervals.
func NewMaskFilter(maskPath string, maskSizeSec int, maskCenter string) (*MaskFilter, error) {
	if maskSizeSec <= 0 {
		maskSizeSec = 60
	}
	if maskCenter == "" {
		maskCenter = "end"
	}
	if maskCenter != "start" && maskCenter != "end" && maskCenter != "both" {
		return nil, fmt.Errorf("invalid mask-center %q: must be 'start', 'end', or 'both'", maskCenter)
	}

	lf, err := logfile.Open(maskPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open mask file %q: %w", maskPath, err)
	}
	defer func() { _ = lf.Close() }()

	halfPadding := time.Duration(maskSizeSec) * time.Second / 2

	var rawIntervals []TimeInterval

	for {
		ev, err := lf.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("error reading mask file %q: %w", maskPath, err)
		}

		if ev.DateTime.IsZero() {
			continue
		}

		duration := time.Duration(0)
		if ev.Duration != nil && *ev.Duration > 0 {
			duration = time.Duration(*ev.Duration) * time.Millisecond
		}

		var startPoint, endPoint time.Time

		switch maskCenter {
		case "start":
			center := ev.DateTime.Add(-duration)
			startPoint = center.Add(-halfPadding)
			endPoint = center.Add(halfPadding)
		case "end":
			center := ev.DateTime
			startPoint = center.Add(-halfPadding)
			endPoint = center.Add(halfPadding)
		case "both":
			startPoint = ev.DateTime.Add(-duration).Add(-halfPadding)
			endPoint = ev.DateTime.Add(halfPadding)
		}

		rawIntervals = append(rawIntervals, TimeInterval{
			Start: startPoint,
			End:   endPoint,
		})
	}

	if len(rawIntervals) == 0 {
		return &MaskFilter{
			intervals:      nil,
			maskEndReached: true,
		}, nil
	}

	// Sort intervals by start time
	sort.Slice(rawIntervals, func(i, j int) bool {
		return rawIntervals[i].Start.Before(rawIntervals[j].Start)
	})

	// Merge overlapping or adjacent intervals
	var merged []TimeInterval
	cur := rawIntervals[0]

	for i := 1; i < len(rawIntervals); i++ {
		next := rawIntervals[i]
		if !next.Start.After(cur.End) {
			// Overlaps or touches
			if next.End.After(cur.End) {
				cur.End = next.End
			}
		} else {
			merged = append(merged, cur)
			cur = next
		}
	}
	merged = append(merged, cur)

	return &MaskFilter{
		intervals: merged,
	}, nil
}

// Intervals returns a copy of the merged time windows.
func (f *MaskFilter) Intervals() []TimeInterval {
	return f.intervals
}

// Accept returns true if ev.DateTime falls within any of the mask intervals.
func (f *MaskFilter) Accept(ev *logevent.LogEvent) bool {
	if len(f.intervals) == 0 || ev.DateTime.IsZero() {
		return false
	}

	dt := ev.DateTime

	// If past the last interval's end, mark end reached
	lastEnd := f.intervals[len(f.intervals)-1].End
	if dt.After(lastEnd) {
		f.maskEndReached = true
		return false
	}

	// Binary search or linear check for interval containment
	for _, interval := range f.intervals {
		if !dt.Before(interval.Start) && !dt.After(interval.End) {
			return true
		}
		if dt.Before(interval.Start) {
			// Since intervals are sorted, no need to check further intervals
			break
		}
	}

	return false
}

// SkipRemaining returns true if the stream has passed beyond all mask intervals.
func (f *MaskFilter) SkipRemaining() bool {
	return f.maskEndReached
}

// StartLimit returns the earliest timestamp of the mask intervals for fast-forwarding.
func (f *MaskFilter) StartLimit() time.Time {
	if len(f.intervals) == 0 {
		return time.Time{}
	}
	return f.intervals[0].Start
}

// HighlightSpans returns spans for timestamp occurrences in line.
func (f *MaskFilter) HighlightSpans(line string, ev *logevent.LogEvent) []Span {
	return FindTimestampSpans(line, ev)
}
