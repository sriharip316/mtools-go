package cmd

import (
	"bytes"
	"testing"
	"time"
)

func TestParseRateBounds(t *testing.T) {
	tests := []struct {
		rateStr string
		minR    float64
		maxR    float64
		expMin  float64
		expMax  float64
		expErr  bool
	}{
		{rateStr: "10-50", expMin: 10, expMax: 50},
		{rateStr: "20:80", expMin: 20, expMax: 80},
		{rateStr: "5..25", expMin: 5, expMax: 25},
		{rateStr: "100", expMin: 100, expMax: 100},
		{rateStr: "50-10", expMin: 50, expMax: 50}, // inverted range clamped
		{minR: 15, maxR: 45, expMin: 15, expMax: 45},
		{rateStr: "invalid", expErr: true},
	}

	for _, tt := range tests {
		minVal, maxVal, err := parseRateBounds(tt.rateStr, tt.minR, tt.maxR)
		if tt.expErr {
			if err == nil {
				t.Errorf("expected error for %q, got nil", tt.rateStr)
			}
			continue
		}
		if err != nil {
			t.Errorf("unexpected error for %q: %v", tt.rateStr, err)
			continue
		}
		if minVal != tt.expMin || maxVal != tt.expMax {
			t.Errorf("for input rateStr=%q, minR=%f, maxR=%f: got min=%f, max=%f; want min=%f, max=%f",
				tt.rateStr, tt.minR, tt.maxR, minVal, maxVal, tt.expMin, tt.expMax)
		}
	}
}

func TestParseFlexibleDuration(t *testing.T) {
	tests := []struct {
		input  string
		expDur time.Duration
		expErr bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"0s", 0, false},
		{"30", 30 * time.Second, false},
		{"30s", 30 * time.Second, false},
		{"5m", 5 * time.Minute, false},
		{"1h", 1 * time.Hour, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		dur, err := parseFlexibleDuration(tt.input)
		if tt.expErr {
			if err == nil {
				t.Errorf("expected error for %q, got nil", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("unexpected error for %q: %v", tt.input, err)
			continue
		}
		if dur != tt.expDur {
			t.Errorf("for input %q: got %v, want %v", tt.input, dur, tt.expDur)
		}
	}
}

func TestLoadCmdDryRun(t *testing.T) {
	cmd := NewLoadCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--dry-run"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error executing load --dry-run: %v", err)
	}
}
