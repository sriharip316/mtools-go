package loginfo

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sriharip316/mtools-go/internal/logfile"
	"github.com/sriharip316/mtools-go/internal/ui"
)

// Options holds CLI parameters for loginfo.
type Options struct {
	Queries      bool
	Distinct     bool
	DistinctMin  int
	Connections  bool
	ConnStats    bool
	Restarts     bool
	RsState      bool
	RsInfo       bool
	Transactions bool
	TSort        string
	Cursors      bool
	StorageStats bool
	Sharding     bool
	Errors       bool
	Migrations   bool
	Clients      bool
	Sort         string // default: "sum"
	Rounding     int    // default: 1
	Verbose      bool
	Debug        bool
	Color        ui.Style // zero value disables styling
}

// RestartInfo captures server restart events.
type RestartInfo struct {
	Time    time.Time
	Version string
}

// LogMetadata holds general metadata extracted from a log file.
type LogMetadata struct {
	Source          string
	Host            string
	Port            string
	Start           time.Time
	End             time.Time
	DateFormat      string
	Length          int
	Binary          string
	ClusterRole     string
	Versions        []string
	StorageEngine   string
	ReplSet         string
	ReplSetMembers  string
	ReplSetVersion  string
	ReplSetProtocol string
	Shards          [][2]string // [shardName, hosts]
	CSRS            [2]string   // [name, hosts]
	Restarts        []RestartInfo
}

// ExtractMetadata scans the log file to extract server metadata and line count.
func ExtractMetadata(lf *logfile.LogFile) (*LogMetadata, error) {
	meta := &LogMetadata{
		Source:        lf.Name,
		Start:         lf.Start,
		End:           lf.End,
		DateFormat:    "iso8601-local",
		StorageEngine: "wiredTiger",
	}

	if !lf.Start.IsZero() && lf.Start.Location() == time.UTC {
		meta.DateFormat = "iso8601-utc"
	}

	seenVersions := make(map[string]bool)

	// Scan lines
	for {
		ev, err := lf.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		meta.Length++

		if meta.Start.IsZero() && !ev.DateTime.IsZero() {
			meta.Start = ev.DateTime
		}
		if !ev.DateTime.IsZero() {
			meta.End = ev.DateTime
		}

		attr := ev.Attr()

		// Host and Port extraction
		if meta.Host == "" {
			if h, ok := attr["host"].(string); ok && h != "" {
				meta.Host = h
			} else if ev.Msg == "Listening on 127.0.0.1:27017" || strings.HasPrefix(ev.Msg, "Listening on") {
				parts := strings.Fields(ev.Msg)
				if len(parts) >= 3 {
					hp := parts[2]
					if idx := strings.LastIndex(hp, ":"); idx != -1 {
						meta.Host = hp[:idx]
						meta.Port = hp[idx+1:]
					}
				}
			}
		}

		if meta.Port == "" {
			if p, ok := attr["port"].(float64); ok && p > 0 {
				meta.Port = strconv.Itoa(int(p))
			} else if p, ok := attr["port"].(int); ok && p > 0 {
				meta.Port = strconv.Itoa(p)
			}
		}

		// Options set by command line
		if ev.Msg == "Options set by command line" || ev.ID == 22315 || ev.ID == 51233 {
			if optsMap, ok := attr["options"].(map[string]interface{}); ok {
				if netMap, ok := optsMap["net"].(map[string]interface{}); ok {
					if meta.Port == "" {
						if p, ok := netMap["port"].(float64); ok {
							meta.Port = strconv.Itoa(int(p))
						} else if p, ok := netMap["port"].(int); ok {
							meta.Port = strconv.Itoa(p)
						}
					}
				}
				if shardingMap, ok := optsMap["sharding"].(map[string]interface{}); ok {
					if cr, ok := shardingMap["clusterRole"].(string); ok {
						meta.ClusterRole = cr
					}
				}
				if storageMap, ok := optsMap["storage"].(map[string]interface{}); ok {
					if eng, ok := storageMap["engine"].(string); ok {
						meta.StorageEngine = eng
					}
				}
				if replMap, ok := optsMap["replication"].(map[string]interface{}); ok {
					if rs, ok := replMap["replSetName"].(string); ok {
						meta.ReplSet = rs
					}
				}
			}
		}

		// Binary & version detection
		if ev.Msg == "Build Info" || ev.ID == 23403 || ev.Msg == "MongoDB starting" || ev.ID == 20001 || ev.ID == 21951 {
			if ev.Thread == "mongosMain" || strings.Contains(ev.Thread, "mongos") {
				meta.Binary = "mongos"
			} else if meta.Binary == "" {
				meta.Binary = "mongod"
			}

			ver := ""
			if buildInfo, ok := attr["buildInfo"].(map[string]interface{}); ok {
				if v, ok := buildInfo["version"].(string); ok {
					ver = v
				}
			} else if v, ok := attr["version"].(string); ok {
				ver = v
			}

			if ver != "" {
				if !seenVersions[ver] {
					seenVersions[ver] = true
					meta.Versions = append(meta.Versions, ver)
				}
				meta.Restarts = append(meta.Restarts, RestartInfo{
					Time:    ev.DateTime,
					Version: ver,
				})
			}
		}

		// Replica set config detection
		if strings.Contains(ev.Msg, "replica set config") || strings.Contains(ev.Msg, "Replica set config") || ev.ID == 21333 || ev.ID == 21334 {
			if cfgMap, ok := attr["config"].(map[string]interface{}); ok {
				if id, ok := cfgMap["_id"].(string); ok {
					meta.ReplSet = id
				}
				if v, ok := cfgMap["version"].(float64); ok {
					meta.ReplSetVersion = strconv.Itoa(int(v))
				}
				if pv, ok := cfgMap["protocolVersion"].(float64); ok {
					meta.ReplSetProtocol = strconv.Itoa(int(pv))
				}
				if members, ok := cfgMap["members"].([]interface{}); ok {
					var memberStrs []string
					for _, m := range members {
						if mMap, ok := m.(map[string]interface{}); ok {
							host, _ := mMap["host"].(string)
							idVal := mMap["_id"]
							memberStrs = append(memberStrs, fmt.Sprintf("{ _id: %v, host: %q }", idVal, host))
						}
					}
					meta.ReplSetMembers = "[ " + strings.Join(memberStrs, ", ") + " ]"
				}
			}
		}

		// Shards & CSRS info
		if meta.ClusterRole == "" && (ev.Msg == "Sharded cluster" || strings.Contains(ev.Msg, "sharding")) {
			meta.ClusterRole = "shard"
		}
	}

	if meta.Binary == "" {
		meta.Binary = "mongod"
	}
	if meta.Port == "" {
		if meta.ClusterRole == "shardsvr" {
			meta.Port = "27018"
		} else if meta.ClusterRole == "configsvr" {
			meta.Port = "27019"
		} else {
			meta.Port = "27017"
		}
	}
	if meta.Host == "" {
		meta.Host = "localhost"
	}

	return meta, nil
}

// PrintMetadata displays the default summary block for a log file.
func PrintMetadata(meta *LogMetadata) {
	printMetadataTo(os.Stdout, meta, ui.Style{})
}

// printMetadataTo renders the metadata block with the given style. Labels
// are right-aligned to the widest label so the layout holds for any style.
func printMetadataTo(w io.Writer, meta *LogMetadata, style ui.Style) {
	unknown := style.Dim("unknown")
	versionStr := unknown
	if len(meta.Versions) > 0 {
		versionStr = style.Bold(strings.Join(meta.Versions, " -> "))
	}

	rows := [][2]string{
		{"source:", style.Bold(meta.Source)},
		{"host:", fmt.Sprintf("%s:%s", meta.Host, meta.Port)},
	}
	if meta.ReplSet != "" {
		rows = append(rows, [2]string{"replSet:", meta.ReplSet})
	}
	rows = append(rows,
		[2]string{"start:", unknown},
		[2]string{"end:", unknown},
		[2]string{"date format:", meta.DateFormat},
		[2]string{"length:", strconv.Itoa(meta.Length)},
		[2]string{"binary:", style.Bold(meta.Binary)},
	)
	if meta.ClusterRole != "" {
		rows = append(rows, [2]string{"clusterRole:", meta.ClusterRole})
	}
	rows = append(rows,
		[2]string{"version:", versionStr},
		[2]string{"storage:", meta.StorageEngine},
	)

	if !meta.Start.IsZero() {
		rows[3][1] = meta.Start.Format("2006 Jan 02 15:04:05.000")
	}
	if !meta.End.IsZero() {
		rows[4][1] = meta.End.Format("2006 Jan 02 15:04:05.000")
	}

	width := 0
	for _, r := range rows {
		if len(r[0]) > width {
			width = len(r[0])
		}
	}
	for _, r := range rows {
		fmt.Fprintf(w, "%s %s\n", style.Cyan(fmt.Sprintf("%*s", width, r[0])), r[1])
	}
}

// printSectionHeader prints a blank line, then a styled section title with
// a dashed rule underneath.
func printSectionHeader(style ui.Style, title string) {
	fmt.Printf("\n%s\n", style.SectionHeader(title))
}

// RunAnalysis analyzes a single log file and executes any requested sections.
func RunAnalysis(lf *logfile.LogFile, opts *Options) error {
	meta, err := ExtractMetadata(lf)
	if err != nil {
		return fmt.Errorf("failed to extract metadata for %s: %w", lf.Name, err)
	}

	style := opts.Color

	// Always print default metadata
	printMetadataTo(os.Stdout, meta, style)

	// Check if any sections are requested
	hasSections := opts.Queries || opts.Distinct || opts.Connections || opts.ConnStats ||
		opts.Restarts || opts.RsState || opts.RsInfo || opts.Transactions ||
		opts.Cursors || opts.StorageStats || opts.Sharding || opts.Clients

	if !hasSections {
		return nil
	}

	// Run requested sections
	if opts.Queries {
		if err := lf.Rewind(); err != nil {
			return err
		}
		printSectionHeader(style, "QUERIES")
		RunQueries(lf, opts)
	}

	if opts.Distinct {
		if err := lf.Rewind(); err != nil {
			return err
		}
		printSectionHeader(style, "DISTINCT")
		RunDistinct(lf, opts)
	}

	if opts.Connections || opts.ConnStats {
		if err := lf.Rewind(); err != nil {
			return err
		}
		printSectionHeader(style, "CONNECTIONS")
		RunConnections(lf, opts)
	}

	if opts.Restarts {
		printSectionHeader(style, "RESTARTS")
		RunRestarts(meta, opts)
	}

	if opts.RsState {
		if err := lf.Rewind(); err != nil {
			return err
		}
		printSectionHeader(style, "RSSTATE")
		RunRsState(lf, opts)
	}

	if opts.RsInfo {
		printSectionHeader(style, "RSINFO")
		RunRsInfo(meta, opts)
	}

	if opts.Transactions {
		if err := lf.Rewind(); err != nil {
			return err
		}
		printSectionHeader(style, "TRANSACTIONS")
		RunTransactions(lf, opts)
	}

	if opts.Cursors {
		if err := lf.Rewind(); err != nil {
			return err
		}
		printSectionHeader(style, "CURSORS")
		RunCursors(lf, opts)
	}

	if opts.StorageStats {
		if err := lf.Rewind(); err != nil {
			return err
		}
		printSectionHeader(style, "STORAGE STATISTICS")
		RunStorageStats(lf, opts)
	}

	if opts.Sharding {
		if err := lf.Rewind(); err != nil {
			return err
		}
		printSectionHeader(style, "SHARDING")
		RunSharding(meta, lf, opts)
	}

	if opts.Clients {
		if err := lf.Rewind(); err != nil {
			return err
		}
		printSectionHeader(style, "CLIENTS")
		RunClients(lf, opts)
	}

	return nil
}

// Basename returns base filename for display.
func Basename(path string) string {
	return filepath.Base(path)
}
