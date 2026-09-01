package ui

import (
	"regexp"
	"strings"
)

// Style wraps text in ANSI codes when color output is enabled. The zero
// value is safe to use and never emits escape codes.
type Style struct {
	Enabled bool
}

// Wrap returns text wrapped in the given ANSI code, or text unchanged when
// styling is disabled or text is empty.
func (s Style) Wrap(code, text string) string {
	if !s.Enabled || text == "" {
		return text
	}
	return code + text + Reset
}

// Bold returns text in bold when styling is enabled.
func (s Style) Bold(text string) string { return s.Wrap(Bold, text) }

// Dim returns text dimmed when styling is enabled.
func (s Style) Dim(text string) string { return s.Wrap(Dim, text) }

// Red returns text in red when styling is enabled.
func (s Style) Red(text string) string { return s.Wrap(Red, text) }

// Yellow returns text in yellow when styling is enabled.
func (s Style) Yellow(text string) string { return s.Wrap(Yellow, text) }

// Green returns text in green when styling is enabled.
func (s Style) Green(text string) string { return s.Wrap(Green, text) }

// Cyan returns text in cyan when styling is enabled.
func (s Style) Cyan(text string) string { return s.Wrap(Cyan, text) }

// SectionHeader renders a section title followed by a dashed rule of the
// same width. In colored mode the title is bold and the rule dimmed.
// Callers should print the returned string followed by a newline.
func (s Style) SectionHeader(title string) string {
	rule := s.Dim(strings.Repeat("-", len(title)))
	return s.Bold(title) + "\n" + rule
}

var reAnsi = regexp.MustCompile("\033\\[[0-9;]*m")

// StripAnsi removes all ANSI color escape codes from s.
func StripAnsi(s string) string {
	return reAnsi.ReplaceAllString(s, "")
}

// VisibleWidth returns the display width of s ignoring ANSI escape codes.
func VisibleWidth(s string) int {
	return len(StripAnsi(s))
}
