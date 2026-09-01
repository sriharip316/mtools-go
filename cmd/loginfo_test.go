package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sriharip316/mtools-go/internal/ui"
)

func executeLoginfo(args ...string) (string, error) {
	cmd := NewLoginfoCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	normalizedArgs := NormalizeFlags(args)
	cmd.SetArgs(normalizedArgs)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := cmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	var outBuf bytes.Buffer
	io.Copy(&outBuf, r)

	return outBuf.String(), err
}

func TestLoginfo_Basic(t *testing.T) {
	logPath := createSampleLogFile(t, "mloginfo_sample")
	defer os.Remove(logPath)

	out, err := executeLoginfo(logPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "source:") || !strings.Contains(out, "host:") || !strings.Contains(out, "length:") {
		t.Errorf("expected basic metadata output, got:\n%s", out)
	}
	if !strings.Contains(out, "length: 6") {
		t.Errorf("expected line count 6, got:\n%s", out)
	}
}

func TestLoginfo_Queries(t *testing.T) {
	logPath := createSampleLogFile(t, "mloginfo_sample")
	defer os.Remove(logPath)

	out, err := executeLoginfo(logPath, "--queries", "--rounding", "2", "--sort", "sum")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "QUERIES") {
		t.Errorf("expected QUERIES section header, got:\n%s", out)
	}
	if !strings.Contains(out, "mydb.users") || !strings.Contains(out, "mydb.orders") {
		t.Errorf("expected query namespaces, got:\n%s", out)
	}
}

func TestLoginfo_Distinct(t *testing.T) {
	logPath := createSampleLogFile(t, "mloginfo_sample")
	defer os.Remove(logPath)

	out, err := executeLoginfo(logPath, "--distinct", "--distinctmin", "1", "--verbose")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "DISTINCT") {
		t.Errorf("expected DISTINCT section header, got:\n%s", out)
	}
	if !strings.Contains(out, "Slow query") {
		t.Errorf("expected Slow query in distinct output, got:\n%s", out)
	}
}

func TestLoginfo_Connections(t *testing.T) {
	logPath := createSampleLogFile(t, "mloginfo_sample")
	defer os.Remove(logPath)

	out, err := executeLoginfo(logPath, "--connections", "--connstats")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "CONNECTIONS") {
		t.Errorf("expected CONNECTIONS section header, got:\n%s", out)
	}
	if !strings.Contains(out, "total opened:") || !strings.Contains(out, "total closed:") {
		t.Errorf("expected connection summary counters, got:\n%s", out)
	}
}

func TestLoginfo_MultipleFiles(t *testing.T) {
	f1 := createSampleLogFile(t, "f1")
	defer os.Remove(f1)
	f2 := createSampleLogFile(t, "f2")
	defer os.Remove(f2)

	out, err := executeLoginfo(f1, f2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "------------------------------------------") {
		t.Errorf("expected file separator in multi-file output, got:\n%s", out)
	}
}

func TestLoginfo_NoArgs(t *testing.T) {
	_, err := executeLoginfo()
	if err == nil {
		t.Fatalf("expected error when no logfiles provided, got nil")
	}
}

func TestLoginfo_Color_Always(t *testing.T) {
	logPath := createSampleLogFile(t, "mloginfo_color_always")
	defer os.Remove(logPath)

	out, err := executeLoginfo(logPath, "--queries", "--color=always")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected ANSI escape codes with --color=always, got:\n%q", out)
	}
	// Colored output must keep the searchable substrings intact.
	if !strings.Contains(out, "QUERIES") {
		t.Errorf("expected QUERIES section header, got:\n%s", out)
	}
	if !strings.Contains(out, "source:") {
		t.Errorf("expected metadata labels, got:\n%s", out)
	}
	if !strings.Contains(out, ui.Bold+"QUERIES"+ui.Reset) {
		t.Errorf("expected bold QUERIES header, got:\n%q", out)
	}
	if !strings.Contains(out, "mydb.orders") {
		t.Errorf("expected query table rows, got:\n%s", out)
	}
}

func TestLoginfo_Color_Disabled(t *testing.T) {
	logPath := createSampleLogFile(t, "mloginfo_color_off")
	defer os.Remove(logPath)

	cases := []struct {
		name string
		args []string
		env  string
	}{
		{name: "default (auto, piped)", args: nil},
		{name: "--no-color", args: []string{"--no-color"}},
		{name: "--color=never", args: []string{"--color=never"}},
		{name: "NO_COLOR env with --color=always", args: []string{"--color=always"}, env: "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("NO_COLOR", tc.env)
			} else {
				t.Setenv("NO_COLOR", "")
			}

			args := append([]string{logPath, "--queries"}, tc.args...)
			out, err := executeLoginfo(args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(out, "\x1b[") {
				t.Errorf("expected no ANSI escape codes, got:\n%q", out)
			}
		})
	}
}

func TestLoginfo_Color_StripMatchesPlain(t *testing.T) {
	logPath := createSampleLogFile(t, "mloginfo_color_strip")
	defer os.Remove(logPath)

	plain, err := executeLoginfo(logPath, "--queries")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	colored, err := executeLoginfo(logPath, "--queries", "--color=always")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stripped := ui.StripAnsi(colored); stripped != plain {
		t.Errorf("colored output stripped of ANSI does not match plain output\ncolor: %q\nplain: %q", stripped, plain)
	}
}
