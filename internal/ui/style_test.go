package ui

import (
	"strings"
	"testing"
)

func TestStyleWrap(t *testing.T) {
	enabled := Style{Enabled: true}
	disabled := Style{Enabled: false}

	if got := enabled.Wrap(Red, "boom"); got != Red+"boom"+Reset {
		t.Errorf("enabled wrap = %q, want %q", got, Red+"boom"+Reset)
	}
	if got := disabled.Wrap(Red, "boom"); got != "boom" {
		t.Errorf("disabled wrap = %q, want %q", got, "boom")
	}
	if got := enabled.Wrap(Red, ""); got != "" {
		t.Errorf("enabled wrap of empty = %q, want empty", got)
	}
}

func TestStyleConvenience(t *testing.T) {
	s := Style{Enabled: true}
	if got := s.Bold("x"); got != Bold+"x"+Reset {
		t.Errorf("Bold = %q", got)
	}
	if got := s.Dim("x"); got != Dim+"x"+Reset {
		t.Errorf("Dim = %q", got)
	}
	if got := s.Cyan("x"); got != Cyan+"x"+Reset {
		t.Errorf("Cyan = %q", got)
	}

	zero := Style{}
	if got := zero.Bold("x"); got != "x" {
		t.Errorf("zero-value Bold = %q, want plain %q", got, "x")
	}
}

func TestStyleSectionHeader(t *testing.T) {
	plain := Style{}.SectionHeader("QUERIES")
	want := "QUERIES\n" + strings.Repeat("-", 7)
	if plain != want {
		t.Errorf("plain header = %q, want %q", plain, want)
	}

	colored := Style{Enabled: true}.SectionHeader("QUERIES")
	if !strings.Contains(colored, Bold+"QUERIES"+Reset) {
		t.Errorf("colored header missing bold title: %q", colored)
	}
	if !strings.Contains(colored, Dim+strings.Repeat("-", 7)+Reset) {
		t.Errorf("colored header missing dim rule: %q", colored)
	}
	if StripAnsi(colored) != plain {
		t.Errorf("StripAnsi(colored) = %q, want plain %q", StripAnsi(colored), plain)
	}
}

func TestStripAnsi(t *testing.T) {
	in := Red + "error" + Reset + " and " + Bold + "bold" + Reset
	if got := StripAnsi(in); got != "error and bold" {
		t.Errorf("StripAnsi = %q", got)
	}
	if got := VisibleWidth(in); got != len("error and bold") {
		t.Errorf("VisibleWidth = %d, want %d", got, len("error and bold"))
	}
	if got := StripAnsi("plain"); got != "plain" {
		t.Errorf("StripAnsi on plain text = %q", got)
	}
}
