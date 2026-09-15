package logevent

import (
	"encoding/json"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sriharip316/mtools-go/internal/pattern"
)

// rawDoc is the internal struct for JSON unmarshaling.
type rawDoc struct {
	T    rawTimestamp   `json:"t"`
	S    string         `json:"s"`
	C    string         `json:"c"`
	ID   int            `json:"id"`
	Ctx  string         `json:"ctx"`
	Msg  string         `json:"msg"`
	Attr map[string]any `json:"attr"`
}

type rawTimestamp struct {
	Date string `json:"$date"`
}

// LogEvent represents a parsed MongoDB LogV2 log entry.
type LogEvent struct {
	LineStr            string    // Original JSON line
	DateTime           time.Time // Parsed timestamp
	Level              string    // F, E, W, I, D1, D2, D3, D4, D5
	Component          string    // COMMAND, QUERY, NETWORK, STORAGE, etc.
	ID                 int       // Stable message ID
	Thread             string    // Connection/thread context
	Conn               string    // Connection ID (e.g. "conn123")
	Msg                string    // Message text
	Duration           *int      // durationMillis
	Operation          string    // attr.type
	Namespace          string    // attr.ns or attr.namespace
	Command            string    // First key of attr.command map
	Pattern            string    // Normalized query pattern
	SortPattern        string    // Normalized sort pattern
	PlanSummary        string    // attr.planSummary
	NScanned           *int      // attr.keysExamined
	NScannedObjects    *int      // attr.docsExamined
	NReturned          *int      // attr.nreturned or attr.nMatched
	NInserted          *int      // attr.nInserted
	NDeleted           *int      // attr.nDeleted
	NUpdated           *int      // attr.nModified
	NumYields          *int      // attr.numYields
	AllowDiskUse       *bool     // allowDiskUse
	RemoteIP           string    // Remote client IP address
	ConnectionID       *int      // Parsed integer connection ID
	BytesRead          *int64    // bytesRead
	BytesWritten       *int64    // bytesWritten
	TimeReadingMicros  *int64    // timeReadingMicros
	TimeWritingMicros  *int64    // timeWritingMicros
	TxnNumber          *int64    // txnNumber
	Autocommit         *bool     // autocommit
	ReadConcern        string    // readConcern
	TimeActiveMicros   *int64    // timeActiveMicros
	TimeInactiveMicros *int64    // timeInactiveMicros
	CursorID           string    // cursorid
	ReapedTime         string    // reapedtime
	doc                rawDoc    // Raw document for JSON output
}

// Attr returns the parsed attr map from the log event.
func (ev *LogEvent) Attr() map[string]any {
	return ev.doc.Attr
}

var firstCommandKeyRegex = regexp.MustCompile(`"command"\s*:\s*\{\s*"([^"]+)"`)

// Parse parses a JSON log line into a LogEvent.
func Parse(line string) (*LogEvent, error) {
	var doc rawDoc
	if err := json.Unmarshal([]byte(line), &doc); err != nil {
		return nil, err
	}

	ev := &LogEvent{
		LineStr:   line,
		Level:     doc.S,
		Component: doc.C,
		ID:        doc.ID,
		Thread:    doc.Ctx,
		Msg:       doc.Msg,
		doc:       doc,
	}

	if strings.HasPrefix(ev.Thread, "conn") {
		ev.Conn = ev.Thread
	}

	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.000Z07:00",
		"2006-01-02T15:04:05.000-0700",
	}
	for _, layout := range layouts {
		t, err := time.Parse(layout, doc.T.Date)
		if err == nil {
			ev.DateTime = t
			break
		}
	}

	ev.parseAttr()
	return ev, nil
}

// parseAttr extracts fields from the attr map.
func (ev *LogEvent) parseAttr() {
	if ev.doc.Attr == nil {
		return
	}

	attr := ev.doc.Attr

	ev.Duration = getIntPtr(attr, "durationMillis")

	if ns := getStr(attr, "ns"); ns != "" {
		ev.Namespace = ns
	} else if ns := getStr(attr, "namespace"); ns != "" {
		ev.Namespace = ns
	}

	ev.Operation = getStr(attr, "type")
	ev.PlanSummary = getStr(attr, "planSummary")

	ev.NScanned = getIntPtr(attr, "keysExamined")
	ev.NScannedObjects = getIntPtr(attr, "docsExamined")

	if n := getIntPtr(attr, "nreturned"); n != nil {
		ev.NReturned = n
	} else if n := getIntPtr(attr, "nReturned"); n != nil {
		ev.NReturned = n
	} else if n := getIntPtr(attr, "nMatched"); n != nil {
		ev.NReturned = n
	}

	ev.NInserted = getIntPtr(attr, "nInserted")
	ev.NDeleted = getIntPtr(attr, "nDeleted")

	if n := getIntPtr(attr, "nModified"); n != nil {
		ev.NUpdated = n
	}

	ev.NumYields = getIntPtr(attr, "numYields")

	// Parse allowDiskUse
	if b := getBoolPtr(attr, "allowDiskUse"); b != nil {
		ev.AllowDiskUse = b
	}

	if cmdMap, ok := attr["command"].(map[string]any); ok {
		// First try regex match on raw line for accurate first key
		matches := firstCommandKeyRegex.FindStringSubmatch(ev.LineStr)
		if len(matches) > 1 {
			ev.Command = matches[1]
		} else {
			// Fallback: collect and sort candidates for deterministic selection
			var candidateKeys []string
			for k := range cmdMap {
				if !strings.HasPrefix(k, "$") && k != "filter" && k != "sort" && k != "limit" &&
					k != "pipeline" && k != "projection" && k != "skip" && k != "lsid" &&
					k != "txnNumber" && k != "autocommit" && k != "comment" && k != "writeConcern" && k != "readConcern" {
					candidateKeys = append(candidateKeys, k)
				}
			}
			if len(candidateKeys) > 0 {
				sort.Strings(candidateKeys)
				ev.Command = candidateKeys[0]
			}
		}

		if ev.AllowDiskUse == nil {
			if b := getBoolPtr(cmdMap, "allowDiskUse"); b != nil {
				ev.AllowDiskUse = b
			}
		}

		if filter, ok := cmdMap["filter"]; ok {
			ev.Pattern = pattern.JSON2Pattern(filter)
		} else if pipeline, ok := cmdMap["pipeline"]; ok {
			ev.Pattern = pattern.JSON2Pattern(pipeline)
		}

		if sort, ok := cmdMap["sort"]; ok {
			ev.SortPattern = pattern.JSON2Pattern(sort)
		}
	}

	// Remote and Connection ID parsing
	if rem := getStr(attr, "remote"); rem != "" {
		ev.RemoteIP = cleanIP(rem)
	} else if rem := getStr(attr, "client"); rem != "" {
		ev.RemoteIP = cleanIP(rem)
	} else if rem := getStr(attr, "address"); rem != "" {
		ev.RemoteIP = cleanIP(rem)
	}

	if cid := getIntPtr(attr, "connectionId"); cid != nil {
		ev.ConnectionID = cid
	} else if after, ok := strings.CutPrefix(ev.Thread, "conn"); ok {
		numStr := after
		if n, err := strconv.Atoi(numStr); err == nil {
			ev.ConnectionID = &n
		}
	}

	// Storage statistics
	ev.BytesRead = getInt64Ptr(attr, "bytesRead")
	ev.BytesWritten = getInt64Ptr(attr, "bytesWritten")
	ev.TimeReadingMicros = getInt64Ptr(attr, "timeReadingMicros")
	ev.TimeWritingMicros = getInt64Ptr(attr, "timeWritingMicros")
	if storageMap, ok := attr["storage"].(map[string]any); ok {
		if dataMap, ok := storageMap["data"].(map[string]any); ok {
			if ev.BytesRead == nil {
				ev.BytesRead = getInt64Ptr(dataMap, "bytesRead")
			}
			if ev.BytesWritten == nil {
				ev.BytesWritten = getInt64Ptr(dataMap, "bytesWritten")
			}
			if ev.TimeReadingMicros == nil {
				ev.TimeReadingMicros = getInt64Ptr(dataMap, "timeReadingMicros")
			}
			if ev.TimeWritingMicros == nil {
				ev.TimeWritingMicros = getInt64Ptr(dataMap, "timeWritingMicros")
			}
		}
	}

	// Transactions
	ev.TxnNumber = getInt64Ptr(attr, "txnNumber")
	ev.Autocommit = getBoolPtr(attr, "autocommit")
	ev.ReadConcern = getStr(attr, "readConcern")
	ev.TimeActiveMicros = getInt64Ptr(attr, "timeActiveMicros")
	ev.TimeInactiveMicros = getInt64Ptr(attr, "timeInactiveMicros")
	if paramMap, ok := attr["parameters"].(map[string]any); ok {
		if ev.TxnNumber == nil {
			ev.TxnNumber = getInt64Ptr(paramMap, "txnNumber")
		}
		if ev.Autocommit == nil {
			ev.Autocommit = getBoolPtr(paramMap, "autocommit")
		}
		if ev.ReadConcern == "" {
			if rcMap, ok := paramMap["readConcern"].(map[string]any); ok {
				ev.ReadConcern = getStr(rcMap, "level")
			} else {
				ev.ReadConcern = getStr(paramMap, "readConcern")
			}
		}
	}

	// Cursors
	if cid := getStr(attr, "cursorid"); cid != "" {
		ev.CursorID = cid
	} else if cid := getStr(attr, "cursorId"); cid != "" {
		ev.CursorID = cid
	} else if cid := getInt64Ptr(attr, "cursorid"); cid != nil {
		ev.CursorID = strconv.FormatInt(*cid, 10)
	} else if cid := getInt64Ptr(attr, "cursorId"); cid != nil {
		ev.CursorID = strconv.FormatInt(*cid, 10)
	}
	if rt := getStr(attr, "reapedtime"); rt != "" {
		ev.ReapedTime = rt
	} else if rt := getStr(attr, "reapedTime"); rt != "" {
		ev.ReapedTime = rt
	}
}

// ToJSON returns JSON representation.
func (ev *LogEvent) ToJSON(pretty bool) string {
	if pretty {
		data, err := json.MarshalIndent(ev.doc, "", "  ")
		if err == nil {
			return string(data)
		}
	}
	return ev.LineStr
}

func cleanIP(addr string) string {
	addr = strings.TrimSpace(addr)
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(addr, "[]")
}

func getStr(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getIntPtr(m map[string]any, key string) *int {
	if v, ok := m[key]; ok {
		switch num := v.(type) {
		case float64:
			i := int(num)
			return &i
		case int:
			return &num
		case int64:
			i := int(num)
			return &i
		}
	}
	return nil
}

func getInt64Ptr(m map[string]any, key string) *int64 {
	if v, ok := m[key]; ok {
		switch num := v.(type) {
		case float64:
			i := int64(num)
			return &i
		case int:
			i := int64(num)
			return &i
		case int64:
			return &num
		}
	}
	return nil
}

func getBoolPtr(m map[string]any, key string) *bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return &b
		}
	}
	return nil
}
