package filter

import (
	"regexp"
	"strings"

	"github.com/sriharip316/mtools-go/internal/logevent"
	"github.com/sriharip316/mtools-go/internal/pattern"
)

// LogLineFilter combines multiple attribute-based filters (--component,
// --level, --namespace, --operation, --thread, --pattern, --command,
// --planSummary). All active criteria are AND'd together.
type LogLineFilter struct {
	Components    map[string]bool
	Levels        map[string]bool
	Namespaces    map[string]bool
	Operations    map[string]bool
	Threads       map[string]bool
	Commands      map[string]bool
	Pattern       string
	PlanSummaries map[string]bool
}

// NewLogLineFilter constructs a LogLineFilter from CLI arguments.
// Each argument is a slice of values. Nil or empty slices mean "not filtering on this".
// patternStr is a JSON string that will be normalized via pattern.JSON2PatternFromString.
func NewLogLineFilter(
	components, levels, namespaces, operations, threads, commands []string,
	patternStr string,
	planSummaries []string,
) (*LogLineFilter, error) {
	f := &LogLineFilter{}

	if len(components) > 0 {
		f.Components = toSet(components)
	}
	if len(levels) > 0 {
		f.Levels = toSet(levels)
	}
	if len(namespaces) > 0 {
		f.Namespaces = toSet(namespaces)
	}
	if len(operations) > 0 {
		f.Operations = toSet(operations)
	}
	if len(threads) > 0 {
		f.Threads = toSet(threads)
	}
	if len(commands) > 0 {
		f.Commands = toSet(commands)
	}
	if patternStr != "" {
		p, err := pattern.JSON2PatternFromString(patternStr)
		if err != nil {
			return nil, err
		}
		f.Pattern = p
	}
	if len(planSummaries) > 0 {
		f.PlanSummaries = toSet(planSummaries)
	}

	return f, nil
}

// Active returns true if any criterion is set.
func (f *LogLineFilter) Active() bool {
	return f.Components != nil || f.Levels != nil || f.Namespaces != nil ||
		f.Operations != nil || f.Threads != nil || f.Commands != nil ||
		f.Pattern != "" || f.PlanSummaries != nil
}

// Accept returns true if the event matches all active criteria.
func (f *LogLineFilter) Accept(ev *logevent.LogEvent) bool {
	if f.Components != nil && !f.Components[ev.Component] {
		return false
	}
	if f.Levels != nil && !f.Levels[ev.Level] {
		return false
	}
	if f.Namespaces != nil && !f.Namespaces[ev.Namespace] {
		return false
	}
	if f.Operations != nil && !f.Operations[ev.Operation] {
		return false
	}
	if f.Commands != nil && !f.Commands[ev.Command] {
		return false
	}
	if f.Threads != nil {
		if !f.Threads[ev.Thread] && !f.Threads[ev.Conn] {
			return false
		}
	}
	if f.Pattern != "" && ev.Pattern != f.Pattern {
		return false
	}
	if f.PlanSummaries != nil && !f.PlanSummaries[ev.PlanSummary] {
		return false
	}
	return true
}

// SkipRemaining always returns false.
func (f *LogLineFilter) SkipRemaining() bool { return false }

// HighlightSpans finds matching substrings corresponding to active criteria.
func (f *LogLineFilter) HighlightSpans(line string, ev *logevent.LogEvent) []Span {
	var spans []Span
	if f.Components != nil && ev.Component != "" && f.Components[ev.Component] {
		spans = append(spans, FindSubstrings(line, ev.Component)...)
	}
	if f.Levels != nil && ev.Level != "" && f.Levels[ev.Level] {
		spans = append(spans, FindSubstrings(line, ev.Level)...)
	}
	if f.Namespaces != nil && ev.Namespace != "" && f.Namespaces[ev.Namespace] {
		spans = append(spans, FindSubstrings(line, ev.Namespace)...)
	}
	if f.Operations != nil && ev.Operation != "" && f.Operations[ev.Operation] {
		spans = append(spans, FindSubstrings(line, ev.Operation)...)
	}
	if f.Commands != nil && ev.Command != "" && f.Commands[ev.Command] {
		spans = append(spans, FindSubstrings(line, ev.Command)...)
	}
	if f.Threads != nil {
		if ev.Thread != "" && f.Threads[ev.Thread] {
			spans = append(spans, FindSubstrings(line, ev.Thread)...)
		}
		if ev.Conn != "" && f.Threads[ev.Conn] && ev.Conn != ev.Thread {
			spans = append(spans, FindSubstrings(line, ev.Conn)...)
		}
	}
	if f.PlanSummaries != nil && ev.PlanSummary != "" && f.PlanSummaries[ev.PlanSummary] {
		spans = append(spans, FindSubstrings(line, ev.PlanSummary)...)
	}
	return spans
}

func toSet(values []string) map[string]bool {
	s := make(map[string]bool, len(values))
	for _, v := range values {
		s[v] = true
	}
	return s
}

// TableScanFilter accepts log events with poor index usage (--scan).
// Heuristic: (keysExamined or docsExamined > 10000) AND ratio of scanned to returned > 100.
type TableScanFilter struct{}

// Accept returns true if the event shows signs of a table scan.
func (f *TableScanFilter) Accept(ev *logevent.LogEvent) bool {
	scanned := 0
	if ev.NScanned != nil && *ev.NScanned > scanned {
		scanned = *ev.NScanned
	}
	if ev.NScannedObjects != nil && *ev.NScannedObjects > scanned {
		scanned = *ev.NScannedObjects
	}
	if scanned == 0 {
		return false
	}

	nr := 1
	if ev.NReturned != nil && *ev.NReturned > 0 {
		nr = *ev.NReturned
	}

	return scanned > 10000 && (scanned/nr) > 100
}

// SkipRemaining always returns false.
func (f *TableScanFilter) SkipRemaining() bool { return false }

// HighlightSpans returns spans for table scan indicators (COLLSCAN, docsExamined, keysExamined).
func (f *TableScanFilter) HighlightSpans(line string, ev *logevent.LogEvent) []Span {
	return FindScanSpans(line)
}

// WordFilter accepts log events matching any of the given regex patterns (--word).
type WordFilter struct {
	Patterns []*regexp.Regexp
}

// NewWordFilter compiles word patterns into regexes.
func NewWordFilter(words []string) (*WordFilter, error) {
	patterns := make([]*regexp.Regexp, 0, len(words))
	for _, w := range words {
		re, err := regexp.Compile(w)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, re)
	}
	return &WordFilter{Patterns: patterns}, nil
}

// Accept returns true if any pattern matches the raw log line.
func (f *WordFilter) Accept(ev *logevent.LogEvent) bool {
	for _, p := range f.Patterns {
		if p.MatchString(ev.LineStr) {
			return true
		}
	}
	return false
}

// SkipRemaining always returns false.
func (f *WordFilter) SkipRemaining() bool { return false }

// HighlightSpans returns all matching regex spans for the line.
func (f *WordFilter) HighlightSpans(line string, ev *logevent.LogEvent) []Span {
	var spans []Span
	for _, p := range f.Patterns {
		matches := p.FindAllStringIndex(line, -1)
		for _, m := range matches {
			spans = append(spans, Span{Start: m[0], End: m[1]})
		}
	}
	return spans
}

// TransactionFilter accepts log events containing "transaction" (--transactions).
type TransactionFilter struct{}

// Accept returns true if the log line contains "transaction".
func (f *TransactionFilter) Accept(ev *logevent.LogEvent) bool {
	return strings.Contains(ev.LineStr, "transaction")
}

// SkipRemaining always returns false.
func (f *TransactionFilter) SkipRemaining() bool { return false }

// HighlightSpans returns spans for "transaction" occurrences.
func (f *TransactionFilter) HighlightSpans(line string, ev *logevent.LogEvent) []Span {
	return FindCaseInsensitiveSubstrings(line, "transaction")
}
