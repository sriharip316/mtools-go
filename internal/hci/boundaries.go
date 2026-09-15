// Package hci provides datetime boundary parsing for log file time range filtering.
package hci

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DateTimeBoundaries represents start and end times for bounds checking.
type DateTimeBoundaries struct {
	Start time.Time
	End   time.Time
}

// New creates a new DateTimeBoundaries instance with UTC times.
func New(start, end time.Time) *DateTimeBoundaries {
	return &DateTimeBoundaries{
		Start: start.UTC(),
		End:   end.UTC(),
	}
}

var (
	constRe   = regexp.MustCompile(`(?i)\b(now|start|end|today|yesterday)\b`)
	weekdayRe = regexp.MustCompile(`(?i)\b(Mon|Tue|Wed|Thu|Fri|Sat|Sun)\b`)
	offsetRe  = regexp.MustCompile(`([+-]\d+)\s*(s|sec|secs|m|min|mins|h|hour|hours|d|day|days|w|week|weeks|mo|month|months|y|year|years)\b`)
)

// Resolve parses from and to strings into bounded time.Time values.
func (dtb *DateTimeBoundaries) Resolve(fromStr, toStr string) (time.Time, time.Time, error) {
	fromDT, err := dtb.String2DT(fromStr, nil)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	toDT, err := dtb.String2DT(toStr, &fromDT)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	if fromDT.Before(dtb.Start) {
		fromDT = dtb.Start
	}
	if fromDT.After(dtb.End) {
		fromDT = dtb.End
	}

	if toDT.After(dtb.End) {
		toDT = dtb.End
	}
	if toDT.Before(dtb.Start) {
		toDT = dtb.Start
	}

	if toDT.Before(fromDT) {
		return time.Time{}, time.Time{}, errors.New("to cannot be before from")
	}

	return fromDT, toDT, nil
}

// String2DT converts a human-readable string to a time.Time.
func (dtb *DateTimeBoundaries) String2DT(s string, lowerBound *time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "start") {
		if lowerBound != nil && s == "" {
			return *lowerBound, nil
		}
		return dtb.Start, nil
	}
	if strings.EqualFold(s, "end") {
		return dtb.End, nil
	}

	var anchor time.Time
	anchorSet := false
	var dur time.Duration
	hasOffset := false

	// Constants
	if match := constRe.FindString(s); match != "" {
		s = constRe.ReplaceAllString(s, "")
		switch strings.ToLower(match) {
		case "now":
			anchor = time.Now().UTC()
		case "start":
			anchor = dtb.Start
		case "end":
			anchor = dtb.End
		case "today":
			now := time.Now().UTC()
			anchor = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		case "yesterday":
			now := time.Now().UTC()
			anchor = time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, time.UTC)
		}
		anchorSet = true
	}

	// Weekdays
	if !anchorSet {
		if match := weekdayRe.FindString(s); match != "" {
			s = weekdayRe.ReplaceAllString(s, "")
			var target time.Weekday
			switch strings.ToLower(match) {
			case "sun":
				target = time.Sunday
			case "mon":
				target = time.Monday
			case "tue":
				target = time.Tuesday
			case "wed":
				target = time.Wednesday
			case "thu":
				target = time.Thursday
			case "fri":
				target = time.Friday
			case "sat":
				target = time.Saturday
			}

			endWeekday := dtb.End.Weekday()
			daysBack := int(endWeekday - target)
			if daysBack < 0 {
				daysBack += 7
			}

			anchorDate := dtb.End.AddDate(0, 0, -daysBack)
			anchor = time.Date(anchorDate.Year(), anchorDate.Month(), anchorDate.Day(), 0, 0, 0, 0, time.UTC)
			anchorSet = true
		}
	}

	// Offsets
	if matches := offsetRe.FindAllStringSubmatch(s, -1); len(matches) > 0 {
		for _, match := range matches {
			s = strings.Replace(s, match[0], "", 1)
			val, _ := strconv.Atoi(match[1])
			unit := match[2]
			switch unit {
			case "s", "sec", "secs":
				dur += time.Duration(val) * time.Second
			case "m", "min", "mins":
				dur += time.Duration(val) * time.Minute
			case "h", "hour", "hours":
				dur += time.Duration(val) * time.Hour
			case "d", "day", "days":
				dur += time.Duration(val) * 24 * time.Hour
			case "w", "week", "weeks":
				dur += time.Duration(val) * 7 * 24 * time.Hour
			case "mo", "month", "months":
				dur += time.Duration(val) * 30 * 24 * time.Hour
			case "y", "year", "years":
				dur += time.Duration(val) * 365 * 24 * time.Hour
			}
		}
		hasOffset = true
	}

	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	var t time.Time
	timeParsed := false
	hasYear := false
	hasDate := false

	if s != "" {
		layouts := []string{
			"2006-01-02T15:04:05.999999999Z07:00",
			"2006-01-02T15:04:05.000Z",
			"2006-01-02T15:04:05.000-0700",
			"2006-01-02T15:04:05Z",
			"2006-01-02T15:04:05-0700",
			"2006-01-02",
			"Jan 2 2006 15:04:05",
			"Jan 2 2006 15:04",
			"Jan 2 2006",
			"2 Jan 2006 15:04:05",
			"2 Jan 2006 15:04",
			"2 Jan 2006",
			"Jan 2006",
			"2006",
			"Jan 2",
			"2 Jan",
			"Jan",
			"15:04:05.000",
			"15:04:05",
			"15:04",
		}

		for _, layout := range layouts {
			if parsedTime, pErr := time.Parse(layout, s); pErr == nil {
				t = parsedTime
				timeParsed = true
				if strings.Contains(layout, "2006") {
					hasYear = true
				}
				if strings.Contains(layout, "Jan") || strings.Contains(layout, "01") || layout == "2006" {
					hasDate = true
				}
				break
			}
		}
		if !timeParsed {
			return time.Time{}, errors.New("cannot parse datetime: " + s)
		}
	} else if anchorSet {
		t = anchor
		hasDate = true
		hasYear = true
	} else if hasOffset {
		if lowerBound != nil {
			t = *lowerBound
		} else {
			t = dtb.End
		}
		hasDate = true
		hasYear = true
	} else {
		if lowerBound != nil {
			return *lowerBound, nil
		}
		return dtb.End, nil
	}

	if anchorSet {
		if hasDate && !timeParsed {
			t = anchor
		} else if !hasDate && timeParsed {
			t = time.Date(anchor.Year(), anchor.Month(), anchor.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
		} else if hasDate && timeParsed {
			if !hasYear {
				t = time.Date(anchor.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
			}
		}
	} else {
		if !hasDate {
			if lowerBound != nil {
				t = time.Date(lowerBound.Year(), lowerBound.Month(), lowerBound.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
			} else {
				t = time.Date(dtb.Start.Year(), dtb.Start.Month(), dtb.Start.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
			}
		} else if !hasYear {
			t = time.Date(dtb.End.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)

			// Try year-1 if after end, or year+1 if before start
			if t.After(dtb.End) {
				t1 := t.AddDate(-1, 0, 0)
				if !t1.Before(dtb.Start) {
					t = t1
				}
			} else if t.Before(dtb.Start) {
				t1 := t.AddDate(1, 0, 0)
				if !t1.After(dtb.End) {
					t = t1
				}
			}
		}
	}

	t = t.Add(dur)
	return t, nil
}
