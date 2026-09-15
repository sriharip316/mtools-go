package logevent

import (
	"strings"
	"testing"
	"time"
)

func TestParse_Basic(t *testing.T) {
	line := `{"t":{"$date":"2023-10-10T07:51:00.000+00:00"},"s":"I","c":"COMMAND","id":51803,"ctx":"conn123","msg":"Slow query","attr":{"type":"command","ns":"myDb.myColl","command":{"find":"myColl","filter":{"status":"active","age":{"$gt":21}},"sort":{"name":1},"limit":100},"planSummary":"IXSCAN { status: 1 }","keysExamined":5000,"docsExamined":5000,"numYields":2,"nreturned":100,"durationMillis":150}}`
	ev, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	expectedTime, _ := time.Parse(time.RFC3339Nano, "2023-10-10T07:51:00.000+00:00")
	if !ev.DateTime.Equal(expectedTime) {
		t.Errorf("Expected DateTime %v, got %v", expectedTime, ev.DateTime)
	}
	if ev.Level != "I" {
		t.Errorf("Expected Level I, got %v", ev.Level)
	}
	if ev.Component != "COMMAND" {
		t.Errorf("Expected Component COMMAND, got %v", ev.Component)
	}
	if ev.ID != 51803 {
		t.Errorf("Expected ID 51803, got %v", ev.ID)
	}
	if ev.Thread != "conn123" {
		t.Errorf("Expected Thread conn123, got %v", ev.Thread)
	}
	if ev.Conn != "conn123" {
		t.Errorf("Expected Conn conn123, got %v", ev.Conn)
	}
	if ev.Msg != "Slow query" {
		t.Errorf("Expected Msg 'Slow query', got %v", ev.Msg)
	}

	if ev.Duration == nil || *ev.Duration != 150 {
		t.Errorf("Expected Duration 150, got %v", ev.Duration)
	}
	if ev.Operation != "command" {
		t.Errorf("Expected Operation 'command', got %v", ev.Operation)
	}
	if ev.Namespace != "myDb.myColl" {
		t.Errorf("Expected Namespace 'myDb.myColl', got %v", ev.Namespace)
	}
	if ev.Command != "find" {
		t.Errorf("Expected Command 'find', got %v", ev.Command)
	}
	if ev.NScanned == nil || *ev.NScanned != 5000 {
		t.Errorf("Expected NScanned 5000, got %v", ev.NScanned)
	}
	if ev.NScannedObjects == nil || *ev.NScannedObjects != 5000 {
		t.Errorf("Expected NScannedObjects 5000, got %v", ev.NScannedObjects)
	}
	if ev.NReturned == nil || *ev.NReturned != 100 {
		t.Errorf("Expected NReturned 100, got %v", ev.NReturned)
	}
	if ev.NumYields == nil || *ev.NumYields != 2 {
		t.Errorf("Expected NumYields 2, got %v", ev.NumYields)
	}
	if ev.PlanSummary != "IXSCAN { status: 1 }" {
		t.Errorf("Expected PlanSummary 'IXSCAN { status: 1 }', got %v", ev.PlanSummary)
	}
}

func TestParse_NoAttr(t *testing.T) {
	line := `{"t":{"$date":"2023-01-01T00:00:00.000Z"},"s":"I","c":"CONTROL","id":12345,"ctx":"main","msg":"Starting up"}`
	ev, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if ev.Component != "CONTROL" {
		t.Errorf("Expected Component CONTROL, got %v", ev.Component)
	}
	if ev.Msg != "Starting up" {
		t.Errorf("Expected Msg 'Starting up', got %v", ev.Msg)
	}
	if ev.Operation != "" {
		t.Errorf("Expected empty Operation, got %v", ev.Operation)
	}
	if ev.Duration != nil {
		t.Errorf("Expected nil Duration, got %v", ev.Duration)
	}
}

func TestParse_UpdateWithMatched(t *testing.T) {
	line := `{"t":{"$date":"2023-06-15T12:00:00.000+05:30"},"s":"I","c":"COMMAND","id":51803,"ctx":"conn456","msg":"Slow query","attr":{"type":"update","ns":"db.coll","nMatched":5,"nModified":3,"durationMillis":200}}`
	ev, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if ev.NReturned == nil || *ev.NReturned != 5 {
		t.Errorf("Expected NReturned (nMatched) 5, got %v", ev.NReturned)
	}
	if ev.NUpdated == nil || *ev.NUpdated != 3 {
		t.Errorf("Expected NUpdated (nModified) 3, got %v", ev.NUpdated)
	}
}

func TestParse_InvalidJSON(t *testing.T) {
	line := `{"t":{"$date":"2023-06-15T12`
	_, err := Parse(line)
	if err == nil {
		t.Errorf("Expected error for invalid JSON, got nil")
	}
}

func TestToJSON(t *testing.T) {
	line := `{"t":{"$date":"2023-01-01T00:00:00.000Z"},"s":"I","c":"CONTROL","id":12345,"ctx":"main","msg":"Starting up"}`
	ev, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	strNonPretty := ev.ToJSON(false)
	if strNonPretty != line {
		t.Errorf("Expected %v, got %v", line, strNonPretty)
	}

	strPretty := ev.ToJSON(true)
	if !strings.Contains(strPretty, `"msg": "Starting up"`) {
		t.Errorf("Expected pretty JSON to contain correctly indented key, got %v", strPretty)
	}
}

func TestCleanIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.10:50000", "192.168.1.10"},
		{"127.0.0.1", "127.0.0.1"},
		{"[::1]:54321", "::1"},
		{"::1", "::1"},
		{"[2001:db8::1]:27017", "2001:db8::1"},
		{"localhost:27017", "localhost"},
	}

	for _, tt := range tests {
		got := cleanIP(tt.input)
		if got != tt.expected {
			t.Errorf("cleanIP(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
