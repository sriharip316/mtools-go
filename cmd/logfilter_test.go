package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func createSampleLogFile(t *testing.T, filename string) string {
	t.Helper()
	lines := []string{
		`{"t":{"$date":"2023-10-10T10:00:00.000Z"},"s":"I","c":"CONTROL","id":20001,"ctx":"main","msg":"MongoDB starting","attr":{"version":"6.0.5"}}`,
		`{"t":{"$date":"2023-10-10T10:01:00.000Z"},"s":"I","c":"NETWORK","id":20002,"ctx":"listener","msg":"Listening on 127.0.0.1:27017"}`,
		`{"t":{"$date":"2023-10-10T10:05:00.000Z"},"s":"I","c":"COMMAND","id":51803,"ctx":"conn1","msg":"Slow query","attr":{"type":"command","ns":"mydb.users","command":{"find":"users","filter":{"age":{"$gt":30}}},"planSummary":"COLLSCAN","keysExamined":0,"docsExamined":15000,"nreturned":5,"durationMillis":250}}`,
		`{"t":{"$date":"2023-10-10T10:10:00.000Z"},"s":"W","c":"COMMAND","id":51803,"ctx":"conn2","msg":"Slow query","attr":{"type":"command","ns":"mydb.orders","command":{"find":"orders","filter":{"status":"pending"}},"planSummary":"IXSCAN { status: 1 }","keysExamined":20,"docsExamined":20,"nreturned":20,"durationMillis":1200}}`,
		`{"t":{"$date":"2023-10-10T10:15:00.000Z"},"s":"D1","c":"STORAGE","id":22000,"ctx":"wt-checkpoint","msg":"Checkpoint finished","attr":{"durationMillis":50}}`,
		`{"t":{"$date":"2023-10-10T10:20:00.000Z"},"s":"I","c":"WRITE","id":51804,"ctx":"conn3","msg":"Slow transaction","attr":{"type":"transaction","ns":"mydb.accounts","durationMillis":3500}}`,
	}

	tmp, err := os.CreateTemp("", filename+"_*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = tmp.Close() }()

	for _, line := range lines {
		if _, err := tmp.WriteString(line + "\n"); err != nil {
			t.Fatalf("failed to write to temp file: %v", err)
		}
	}

	return tmp.Name()
}

func executeCommand(args ...string) (string, error) {
	cmd := NewLogfilterCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	normalizedArgs := NormalizeFlags(args)
	cmd.SetArgs(normalizedArgs)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	defer func() { _ = r.Close() }()
	os.Stdout = w

	err := cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout

	var outBuf bytes.Buffer
	_, _ = outBuf.ReadFrom(r)

	return outBuf.String(), err
}

func TestLogfilter_Basic(t *testing.T) {
	logPath := createSampleLogFile(t, "sample")
	defer func() { _ = os.Remove(logPath) }()

	out, err := executeCommand(logPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines, got %d:\n%s", len(lines), out)
	}
}

func TestLogfilter_Slow(t *testing.T) {
	logPath := createSampleLogFile(t, "sample")
	defer func() { _ = os.Remove(logPath) }()

	// Test with explicit threshold
	out, err := executeCommand(logPath, "--slow", "500")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	// 1200ms and 3500ms entries match >= 500
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines with duration >= 500ms, got %d:\n%s", len(lines), out)
	}

	// Test with default threshold (1000ms)
	outDefault, err := executeCommand(logPath, "--slow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	linesDefault := strings.Split(strings.TrimSpace(outDefault), "\n")
	// 1200ms and 3500ms entries match >= 1000
	if len(linesDefault) != 2 {
		t.Fatalf("expected 2 lines with duration >= 1000ms, got %d:\n%s", len(linesDefault), outDefault)
	}
}

func TestLogfilter_Fast(t *testing.T) {
	logPath := createSampleLogFile(t, "sample")
	defer func() { _ = os.Remove(logPath) }()

	out, err := executeCommand(logPath, "--fast", "100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	// only 50ms entry matches <= 100
	if len(lines) != 1 {
		t.Fatalf("expected 1 line with duration <= 100ms, got %d:\n%s", len(lines), out)
	}
}

func TestLogfilter_Scan(t *testing.T) {
	logPath := createSampleLogFile(t, "sample")
	defer func() { _ = os.Remove(logPath) }()

	out, err := executeCommand(logPath, "--scan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	// docsExamined=15000, nreturned=5 -> ratio=3000 > 100 and scanned > 10000
	if len(lines) != 1 {
		t.Fatalf("expected 1 line for table scan, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "mydb.users") {
		t.Errorf("expected mydb.users scan, got: %s", lines[0])
	}
}

func TestLogfilter_ComponentAndLevel(t *testing.T) {
	logPath := createSampleLogFile(t, "sample")
	defer func() { _ = os.Remove(logPath) }()

	out, err := executeCommand(logPath, "--component", "COMMAND", "--level", "W")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "mydb.orders") {
		t.Errorf("expected mydb.orders line, got: %s", lines[0])
	}
}

func TestLogfilter_Namespace(t *testing.T) {
	logPath := createSampleLogFile(t, "sample")
	defer func() { _ = os.Remove(logPath) }()

	out, err := executeCommand(logPath, "--namespace", "mydb.orders")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d:\n%s", len(lines), out)
	}
}

func TestLogfilter_Pattern(t *testing.T) {
	logPath := createSampleLogFile(t, "sample")
	defer func() { _ = os.Remove(logPath) }()

	out, err := executeCommand(logPath, "--pattern", `{"age": {"$gt": 1}}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line matching pattern, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "mydb.users") {
		t.Errorf("expected mydb.users line, got: %s", lines[0])
	}
}

func TestLogfilter_Transactions(t *testing.T) {
	logPath := createSampleLogFile(t, "sample")
	defer func() { _ = os.Remove(logPath) }()

	out, err := executeCommand(logPath, "--transactions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 transaction line, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "Slow transaction") {
		t.Errorf("expected Slow transaction line, got: %s", lines[0])
	}
}

func TestLogfilter_TimeRange(t *testing.T) {
	logPath := createSampleLogFile(t, "sample")
	defer func() { _ = os.Remove(logPath) }()

	out, err := executeCommand(logPath, "--from", "2023-10-10T10:04:00Z", "--to", "2023-10-10T10:12:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	// 10:05 and 10:10 match
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines in time range, got %d:\n%s", len(lines), out)
	}
}

func TestLogfilter_Exclude(t *testing.T) {
	logPath := createSampleLogFile(t, "sample")
	defer func() { _ = os.Remove(logPath) }()

	out, err := executeCommand(logPath, "--component", "COMMAND", "--exclude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	// 6 total lines - 2 COMMAND lines = 4 lines
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines excluded, got %d:\n%s", len(lines), out)
	}
}

func TestLogfilter_Shorten(t *testing.T) {
	logPath := createSampleLogFile(t, "sample")
	defer func() { _ = os.Remove(logPath) }()

	out, err := executeCommand(logPath, "--shorten", "50")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.SplitSeq(strings.TrimSpace(out), "\n")
	for l := range lines {
		if len(l) > 50 {
			t.Errorf("line length %d exceeds 50: %s", len(l), l)
		}
	}
}

func TestLogfilter_Human(t *testing.T) {
	logPath := createSampleLogFile(t, "sample")
	defer func() { _ = os.Remove(logPath) }()

	out, err := executeCommand(logPath, "--slow", "1000", "--human")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "1sec") && !strings.Contains(out, "3secs") {
		t.Errorf("expected human duration format, got:\n%s", out)
	}
}

func TestLogfilter_MergeMultipleFiles(t *testing.T) {
	// Create two files with interleaved timestamps
	lines1 := []string{
		fmt.Sprintf(`{"t":{"$date":"%s"},"s":"I","c":"COMMAND","id":1,"ctx":"conn1","msg":"Query 1"}`, time.Date(2023, 10, 10, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)),
		fmt.Sprintf(`{"t":{"$date":"%s"},"s":"I","c":"COMMAND","id":3,"ctx":"conn1","msg":"Query 3"}`, time.Date(2023, 10, 10, 10, 2, 0, 0, time.UTC).Format(time.RFC3339Nano)),
	}
	lines2 := []string{
		fmt.Sprintf(`{"t":{"$date":"%s"},"s":"I","c":"COMMAND","id":2,"ctx":"conn2","msg":"Query 2"}`, time.Date(2023, 10, 10, 10, 1, 0, 0, time.UTC).Format(time.RFC3339Nano)),
		fmt.Sprintf(`{"t":{"$date":"%s"},"s":"I","c":"COMMAND","id":4,"ctx":"conn2","msg":"Query 4"}`, time.Date(2023, 10, 10, 10, 3, 0, 0, time.UTC).Format(time.RFC3339Nano)),
	}

	f1, _ := os.CreateTemp("", "f1_*.log")
	defer func() { _ = os.Remove(f1.Name()) }()
	for _, l := range lines1 {
		_, _ = f1.WriteString(l + "\n")
	}
	_ = f1.Close()

	f2, _ := os.CreateTemp("", "f2_*.log")
	defer func() { _ = os.Remove(f2.Name()) }()
	for _, l := range lines2 {
		_, _ = f2.WriteString(l + "\n")
	}
	_ = f2.Close()

	out, err := executeCommand(f1.Name(), f2.Name(), "--markers", "enum")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 merged lines, got %d:\n%s", len(lines), out)
	}

	// Verify order: Query 1 ({1}), Query 2 ({2}), Query 3 ({1}), Query 4 ({2})
	if !strings.Contains(lines[0], "{1}") || !strings.Contains(lines[0], "Query 1") {
		t.Errorf("line 0 mismatch: %s", lines[0])
	}
	if !strings.Contains(lines[1], "{2}") || !strings.Contains(lines[1], "Query 2") {
		t.Errorf("line 1 mismatch: %s", lines[1])
	}
	if !strings.Contains(lines[2], "{1}") || !strings.Contains(lines[2], "Query 3") {
		t.Errorf("line 2 mismatch: %s", lines[2])
	}
	if !strings.Contains(lines[3], "{2}") || !strings.Contains(lines[3], "Query 4") {
		t.Errorf("line 3 mismatch: %s", lines[3])
	}
}

func TestLogfilter_Mask(t *testing.T) {
	// Main log file with lines from 10:00 to 10:20 (same as sample: 10:00, 10:01, 10:05, 10:10, 10:15, 10:20)
	mainLog := createSampleLogFile(t, "main_log")
	defer func() { _ = os.Remove(mainLog) }()

	// Mask file containing 1 event at 10:05:00
	maskLines := []string{
		fmt.Sprintf(`{"t":{"$date":"%s"},"s":"E","c":"CONTROL","id":999,"ctx":"main","msg":"Assertion failure"}`, time.Date(2023, 10, 10, 10, 5, 0, 0, time.UTC).Format(time.RFC3339Nano)),
	}
	maskLog, err := os.CreateTemp("", "mask_log_*.log")
	if err != nil {
		t.Fatalf("failed to create mask file: %v", err)
	}
	defer func() { _ = os.Remove(maskLog.Name()) }()
	for _, l := range maskLines {
		_, _ = maskLog.WriteString(l + "\n")
	}
	_ = maskLog.Close()

	// Mask size 120s (±60s): window is [10:04:00, 10:06:00]
	// Should only match line at 10:05:00
	out, err := executeCommand(mainLog, "--mask", maskLog.Name(), "--mask-size", "120")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 masked line, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "mydb.users") {
		t.Errorf("expected 10:05 line (mydb.users), got: %s", lines[0])
	}
}

func TestLogfilter_Color_BaseLevels(t *testing.T) {
	lines := []string{
		`{"t":{"$date":"2023-10-10T10:00:00.000Z"},"s":"E","c":"CONTROL","id":100,"ctx":"main","msg":"Error message"}`,
		`{"t":{"$date":"2023-10-10T10:01:00.000Z"},"s":"F","c":"CONTROL","id":101,"ctx":"main","msg":"Fatal crash"}`,
		`{"t":{"$date":"2023-10-10T10:02:00.000Z"},"s":"W","c":"COMMAND","id":102,"ctx":"conn1","msg":"Warning message"}`,
		`{"t":{"$date":"2023-10-10T10:03:00.000Z"},"s":"I","c":"NETWORK","id":103,"ctx":"listener","msg":"Info message"}`,
		`{"t":{"$date":"2023-10-10T10:04:00.000Z"},"s":"D1","c":"STORAGE","id":104,"ctx":"wt","msg":"Debug message"}`,
	}

	tmp, err := os.CreateTemp("", "color_test_*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	for _, l := range lines {
		_, _ = tmp.WriteString(l + "\n")
	}
	_ = tmp.Close()

	out, err := executeCommand(tmp.Name(), "--color=always")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outLines := strings.Split(strings.TrimSpace(out), "\n")
	if len(outLines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(outLines))
	}

	// Line 0: Error -> Red (\033[31m)
	if !strings.HasPrefix(outLines[0], "\033[31m") || !strings.HasSuffix(outLines[0], "\033[0m") {
		t.Errorf("expected Error line to start with Red (\\033[31m) and end with Reset, got: %q", outLines[0])
	}

	// Line 1: Fatal -> Red (\033[31m)
	if !strings.HasPrefix(outLines[1], "\033[31m") || !strings.HasSuffix(outLines[1], "\033[0m") {
		t.Errorf("expected Fatal line to start with Red (\\033[31m) and end with Reset, got: %q", outLines[1])
	}

	// Line 2: Warning -> Yellow (\033[33m)
	if !strings.HasPrefix(outLines[2], "\033[33m") || !strings.HasSuffix(outLines[2], "\033[0m") {
		t.Errorf("expected Warning line to start with Yellow (\\033[33m) and end with Reset, got: %q", outLines[2])
	}

	// Line 3: Info -> Light Grey (\033[37m)
	if !strings.HasPrefix(outLines[3], "\033[37m") || !strings.HasSuffix(outLines[3], "\033[0m") {
		t.Errorf("expected Info line to start with Light Grey (\\033[37m) and end with Reset, got: %q", outLines[3])
	}

	// Line 4: Debug -> Light Grey (\033[37m)
	if !strings.HasPrefix(outLines[4], "\033[37m") || !strings.HasSuffix(outLines[4], "\033[0m") {
		t.Errorf("expected Debug line to start with Light Grey (\\033[37m) and end with Reset, got: %q", outLines[4])
	}
}

func TestLogfilter_Color_FilterMatchHighlight(t *testing.T) {
	logPath := createSampleLogFile(t, "sample_color")
	defer func() { _ = os.Remove(logPath) }()

	// 1. Namespace filter highlight in Blue (\033[1;34m)
	outNs, err := executeCommand(logPath, "--color=always", "--namespace", "mydb.orders")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outNs, "\033[1;34mmydb.orders\033[0m") {
		t.Errorf("expected blue highlight on mydb.orders, got:\n%s", outNs)
	}

	// 2. Word filter highlight in Blue
	outWord, err := executeCommand(logPath, "--color=always", "--word", "Listening")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outWord, "\033[1;34mListening\033[0m") {
		t.Errorf("expected blue highlight on Listening, got:\n%s", outWord)
	}

	// 3. Slow filter highlight in Blue
	outSlow, err := executeCommand(logPath, "--color=always", "--slow", "1000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outSlow, "\033[1;34m\"durationMillis\":1200\033[0m") &&
		!strings.Contains(outSlow, "\033[1;34m\"durationMillis\": 1200\033[0m") {
		t.Errorf("expected blue highlight on durationMillis:1200, got:\n%s", outSlow)
	}
}

func TestLogfilter_Color_NoColorFlags(t *testing.T) {
	logPath := createSampleLogFile(t, "sample_nocolor")
	defer func() { _ = os.Remove(logPath) }()

	// 1. --color=never
	outNever, err := executeCommand(logPath, "--color=never")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(outNever, "\033[") {
		t.Errorf("expected no ANSI codes with --color=never, got:\n%s", outNever)
	}

	// 2. --no-color
	outNoColor, err := executeCommand(logPath, "--no-color")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(outNoColor, "\033[") {
		t.Errorf("expected no ANSI codes with --no-color, got:\n%s", outNoColor)
	}

	// 3. NO_COLOR env var
	t.Setenv("NO_COLOR", "1")
	outEnv, err := executeCommand(logPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(outEnv, "\033[") {
		t.Errorf("expected no ANSI codes with NO_COLOR=1, got:\n%s", outEnv)
	}
}
