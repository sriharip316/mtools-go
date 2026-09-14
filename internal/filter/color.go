package filter

import (
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/sriharip316/mtools-go/internal/logevent"
	"github.com/sriharip316/mtools-go/internal/ui"
)

// ANSI escape codes for terminal styling.
const (
	ColorReset  = ui.Reset
	ColorRed    = ui.Red
	ColorYellow = ui.Yellow
	ColorGrey   = ui.Grey
	ColorBlue   = ui.Blue
)

// GetBaseColor returns the ANSI color code based on log event severity:
// - Red for Error ("E") and Fatal ("F")
// - Orange/Yellow for Warning ("W")
// - Grey for all other levels (Info, Debug, etc.)
func GetBaseColor(level string) string {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "F", "FATAL", "E", "ERROR":
		return ColorRed
	case "W", "WARN", "WARNING":
		return ColorYellow
	default:
		return ColorGrey
	}
}

// IsTerminal checks if the given file handle is an interactive terminal.
func IsTerminal(f *os.File) bool {
	return ui.IsTerminal(f)
}

// MergeSpans sorts and merges overlapping or adjacent byte index ranges.
func MergeSpans(spans []Span) []Span {
	var valid []Span
	for _, s := range spans {
		if s.Start >= 0 && s.End > s.Start {
			valid = append(valid, s)
		}
	}
	if len(valid) <= 1 {
		return valid
	}

	sort.Slice(valid, func(i, j int) bool {
		if valid[i].Start == valid[j].Start {
			return valid[i].End < valid[j].End
		}
		return valid[i].Start < valid[j].Start
	})

	merged := []Span{valid[0]}
	for _, s := range valid[1:] {
		last := &merged[len(merged)-1]
		if s.Start <= last.End {
			if s.End > last.End {
				last.End = s.End
			}
		} else {
			merged = append(merged, s)
		}
	}
	return merged
}

// Colorize wraps matching spans in Blue and the remaining text in baseColor.
func Colorize(line string, baseColor string, spans []Span) string {
	if line == "" {
		return ""
	}

	lineLen := len(line)
	var clamped []Span
	for _, s := range spans {
		if s.Start >= lineLen {
			continue
		}
		end := min(s.End, lineLen)
		if end > s.Start {
			clamped = append(clamped, Span{Start: s.Start, End: end})
		}
	}

	merged := MergeSpans(clamped)
	if len(merged) == 0 {
		return baseColor + line + ColorReset
	}

	var sb strings.Builder
	curr := 0
	for _, span := range merged {
		if span.Start < curr {
			continue
		}
		if span.Start > curr {
			sb.WriteString(baseColor)
			sb.WriteString(line[curr:span.Start])
			sb.WriteString(ColorReset)
		}
		sb.WriteString(ColorBlue)
		sb.WriteString(line[span.Start:span.End])
		sb.WriteString(ColorReset)
		curr = span.End
	}
	if curr < lineLen {
		sb.WriteString(baseColor)
		sb.WriteString(line[curr:])
		sb.WriteString(ColorReset)
	}
	return sb.String()
}

// FindSubstrings returns all byte index spans of substr in line.
func FindSubstrings(line, substr string) []Span {
	if substr == "" {
		return nil
	}
	var spans []Span
	idx := 0
	for {
		pos := strings.Index(line[idx:], substr)
		if pos == -1 {
			break
		}
		start := idx + pos
		end := start + len(substr)
		spans = append(spans, Span{Start: start, End: end})
		idx = end
	}
	return spans
}

// FindCaseInsensitiveSubstrings returns all byte index spans of substr in line ignoring case.
func FindCaseInsensitiveSubstrings(line, substr string) []Span {
	if substr == "" {
		return nil
	}
	var spans []Span
	lowerLine := strings.ToLower(line)
	lowerSubstr := strings.ToLower(substr)
	idx := 0
	for {
		pos := strings.Index(lowerLine[idx:], lowerSubstr)
		if pos == -1 {
			break
		}
		start := idx + pos
		end := start + len(substr)
		spans = append(spans, Span{Start: start, End: end})
		idx = end
	}
	return spans
}

var reDurationSpans = regexp.MustCompile(`("durationMillis"\s*:\s*\d+|durationMillis\s*:\s*[\d,]+|\(\s*\d+\s*hr[^\)]*\)|\(\s*\d+\s*min[^\)]*\)|\(\s*\d+\s*secs?[^\)]*\)|\(\s*\d+\s*ms\s*\))`)

// FindDurationSpans finds duration values and formatted duration strings in line.
func FindDurationSpans(line string) []Span {
	matches := reDurationSpans.FindAllStringIndex(line, -1)
	var spans []Span
	for _, m := range matches {
		spans = append(spans, Span{Start: m[0], End: m[1]})
	}
	return spans
}

var reScanSpans = regexp.MustCompile(`("COLLSCAN"|COLLSCAN|"docsExamined"\s*:\s*[\d,]+|"keysExamined"\s*:\s*[\d,]+|docsExamined\s*:\s*[\d,]+|keysExamined\s*:\s*[\d,]+)`)

// FindScanSpans finds table scan indicators (COLLSCAN, docsExamined, keysExamined) in line.
func FindScanSpans(line string) []Span {
	matches := reScanSpans.FindAllStringIndex(line, -1)
	var spans []Span
	for _, m := range matches {
		spans = append(spans, Span{Start: m[0], End: m[1]})
	}
	return spans
}

var reDateSpan = regexp.MustCompile(`"t"\s*:\s*\{\s*"$date"\s*:\s*"([^"]+)"\s*\}`)

// FindTimestampSpans finds timestamp occurrences in the log line.
func FindTimestampSpans(line string, ev *logevent.LogEvent) []Span {
	var spans []Span
	matches := reDateSpan.FindAllStringSubmatchIndex(line, -1)
	for _, m := range matches {
		if len(m) >= 4 && m[2] != -1 && m[3] != -1 {
			spans = append(spans, Span{Start: m[2], End: m[3]})
		} else if len(m) >= 2 {
			spans = append(spans, Span{Start: m[0], End: m[1]})
		}
	}
	return spans
}
