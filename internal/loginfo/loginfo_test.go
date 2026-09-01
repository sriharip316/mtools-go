package loginfo

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sriharip316/mtools-go/internal/logfile"
	"github.com/sriharip316/mtools-go/internal/ui"
)

func createTestLogFile(t *testing.T, lines []string) (*logfile.LogFile, func()) {
	t.Helper()
	tmp, err := os.CreateTemp("", "loginfo_test_*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	for _, line := range lines {
		tmp.WriteString(line + "\n")
	}
	tmp.Close()

	lf, err := logfile.Open(tmp.Name())
	if err != nil {
		os.Remove(tmp.Name())
		t.Fatalf("failed to open logfile: %v", err)
	}

	cleanup := func() {
		lf.Close()
		os.Remove(tmp.Name())
	}
	return lf, cleanup
}

func captureOutput(f func()) string {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestPrintTable(t *testing.T) {
	rows := []map[string]string{
		{"ns": "test.coll", "op": "find", "count": "10"},
		{"ns": "admin.users", "op": "update", "count": "2"},
	}
	headers := []string{"ns", "op", "count"}

	out := captureOutput(func() {
		PrintTable(rows, headers, true, ui.Style{})
	})

	if !strings.Contains(out, "NS") || !strings.Contains(out, "OP") || !strings.Contains(out, "COUNT") {
		t.Errorf("expected uppercase headers, got:\n%s", out)
	}
	if !strings.Contains(out, "test.coll") || !strings.Contains(out, "admin.users") {
		t.Errorf("expected table rows, got:\n%s", out)
	}
}

func TestPrintTableColor(t *testing.T) {
	rows := []map[string]string{
		{"ns": "test.coll", "op": "find", "note": "COLLSCAN"},
		{"ns": "test.users", "op": "", "note": ""},
	}
	headers := []string{"ns", "op", "note"}

	plain := captureOutput(func() {
		PrintTable(rows, headers, false, ui.Style{})
	})
	colored := captureOutput(func() {
		PrintTable(rows, headers, false, ui.Style{Enabled: true})
	})

	if !strings.Contains(colored, ui.Yellow+"COLLSCAN"+ui.Reset) {
		t.Errorf("expected COLLSCAN highlighted yellow, got:\n%q", colored)
	}
	if !strings.Contains(colored, ui.Dim+"None") {
		t.Errorf("expected None placeholder dimmed, got:\n%q", colored)
	}
	if !strings.Contains(colored, ui.Bold+"ns") {
		t.Errorf("expected bold header row, got:\n%q", colored)
	}
	if ui.StripAnsi(colored) != plain {
		t.Errorf("colored table stripped of ANSI does not match plain table\ncolor: %q\nplain: %q", ui.StripAnsi(colored), plain)
	}
}

func TestExtractMetadata(t *testing.T) {
	lines := []string{
		`{"t":{"$date":"2023-10-10T10:00:00.000Z"},"s":"I","c":"CONTROL","id":20001,"ctx":"initandlisten","msg":"MongoDB starting","attr":{"host":"test-server","port":27017,"version":"6.0.5"}}`,
		`{"t":{"$date":"2023-10-10T10:01:00.000Z"},"s":"I","c":"CONTROL","id":22315,"ctx":"initandlisten","msg":"Options set by command line","attr":{"options":{"storage":{"engine":"wiredTiger"},"replication":{"replSetName":"myRS"}}}}`,
		`{"t":{"$date":"2023-10-10T10:05:00.000Z"},"s":"I","c":"COMMAND","id":51803,"ctx":"conn1","msg":"Slow query","attr":{"type":"command","ns":"test.users","durationMillis":100}}`,
	}

	lf, cleanup := createTestLogFile(t, lines)
	defer cleanup()

	meta, err := ExtractMetadata(lf)
	if err != nil {
		t.Fatalf("ExtractMetadata failed: %v", err)
	}

	if meta.Length != 3 {
		t.Errorf("expected length 3, got %d", meta.Length)
	}
	if meta.Host != "test-server" {
		t.Errorf("expected host test-server, got %s", meta.Host)
	}
	if meta.Port != "27017" {
		t.Errorf("expected port 27017, got %s", meta.Port)
	}
	if meta.Binary != "mongod" {
		t.Errorf("expected binary mongod, got %s", meta.Binary)
	}
	if meta.StorageEngine != "wiredTiger" {
		t.Errorf("expected storage wiredTiger, got %s", meta.StorageEngine)
	}
	if meta.ReplSet != "myRS" {
		t.Errorf("expected replSet myRS, got %s", meta.ReplSet)
	}
	if len(meta.Versions) == 0 || meta.Versions[0] != "6.0.5" {
		t.Errorf("expected version 6.0.5, got %v", meta.Versions)
	}
}

func TestRunQueries(t *testing.T) {
	lines := []string{
		`{"t":{"$date":"2023-10-10T10:05:00.000Z"},"s":"I","c":"COMMAND","id":51803,"ctx":"conn1","msg":"Slow query","attr":{"type":"find","ns":"app.orders","command":{"find":"orders","filter":{"status":"pending"}},"planSummary":"IXSCAN","durationMillis":150,"allowDiskUse":true}}`,
		`{"t":{"$date":"2023-10-10T10:06:00.000Z"},"s":"I","c":"COMMAND","id":51803,"ctx":"conn1","msg":"Slow query","attr":{"type":"find","ns":"app.orders","command":{"find":"orders","filter":{"status":"shipped"}},"planSummary":"IXSCAN","durationMillis":250,"allowDiskUse":true}}`,
		`{"t":{"$date":"2023-10-10T10:07:00.000Z"},"s":"I","c":"COMMAND","id":51803,"ctx":"conn2","msg":"Slow query","attr":{"type":"update","ns":"app.users","command":{"update":"users","filter":{"_id":1}},"durationMillis":50}}`,
	}

	lf, cleanup := createTestLogFile(t, lines)
	defer cleanup()

	opts := &Options{
		Queries:  true,
		Sort:     "sum",
		Rounding: 1,
	}

	out := captureOutput(func() {
		RunQueries(lf, opts)
	})

	if !strings.Contains(out, "namespace") || !strings.Contains(out, "app.orders") || !strings.Contains(out, "app.users") {
		t.Errorf("expected query table output, got:\n%s", out)
	}
	if !strings.Contains(out, "400") { // sum of 150 + 250
		t.Errorf("expected sum 400 for app.orders, got:\n%s", out)
	}
}

func TestRunDistinct(t *testing.T) {
	lines := []string{
		`{"t":{"$date":"2023-10-10T10:00:00.000Z"},"s":"I","c":"CONTROL","id":1,"ctx":"main","msg":"Message Alpha"}`,
		`{"t":{"$date":"2023-10-10T10:01:00.000Z"},"s":"I","c":"CONTROL","id":1,"ctx":"main","msg":"Message Alpha"}`,
		`{"t":{"$date":"2023-10-10T10:02:00.000Z"},"s":"I","c":"CONTROL","id":1,"ctx":"main","msg":"Message Alpha"}`,
		`{"t":{"$date":"2023-10-10T10:03:00.000Z"},"s":"I","c":"CONTROL","id":2,"ctx":"main","msg":"Message Beta"}`,
	}

	lf, cleanup := createTestLogFile(t, lines)
	defer cleanup()

	opts := &Options{
		Distinct:    true,
		DistinctMin: 2,
		Verbose:     false,
	}

	out := captureOutput(func() {
		RunDistinct(lf, opts)
	})

	if !strings.Contains(out, "Message Alpha") {
		t.Errorf("expected Message Alpha in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Distinct ignored 1 less informative lines") {
		t.Errorf("expected ignored lines notice for Message Beta (count 1 < 2), got:\n%s", out)
	}
}

func TestRunConnectionsAndStats(t *testing.T) {
	lines := []string{
		`{"t":{"$date":"2023-10-10T10:00:00.000Z"},"s":"I","c":"NETWORK","id":22943,"ctx":"listener","msg":"Connection accepted","attr":{"remote":"192.168.1.100:51234","connectionId":10}}`,
		`{"t":{"$date":"2023-10-10T10:00:05.000Z"},"s":"I","c":"NETWORK","id":22944,"ctx":"conn10","msg":"Connection ended","attr":{"remote":"192.168.1.100:51234","connectionId":10}}`,
		`{"t":{"$date":"2023-10-10T10:00:10.000Z"},"s":"W","c":"NETWORK","id":20000,"ctx":"conn11","msg":"SocketException occurred"}`,
	}

	lf, cleanup := createTestLogFile(t, lines)
	defer cleanup()

	opts := &Options{
		Connections: true,
		ConnStats:   true,
	}

	out := captureOutput(func() {
		RunConnections(lf, opts)
	})

	if !strings.Contains(out, "total opened: 1") || !strings.Contains(out, "total closed: 1") {
		t.Errorf("expected opened/closed counts, got:\n%s", out)
	}
	if !strings.Contains(out, "socket exceptions: 1") {
		t.Errorf("expected socket exceptions 1, got:\n%s", out)
	}
	if !strings.Contains(out, "overall average connection duration(s): 5") {
		t.Errorf("expected connection duration 5s, got:\n%s", out)
	}
	if !strings.Contains(out, "192.168.1.100") {
		t.Errorf("expected IP 192.168.1.100 in output, got:\n%s", out)
	}
}

func TestRunRestarts(t *testing.T) {
	meta := &LogMetadata{
		Restarts: []RestartInfo{
			{Time: time.Date(2023, 10, 10, 10, 0, 0, 0, time.UTC), Version: "6.0.5"},
		},
	}

	out := captureOutput(func() {
		RunRestarts(meta, &Options{Restarts: true})
	})

	if !strings.Contains(out, "Oct 10 10:00:00 version 6.0.5") {
		t.Errorf("expected restart line, got:\n%s", out)
	}
}

func TestRunTransactions(t *testing.T) {
	lines := []string{
		`{"t":{"$date":"2023-10-10T10:00:00.000Z"},"s":"I","c":"TXN","id":51804,"ctx":"conn1","msg":"Slow transaction","attr":{"type":"transaction","txnNumber":123,"autocommit":false,"readConcern":"snapshot","timeActiveMicros":5000,"timeInactiveMicros":1000,"durationMillis":6}}`,
	}

	lf, cleanup := createTestLogFile(t, lines)
	defer cleanup()

	out := captureOutput(func() {
		RunTransactions(lf, &Options{Transactions: true})
	})

	if !strings.Contains(out, "TXNNUMBER") || !strings.Contains(out, "123") || !strings.Contains(out, "snapshot") {
		t.Errorf("expected transaction output, got:\n%s", out)
	}
}

func TestRunClients(t *testing.T) {
	lines := []string{
		`{"t":{"$date":"2023-10-10T10:00:00.000Z"},"s":"I","c":"NETWORK","id":51800,"ctx":"conn1","msg":"Received client metadata","attr":{"remote":"10.0.0.1:44000","doc":{"driver":{"name":"mongo-go-driver","version":"v1.11.0"},"application":{"name":"testApp"}}}}`,
		`{"t":{"$date":"2023-10-10T10:00:01.000Z"},"s":"I","c":"ACCESS","id":5286307,"ctx":"conn1","msg":"Successfully authenticated as","attr":{"user":"appUser","db":"admin"}}`,
	}

	lf, cleanup := createTestLogFile(t, lines)
	defer cleanup()

	out := captureOutput(func() {
		RunClients(lf, &Options{Clients: true})
	})

	if !strings.Contains(out, "Driver: mongo-go-driver | Version: v1.11.0 | App: testApp") {
		t.Errorf("expected driver header, got:\n%s", out)
	}
	if !strings.Contains(out, "appUser@admin") {
		t.Errorf("expected user auth in output, got:\n%s", out)
	}
	if !strings.Contains(out, "10.0.0.1") {
		t.Errorf("expected IP in output, got:\n%s", out)
	}
}

func TestRunClients_Color(t *testing.T) {
	lines := []string{
		`{"t":{"$date":"2023-10-10T10:00:00.000Z"},"s":"I","c":"NETWORK","id":51800,"ctx":"conn1","msg":"Received client metadata","attr":{"remote":"10.0.0.1:44000","doc":{"driver":{"name":"mongo-go-driver","version":"v1.11.0"},"application":{"name":"testApp"}}}}`,
		`{"t":{"$date":"2023-10-10T10:00:01.000Z"},"s":"I","c":"ACCESS","id":5286307,"ctx":"conn1","msg":"Successfully authenticated as","attr":{"user":"appUser","db":"admin"}}`,
	}

	lf, cleanup := createTestLogFile(t, lines)
	defer cleanup()

	plain := captureOutput(func() {
		RunClients(lf, &Options{Clients: true})
	})

	if err := lf.Rewind(); err != nil {
		t.Fatalf("failed to rewind log file: %v", err)
	}

	colored := captureOutput(func() {
		RunClients(lf, &Options{Clients: true, Color: ui.Style{Enabled: true}})
	})

	if !strings.Contains(colored, "\x1b[36m") {
		t.Errorf("expected cyan key labels in colored output, got:\n%q", colored)
	}
	if !strings.Contains(colored, "\x1b[2m(1 auths)\x1b[0m") {
		t.Errorf("expected dimmed auth count in colored output, got:\n%q", colored)
	}
	if !strings.Contains(colored, "\x1b[2m(1 conns)\x1b[0m") {
		t.Errorf("expected dimmed conn count in colored output, got:\n%q", colored)
	}
	if got := ui.StripAnsi(colored); got != plain {
		t.Errorf("strip-ANSI mismatch:\nplain:   %q\ncolored: %q", plain, got)
	}
}
