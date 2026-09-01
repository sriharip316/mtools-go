package filter

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/sriharip316/mtools-go/internal/logevent"
)

func intPtr(i int) *int {
	return &i
}

func TestSlowFastFilter(t *testing.T) {
	slow := &SlowFilter{ThresholdMs: 100}
	fast := &FastFilter{ThresholdMs: 100}

	evNoDuration := &logevent.LogEvent{}
	ev50 := &logevent.LogEvent{Duration: intPtr(50)}
	ev100 := &logevent.LogEvent{Duration: intPtr(100)}
	ev150 := &logevent.LogEvent{Duration: intPtr(150)}

	if slow.Accept(evNoDuration) {
		t.Errorf("slow should reject nil duration")
	}
	if slow.Accept(ev50) {
		t.Errorf("slow should reject 50ms")
	}
	if !slow.Accept(ev100) {
		t.Errorf("slow should accept 100ms")
	}
	if !slow.Accept(ev150) {
		t.Errorf("slow should accept 150ms")
	}
	if slow.SkipRemaining() {
		t.Errorf("slow SkipRemaining should be false")
	}

	if fast.Accept(evNoDuration) {
		t.Errorf("fast should reject nil duration")
	}
	if !fast.Accept(ev50) {
		t.Errorf("fast should accept 50ms")
	}
	if !fast.Accept(ev100) {
		t.Errorf("fast should accept 100ms")
	}
	if fast.Accept(ev150) {
		t.Errorf("fast should reject 150ms")
	}
	if fast.SkipRemaining() {
		t.Errorf("fast SkipRemaining should be false")
	}
}

func TestTableScanFilter(t *testing.T) {
	scan := &TableScanFilter{}

	evNil := &logevent.LogEvent{}
	evLowScanned := &logevent.LogEvent{NScanned: intPtr(5000), NReturned: intPtr(1)}
	evLowRatio := &logevent.LogEvent{NScanned: intPtr(20000), NReturned: intPtr(500)}   // ratio 40
	evHighRatio := &logevent.LogEvent{NScanned: intPtr(20000), NReturned: intPtr(10)}   // ratio 2000
	evZeroReturned := &logevent.LogEvent{NScanned: intPtr(20000), NReturned: intPtr(0)} // ratio 20000

	if scan.Accept(evNil) {
		t.Errorf("scan should reject nil counters")
	}
	if scan.Accept(evLowScanned) {
		t.Errorf("scan should reject nscanned <= 10000")
	}
	if scan.Accept(evLowRatio) {
		t.Errorf("scan should reject ratio <= 100")
	}
	if !scan.Accept(evHighRatio) {
		t.Errorf("scan should accept high ratio")
	}
	if !scan.Accept(evZeroReturned) {
		t.Errorf("scan should accept zero returned with high scanned")
	}
}

func TestWordFilter(t *testing.T) {
	wf, err := NewWordFilter([]string{"foo.*bar", "baz"})
	if err != nil {
		t.Fatalf("failed to create WordFilter: %v", err)
	}

	if !wf.Accept(&logevent.LogEvent{LineStr: "hello foo123bar world"}) {
		t.Errorf("expected match for foo.*bar")
	}
	if !wf.Accept(&logevent.LogEvent{LineStr: "something baz here"}) {
		t.Errorf("expected match for baz")
	}
	if wf.Accept(&logevent.LogEvent{LineStr: "nothing matching"}) {
		t.Errorf("expected reject for non-matching")
	}
}

func TestTransactionFilter(t *testing.T) {
	tf := &TransactionFilter{}
	if !tf.Accept(&logevent.LogEvent{LineStr: "a transaction occurred"}) {
		t.Errorf("expected match")
	}
	if tf.Accept(&logevent.LogEvent{LineStr: "regular query"}) {
		t.Errorf("expected reject")
	}
}

func TestLogLineFilter(t *testing.T) {
	f, err := NewLogLineFilter(
		[]string{"COMMAND", "QUERY"},
		[]string{"I", "W"},
		[]string{"db1.coll1"},
		[]string{"command"},
		[]string{"conn1", "conn2"},
		[]string{"find"},
		`{"a": 123}`,
		[]string{"IXSCAN", "COLLSCAN"},
	)
	if err != nil {
		t.Fatalf("unexpected error creating LogLineFilter: %v", err)
	}

	if !f.Active() {
		t.Fatalf("expected filter to be active")
	}

	evValid := &logevent.LogEvent{
		Component:   "COMMAND",
		Level:       "I",
		Namespace:   "db1.coll1",
		Operation:   "command",
		Thread:      "conn1",
		Command:     "find",
		Pattern:     `{"a": 1}`,
		PlanSummary: "IXSCAN",
	}
	if !f.Accept(evValid) {
		t.Errorf("expected valid event to be accepted")
	}

	// Mismatched component
	evBadComp := *evValid
	evBadComp.Component = "STORAGE"
	if f.Accept(&evBadComp) {
		t.Errorf("expected bad component to be rejected")
	}

	// Mismatched pattern
	evBadPattern := *evValid
	evBadPattern.Pattern = `{"b": 1}`
	if f.Accept(&evBadPattern) {
		t.Errorf("expected bad pattern to be rejected")
	}
}

func TestDateTimeFilter(t *testing.T) {
	start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2023, 1, 10, 0, 0, 0, 0, time.UTC)

	dtFilter, err := NewDateTimeFilter("2023-01-02", "2023-01-05", start, end, false)
	if err != nil {
		t.Fatalf("failed to create DateTimeFilter: %v", err)
	}

	evBefore := &logevent.LogEvent{DateTime: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)}
	evInside := &logevent.LogEvent{DateTime: time.Date(2023, 1, 3, 12, 0, 0, 0, time.UTC)}
	evAfter := &logevent.LogEvent{DateTime: time.Date(2023, 1, 6, 12, 0, 0, 0, time.UTC)}

	if dtFilter.Accept(evBefore) {
		t.Errorf("expected evBefore to be rejected")
	}
	if dtFilter.SkipRemaining() {
		t.Errorf("SkipRemaining should be false before end")
	}

	if !dtFilter.Accept(evInside) {
		t.Errorf("expected evInside to be accepted")
	}
	if dtFilter.SkipRemaining() {
		t.Errorf("SkipRemaining should be false during range")
	}

	if dtFilter.Accept(evAfter) {
		t.Errorf("expected evAfter to be rejected")
	}
	if !dtFilter.SkipRemaining() {
		t.Errorf("SkipRemaining should be true after range")
	}
}

func createTempMaskFile(t *testing.T, lines []string) string {
	t.Helper()
	tmp, err := os.CreateTemp("", "mask_test_*.log")
	if err != nil {
		t.Fatalf("failed to create temp mask file: %v", err)
	}
	defer tmp.Close()
	for _, l := range lines {
		tmp.WriteString(l + "\n")
	}
	return tmp.Name()
}

func TestMaskFilter_CenteringAndMerging(t *testing.T) {
	// Two events:
	// 1. at 10:00:00 with duration 20s (20000ms)
	// 2. at 10:00:40 with duration 10s (10000ms)
	lines := []string{
		fmt.Sprintf(`{"t":{"$date":"%s"},"s":"I","c":"COMMAND","id":1,"ctx":"conn1","msg":"Query 1","attr":{"durationMillis":20000}}`, time.Date(2023, 10, 10, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)),
		fmt.Sprintf(`{"t":{"$date":"%s"},"s":"I","c":"COMMAND","id":2,"ctx":"conn1","msg":"Query 2","attr":{"durationMillis":10000}}`, time.Date(2023, 10, 10, 10, 0, 40, 0, time.UTC).Format(time.RFC3339Nano)),
	}

	maskFile := createTempMaskFile(t, lines)
	defer os.Remove(maskFile)

	// Test 1: mask-center=end with mask-size=60 (halfPadding=30s)
	// Event 1 end: 10:00:00 -> [09:59:30, 10:00:30]
	// Event 2 end: 10:00:40 -> [10:00:10, 10:01:10]
	// These overlap! [09:59:30, 10:00:30] and [10:00:10, 10:01:10] merge into [09:59:30, 10:01:10]
	mfEnd, err := NewMaskFilter(maskFile, 60, "end")
	if err != nil {
		t.Fatalf("failed to create MaskFilter (end): %v", err)
	}

	intervalsEnd := mfEnd.Intervals()
	if len(intervalsEnd) != 1 {
		t.Fatalf("expected 1 merged interval, got %d", len(intervalsEnd))
	}
	expectedStart := time.Date(2023, 10, 10, 9, 59, 30, 0, time.UTC)
	expectedEnd := time.Date(2023, 10, 10, 10, 1, 10, 0, time.UTC)
	if !intervalsEnd[0].Start.Equal(expectedStart) || !intervalsEnd[0].End.Equal(expectedEnd) {
		t.Errorf("expected [%v, %v], got [%v, %v]", expectedStart, expectedEnd, intervalsEnd[0].Start, intervalsEnd[0].End)
	}

	// Test StartLimit
	if !mfEnd.StartLimit().Equal(expectedStart) {
		t.Errorf("expected StartLimit %v, got %v", expectedStart, mfEnd.StartLimit())
	}

	// Test Accept inside interval
	evInside := &logevent.LogEvent{DateTime: time.Date(2023, 10, 10, 10, 0, 15, 0, time.UTC)}
	if !mfEnd.Accept(evInside) {
		t.Errorf("expected evInside to be accepted")
	}

	// Test Accept before interval
	evBefore := &logevent.LogEvent{DateTime: time.Date(2023, 10, 10, 9, 59, 0, 0, time.UTC)}
	if mfEnd.Accept(evBefore) {
		t.Errorf("expected evBefore to be rejected")
	}

	// Test Accept after interval and SkipRemaining
	evAfter := &logevent.LogEvent{DateTime: time.Date(2023, 10, 10, 10, 2, 0, 0, time.UTC)}
	if mfEnd.Accept(evAfter) {
		t.Errorf("expected evAfter to be rejected")
	}
	if !mfEnd.SkipRemaining() {
		t.Errorf("expected SkipRemaining to be true after last interval")
	}

	// Test 2: mask-center=start with mask-size=20 (halfPadding=10s)
	// Event 1 start: 10:00:00 - 20s = 09:59:40 -> [09:59:30, 09:59:50]
	// Event 2 start: 10:00:40 - 10s = 10:00:30 -> [10:00:20, 10:00:40]
	// These do NOT overlap -> 2 disjoint intervals
	mfStart, err := NewMaskFilter(maskFile, 20, "start")
	if err != nil {
		t.Fatalf("failed to create MaskFilter (start): %v", err)
	}
	intervalsStart := mfStart.Intervals()
	if len(intervalsStart) != 2 {
		t.Fatalf("expected 2 intervals, got %d", len(intervalsStart))
	}
	if !intervalsStart[0].Start.Equal(time.Date(2023, 10, 10, 9, 59, 30, 0, time.UTC)) ||
		!intervalsStart[0].End.Equal(time.Date(2023, 10, 10, 9, 59, 50, 0, time.UTC)) {
		t.Errorf("unexpected interval 0: %v", intervalsStart[0])
	}
	if !intervalsStart[1].Start.Equal(time.Date(2023, 10, 10, 10, 0, 20, 0, time.UTC)) ||
		!intervalsStart[1].End.Equal(time.Date(2023, 10, 10, 10, 0, 40, 0, time.UTC)) {
		t.Errorf("unexpected interval 1: %v", intervalsStart[1])
	}
}

func TestGetBaseColor(t *testing.T) {
	tests := []struct {
		level    string
		expected string
	}{
		{"E", ColorRed},
		{"e", ColorRed},
		{"ERROR", ColorRed},
		{"F", ColorRed},
		{"FATAL", ColorRed},
		{"W", ColorYellow},
		{"w", ColorYellow},
		{"WARN", ColorYellow},
		{"WARNING", ColorYellow},
		{"I", ColorGrey},
		{"D1", ColorGrey},
		{"D2", ColorGrey},
		{"", ColorGrey},
		{"UNKNOWN", ColorGrey},
	}

	for _, tt := range tests {
		got := GetBaseColor(tt.level)
		if got != tt.expected {
			t.Errorf("GetBaseColor(%q) = %q, expected %q", tt.level, got, tt.expected)
		}
	}
}

func TestMergeSpans(t *testing.T) {
	tests := []struct {
		name     string
		input    []Span
		expected []Span
	}{
		{
			name:     "empty",
			input:    nil,
			expected: nil,
		},
		{
			name:     "single",
			input:    []Span{{Start: 5, End: 10}},
			expected: []Span{{Start: 5, End: 10}},
		},
		{
			name:     "disjoint",
			input:    []Span{{Start: 0, End: 5}, {Start: 10, End: 15}},
			expected: []Span{{Start: 0, End: 5}, {Start: 10, End: 15}},
		},
		{
			name:     "overlapping",
			input:    []Span{{Start: 0, End: 8}, {Start: 5, End: 12}},
			expected: []Span{{Start: 0, End: 12}},
		},
		{
			name:     "adjacent",
			input:    []Span{{Start: 0, End: 5}, {Start: 5, End: 10}},
			expected: []Span{{Start: 0, End: 10}},
		},
		{
			name:     "nested",
			input:    []Span{{Start: 0, End: 20}, {Start: 5, End: 10}},
			expected: []Span{{Start: 0, End: 20}},
		},
		{
			name:     "unsorted and invalid",
			input:    []Span{{Start: 15, End: 20}, {Start: -1, End: 5}, {Start: 5, End: 5}, {Start: 0, End: 5}},
			expected: []Span{{Start: 0, End: 5}, {Start: 15, End: 20}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeSpans(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d spans, got %d", len(tt.expected), len(got))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("span %d: expected %+v, got %+v", i, tt.expected[i], got[i])
				}
			}
		})
	}
}

func TestColorize(t *testing.T) {
	line := "Hello world foo bar"
	baseColor := ColorGrey

	// No spans -> whole line wrapped in baseColor
	c0 := Colorize(line, baseColor, nil)
	expected0 := ColorGrey + "Hello world foo bar" + ColorReset
	if c0 != expected0 {
		t.Errorf("expected %q, got %q", expected0, c0)
	}

	// Single span on "world" [6, 11]
	c1 := Colorize(line, baseColor, []Span{{Start: 6, End: 11}})
	expected1 := ColorGrey + "Hello " + ColorReset + ColorBlue + "world" + ColorReset + ColorGrey + " foo bar" + ColorReset
	if c1 != expected1 {
		t.Errorf("expected %q, got %q", expected1, c1)
	}

	// Multiple spans: "Hello" [0, 5] and "bar" [16, 19]
	c2 := Colorize(line, baseColor, []Span{{Start: 0, End: 5}, {Start: 16, End: 19}})
	expected2 := ColorBlue + "Hello" + ColorReset + ColorGrey + " world foo " + ColorReset + ColorBlue + "bar" + ColorReset
	if c2 != expected2 {
		t.Errorf("expected %q, got %q", expected2, c2)
	}
}

func TestHighlighters(t *testing.T) {
	line := `{"t":{"$date":"2023-10-10T10:00:00.000Z"},"s":"W","c":"COMMAND","ctx":"conn1","msg":"Slow query","attr":{"ns":"mydb.orders","command":{"find":"orders"},"planSummary":"COLLSCAN","durationMillis":1200}}`
	ev := &logevent.LogEvent{
		LineStr:     line,
		Level:       "W",
		Component:   "COMMAND",
		Namespace:   "mydb.orders",
		Command:     "find",
		PlanSummary: "COLLSCAN",
		Duration:    intPtr(1200),
	}

	// WordFilter highlighter
	wf, _ := NewWordFilter([]string{"orders", "find"})
	wfSpans := wf.HighlightSpans(line, ev)
	if len(wfSpans) < 2 {
		t.Errorf("expected at least 2 spans for word filter, got %d", len(wfSpans))
	}

	// LogLineFilter highlighter
	llf, _ := NewLogLineFilter([]string{"COMMAND"}, []string{"W"}, []string{"mydb.orders"}, nil, nil, []string{"find"}, "", []string{"COLLSCAN"})
	llfSpans := llf.HighlightSpans(line, ev)
	if len(llfSpans) < 5 {
		t.Errorf("expected at least 5 spans for logline filter, got %d", len(llfSpans))
	}

	// SlowFilter highlighter
	sf := &SlowFilter{ThresholdMs: 500}
	sfSpans := sf.HighlightSpans(line, ev)
	if len(sfSpans) == 0 {
		t.Errorf("expected durationMillis spans for slow filter")
	}

	// TableScanFilter highlighter
	tsf := &TableScanFilter{}
	tsfSpans := tsf.HighlightSpans(line, ev)
	if len(tsfSpans) == 0 {
		t.Errorf("expected COLLSCAN span for table scan filter")
	}

	// TransactionFilter highlighter
	txLine := `{"msg":"a transaction log"}`
	txEv := &logevent.LogEvent{LineStr: txLine}
	tf := &TransactionFilter{}
	tfSpans := tf.HighlightSpans(txLine, txEv)
	if len(tfSpans) == 0 {
		t.Errorf("expected transaction span for transaction filter")
	}
}
