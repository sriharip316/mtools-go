package logfile

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func generateLogLines(count int, start time.Time, step time.Duration) string {
	var sb strings.Builder
	for i := range count {
		t := start.Add(time.Duration(i) * step)
		fmt.Fprintf(&sb, `{"t":{"$date":"%s"},"s":"I","c":"COMMAND","id":51803,"ctx":"conn%d","msg":"Slow query","attr":{"type":"command","ns":"test.coll","durationMillis":%d}}`+"\n",
			t.Format(time.RFC3339Nano),
			i+1,
			(i+1)*10)
	}
	return sb.String()
}

func TestLogFile_Reader(t *testing.T) {
	start := time.Date(2023, 10, 1, 12, 0, 0, 0, time.UTC)
	content := generateLogLines(5, start, time.Minute)

	lf := NewFromReader(strings.NewReader(content), "stdin")
	var count int
	for {
		ev, err := lf.Next()
		if err != nil {
			break
		}
		count++
		if ev.Component != "COMMAND" {
			t.Errorf("unexpected component: %s", ev.Component)
		}
	}
	if count != 5 {
		t.Fatalf("expected 5 events, got %d", count)
	}
}

func TestLogFile_FileAndBounds(t *testing.T) {
	start := time.Date(2023, 10, 1, 12, 0, 0, 0, time.UTC)
	content := generateLogLines(100, start, time.Minute)

	tmp, err := os.CreateTemp("", "mtools_test_*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(content); err != nil {
		t.Fatalf("failed to write content: %v", err)
	}
	tmp.Close()

	lf, err := Open(tmp.Name())
	if err != nil {
		t.Fatalf("failed to open logfile: %v", err)
	}
	defer lf.Close()

	if !lf.Start.Equal(start) {
		t.Errorf("expected start %v, got %v", start, lf.Start)
	}
	expectedEnd := start.Add(99 * time.Minute)
	if !lf.End.Equal(expectedEnd) {
		t.Errorf("expected end %v, got %v", expectedEnd, lf.End)
	}
}

func TestLogFile_FastForward(t *testing.T) {
	start := time.Date(2023, 10, 1, 12, 0, 0, 0, time.UTC)
	content := generateLogLines(500, start, time.Minute)

	tmp, err := os.CreateTemp("", "mtools_test_ff_*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(content); err != nil {
		t.Fatalf("failed to write content: %v", err)
	}
	tmp.Close()

	lf, err := Open(tmp.Name())
	if err != nil {
		t.Fatalf("failed to open logfile: %v", err)
	}
	defer lf.Close()

	// Fast forward to index 250 (start + 250m)
	target := start.Add(250 * time.Minute)
	lf.FastForward(target)

	ev, err := lf.Next()
	if err != nil {
		t.Fatalf("unexpected error after FastForward: %v", err)
	}

	if !ev.DateTime.Equal(target) {
		t.Errorf("expected event at %v, got %v", target, ev.DateTime)
	}
}
