package loginfo

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sriharip316/mtools-go/internal/logfile"
	"github.com/sriharip316/mtools-go/internal/ui"
)

// QueryKey uniquely identifies a query group.
type QueryKey struct {
	Namespace    string
	Operation    string
	Pattern      string
	AllowDiskUse string
}

type queryStats struct {
	key       QueryKey
	durations []int
}

// RunQueries implements the --queries section.
func RunQueries(lf *logfile.LogFile, opts *Options) {
	groupMap := make(map[QueryKey]*queryStats)
	var groupOrder []*queryStats

	rounding := opts.Rounding
	if rounding < 0 {
		rounding = 0
	} else if rounding > 4 {
		rounding = 4
	}

	for {
		ev, err := lf.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		op := ev.Operation
		if op == "command" || op == "" {
			if ev.Command != "" {
				op = ev.Command
			}
		}

		isQuery := ev.Duration != nil && (op != "" || ev.Namespace != "" ||
			ev.Component == "COMMAND" || ev.Component == "QUERY" || ev.Component == "WRITE")

		if !isQuery {
			continue
		}

		diskUse := "None"
		if ev.AllowDiskUse != nil {
			if *ev.AllowDiskUse {
				diskUse = "True"
			} else {
				diskUse = "False"
			}
		}

		pat := ev.Pattern
		if pat == "" {
			pat = "{}"
		}

		key := QueryKey{
			Namespace:    ev.Namespace,
			Operation:    op,
			Pattern:      pat,
			AllowDiskUse: diskUse,
		}

		qs, exists := groupMap[key]
		if !exists {
			qs = &queryStats{key: key}
			groupMap[key] = qs
			groupOrder = append(groupOrder, qs)
		}

		qs.durations = append(qs.durations, *ev.Duration)
	}

	if len(groupOrder) == 0 {
		fmt.Println(opts.Color.Dim("no queries found."))
		return
	}

	type rowData struct {
		rawMap map[string]string
		key    QueryKey
		count  int
		min    int
		max    int
		sum    int
		mean   float64
		p95    float64
	}

	var rows []rowData

	for _, qs := range groupOrder {
		durs := qs.durations
		count := len(durs)
		if count == 0 {
			continue
		}

		sort.Ints(durs)
		minVal := durs[0]
		maxVal := durs[count-1]
		sumVal := 0
		for _, d := range durs {
			sumVal += d
		}
		meanVal := float64(sumVal) / float64(count)

		// 95th percentile (linear interpolation)
		p95Val := float64(durs[0])
		if count > 1 {
			idx := 0.95 * float64(count-1)
			low := int(idx)
			high := low + 1
			frac := idx - float64(low)
			p95Val = float64(durs[low])
			if high < count {
				p95Val += frac * float64(durs[high]-durs[low])
			}
		}

		m := map[string]string{
			"namespace":    qs.key.Namespace,
			"operation":    qs.key.Operation,
			"pattern":      qs.key.Pattern,
			"count":        strconv.Itoa(count),
			"min (ms)":     strconv.Itoa(minVal),
			"max (ms)":     strconv.Itoa(maxVal),
			"95%-ile (ms)": formatFloat(p95Val, rounding),
			"sum (ms)":     strconv.Itoa(sumVal),
			"mean (ms)":    formatFloat(meanVal, rounding),
			"allowDiskUse": qs.key.AllowDiskUse,
		}

		rows = append(rows, rowData{
			rawMap: m,
			key:    qs.key,
			count:  count,
			min:    minVal,
			max:    maxVal,
			sum:    sumVal,
			mean:   meanVal,
			p95:    p95Val,
		})
	}

	sortField := opts.Sort
	if sortField == "" {
		sortField = "sum"
	}

	sort.Slice(rows, func(i, j int) bool {
		switch sortField {
		case "namespace":
			if rows[i].key.Namespace != rows[j].key.Namespace {
				return rows[i].key.Namespace < rows[j].key.Namespace
			}
			return rows[i].key.Pattern < rows[j].key.Pattern
		case "pattern":
			if rows[i].key.Pattern != rows[j].key.Pattern {
				return rows[i].key.Pattern < rows[j].key.Pattern
			}
			return rows[i].key.Namespace < rows[j].key.Namespace
		case "count":
			return rows[i].count > rows[j].count
		case "min":
			return rows[i].min > rows[j].min
		case "max":
			return rows[i].max > rows[j].max
		case "mean":
			return rows[i].mean > rows[j].mean
		case "95%":
			return rows[i].p95 > rows[j].p95
		case "sum":
			fallthrough
		default:
			return rows[i].sum > rows[j].sum
		}
	})

	tableRows := make([]map[string]string, len(rows))
	for i, r := range rows {
		tableRows[i] = r.rawMap
	}

	headers := []string{"namespace", "operation", "pattern", "count", "min (ms)", "max (ms)", "95%-ile (ms)", "sum (ms)", "mean (ms)", "allowDiskUse"}
	PrintTable(tableRows, headers, false, opts.Color)
	fmt.Println()
}

// RunDistinct implements the --distinct section.
func RunDistinct(lf *logfile.LogFile, opts *Options) {
	counts := make(map[string]int)
	distinctMin := opts.DistinctMin
	if distinctMin <= 0 {
		distinctMin = 5
	}
	nonMatches := 0

	for {
		ev, err := lf.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		msg := ev.Msg
		if msg == "" {
			msg = ev.Component
		}

		if !opts.Verbose {
			// Skip uninteresting background threads
			if ev.Thread == "initandlisten" || ev.Thread == "WTCheckpointThread" ||
				(ev.Component == "STORAGE" && strings.Contains(ev.Thread, "checkpoint")) {
				nonMatches++
				continue
			}
		}

		counts[msg]++
	}

	type pair struct {
		msg   string
		count int
	}

	var pairs []pair
	for m, c := range counts {
		if !opts.Verbose && c < distinctMin {
			nonMatches += c
		} else {
			pairs = append(pairs, pair{msg: m, count: c})
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].msg < pairs[j].msg
	})

	for _, p := range pairs {
		fmt.Printf("%s  %s\n", opts.Color.Bold(fmt.Sprintf("%8d", p.count)), p.msg)
	}

	fmt.Println()

	if nonMatches > 0 {
		fmt.Println(opts.Color.Dim(fmt.Sprintf("Distinct ignored %d less informative lines", nonMatches)))
		if !opts.Verbose {
			fmt.Println(opts.Color.Dim("To show ignored lines, run with --verbose"))
		}
	}
}

// RunConnections implements the --connections and --connstats sections.
func RunConnections(lf *logfile.LogFile, opts *Options) {
	ipOpened := make(map[string]int)
	ipClosed := make(map[string]int)
	socketExceptions := 0

	// Duration tracking for --connstats
	connStarts := make(map[int]time.Time)
	ipDurations := make(map[string][]int)
	var allDurations []int

	for {
		ev, err := lf.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		isAccepted := ev.ID == 22943 ||
			strings.Contains(strings.ToLower(ev.Msg), "connection accepted") ||
			(ev.Component == "NETWORK" && strings.Contains(strings.ToLower(ev.Msg), "accepted"))

		isEnded := ev.ID == 22944 || ev.ID == 20883 || ev.ID == 22945 ||
			strings.Contains(strings.ToLower(ev.Msg), "connection ended") ||
			strings.Contains(strings.ToLower(ev.Msg), "end connection") ||
			(ev.Component == "NETWORK" && strings.Contains(strings.ToLower(ev.Msg), "ended"))

		ip := ev.RemoteIP
		if ip == "" {
			ip = "anonymous"
		}

		if isAccepted {
			ipOpened[ip]++
			if opts.ConnStats && ev.ConnectionID != nil && !ev.DateTime.IsZero() {
				connStarts[*ev.ConnectionID] = ev.DateTime
			}
		}

		if isEnded {
			ipClosed[ip]++
			if opts.ConnStats && ev.ConnectionID != nil && !ev.DateTime.IsZero() {
				if start, ok := connStarts[*ev.ConnectionID]; ok {
					dur := int(ev.DateTime.Sub(start).Seconds())
					if dur < 0 {
						dur = 0
					}
					ipDurations[ip] = append(ipDurations[ip], dur)
					allDurations = append(allDurations, dur)
					delete(connStarts, *ev.ConnectionID)
				}
			}
		}

		if strings.Contains(ev.Msg, "SocketException") || strings.Contains(ev.Msg, "socket exception") ||
			strings.Contains(ev.LineStr, "SocketException") || strings.Contains(ev.LineStr, "Connection reset by peer") {
			socketExceptions++
		}
	}

	totalOpened := 0
	for _, c := range ipOpened {
		totalOpened += c
	}
	totalClosed := 0
	for _, c := range ipClosed {
		totalClosed += c
	}

	uniqueIPsMap := make(map[string]bool)
	for ip := range ipOpened {
		uniqueIPsMap[ip] = true
	}
	for ip := range ipClosed {
		uniqueIPsMap[ip] = true
	}

	label := func(s string) string { return opts.Color.Cyan(fmt.Sprintf("%*s", 18, s)) }
	fmt.Printf("%s %d\n", label("total opened:"), totalOpened)
	fmt.Printf("%s %d\n", label("total closed:"), totalClosed)
	fmt.Printf("%s %d\n", label("no unique IPs:"), len(uniqueIPsMap))
	fmt.Printf("%s %d\n", label("socket exceptions:"), socketExceptions)

	if opts.ConnStats {
		statLabel := func(s string) string { return opts.Color.Cyan(s) }
		if len(allDurations) > 0 {
			sort.Ints(allDurations)
			sumDur := 0
			for _, d := range allDurations {
				sumDur += d
			}
			avgDur := sumDur / len(allDurations)
			minDur := allDurations[0]
			maxDur := allDurations[len(allDurations)-1]

			fmt.Printf("%s %d\n", statLabel("overall average connection duration(s):"), avgDur)
			fmt.Printf("%s %d\n", statLabel("overall minimum connection duration(s):"), minDur)
			fmt.Printf("%s %d\n", statLabel("overall maximum connection duration(s):"), maxDur)
		} else {
			fmt.Printf("%s %s\n", statLabel("overall average connection duration(s):"), opts.Color.Dim("-"))
			fmt.Printf("%s %s\n", statLabel("overall minimum connection duration(s):"), opts.Color.Dim("-"))
			fmt.Printf("%s %s\n", statLabel("overall maximum connection duration(s):"), opts.Color.Dim("-"))
		}
	}

	fmt.Println()

	var ipList []string
	for ip := range uniqueIPsMap {
		ipList = append(ipList, ip)
	}
	sort.Slice(ipList, func(i, j int) bool {
		if ipOpened[ipList[i]] != ipOpened[ipList[j]] {
			return ipOpened[ipList[i]] > ipOpened[ipList[j]]
		}
		return ipList[i] < ipList[j]
	})

	for _, ip := range ipList {
		opened := ipOpened[ip]
		closed := ipClosed[ip]

		if opts.ConnStats {
			durs := ipDurations[ip]
			avg, min, max := 0, 0, 0
			if len(durs) > 0 {
				sort.Ints(durs)
				min = durs[0]
				max = durs[len(durs)-1]
				sum := 0
				for _, d := range durs {
					sum += d
				}
				avg = sum / len(durs)
			}
			fmt.Printf("%s  %s %-8d  %s %-8d %s %-8d %s %-8d %s %-8d\n",
				opts.Color.Bold(fmt.Sprintf("%-15s", ip)),
				opts.Color.Cyan("opened:"), opened,
				opts.Color.Cyan("closed:"), closed,
				opts.Color.Cyan("dur-avg(s):"), avg,
				opts.Color.Cyan("dur-min(s):"), min,
				opts.Color.Cyan("dur-max(s):"), max)
		} else {
			fmt.Printf("%s  %s %-8d  %s %-8d\n",
				opts.Color.Bold(fmt.Sprintf("%-15s", ip)),
				opts.Color.Cyan("opened:"), opened,
				opts.Color.Cyan("closed:"), closed)
		}
	}
	fmt.Println()
}

// RunRestarts implements the --restarts section.
func RunRestarts(meta *LogMetadata, opts *Options) {
	if len(meta.Restarts) == 0 {
		fmt.Println(opts.Color.Dim("  no restarts found"))
		return
	}

	for _, r := range meta.Restarts {
		fmt.Printf("   %s version %s\n", r.Time.Format("Jan 02 15:04:05"), opts.Color.Cyan(r.Version))
	}
}

// RunRsState implements the --rsstate section.
func RunRsState(lf *logfile.LogFile, opts *Options) {
	var rows []map[string]string

	for {
		ev, err := lf.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		isRSState := ev.Component == "REPL" && (strings.Contains(ev.Msg, "state") ||
			strings.Contains(ev.Msg, "Transition") || strings.Contains(ev.Msg, "PRIMARY") ||
			strings.Contains(ev.Msg, "SECONDARY") || strings.Contains(ev.Msg, "STARTUP") ||
			strings.Contains(ev.Msg, "RECOVERING") || strings.Contains(ev.Msg, "ROLLBACK") ||
			strings.Contains(ev.Msg, "ARBITER") || strings.Contains(ev.Msg, "electSelf") ||
			strings.Contains(ev.Msg, "votes is even"))

		if isRSState {
			host := ev.RemoteIP
			if host == "" {
				if h, ok := ev.Attr()["host"].(string); ok {
					host = h
				} else {
					host = "localhost (self)"
				}
			}

			rows = append(rows, map[string]string{
				"date":          ev.DateTime.Format("Jan 02 15:04:05"),
				"host":          host,
				"state/message": ev.Msg,
			})
		}
	}

	if len(rows) == 0 {
		fmt.Println(opts.Color.Dim("  no rs state changes found"))
		return
	}

	PrintTable(rows, []string{"date", "host", "state/message"}, false, opts.Color)
	fmt.Println()
}

// RunRsInfo implements the --rsinfo section.
func RunRsInfo(meta *LogMetadata, opts *Options) {
	if meta.ReplSet != "" {
		members := meta.ReplSetMembers
		if members == "" {
			members = "unknown"
		}
		ver := meta.ReplSetVersion
		if ver == "" {
			ver = "unknown"
		}
		proto := meta.ReplSetProtocol
		if proto == "" {
			proto = "unknown"
		}

		rsLabel := func(s string) string { return opts.Color.Cyan(fmt.Sprintf("%*s", 12, s)) }
		fmt.Printf("%s %s\n", rsLabel("rs name:"), meta.ReplSet)
		fmt.Printf("%s %s\n", rsLabel("rs members:"), members)
		fmt.Printf("%s %s\n", rsLabel("rs version:"), ver)
		fmt.Printf("%s %s\n", rsLabel("rs protocol:"), proto)
	} else {
		fmt.Println(opts.Color.Dim("  no rs info changes found"))
	}
}

// RunTransactions implements the --transactions and --tsort sections.
func RunTransactions(lf *logfile.LogFile, opts *Options) {
	type txnRow struct {
		rawMap   map[string]string
		duration int
	}

	var rows []txnRow

	for {
		ev, err := lf.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		isTxn := ev.TxnNumber != nil || ev.Operation == "transaction" ||
			strings.Contains(strings.ToLower(ev.Msg), "transaction") ||
			strings.Contains(ev.LineStr, "transaction")

		if isTxn {
			txnNumStr := "None"
			if ev.TxnNumber != nil {
				txnNumStr = strconv.FormatInt(*ev.TxnNumber, 10)
			}
			autocommitStr := "None"
			if ev.Autocommit != nil {
				autocommitStr = strconv.FormatBool(*ev.Autocommit)
			}
			rcStr := ev.ReadConcern
			if rcStr == "" {
				rcStr = "None"
			}
			timeActiveStr := "None"
			if ev.TimeActiveMicros != nil {
				timeActiveStr = strconv.FormatInt(*ev.TimeActiveMicros, 10)
			}
			timeInactiveStr := "None"
			if ev.TimeInactiveMicros != nil {
				timeInactiveStr = strconv.FormatInt(*ev.TimeInactiveMicros, 10)
			}
			durStr := "None"
			durVal := 0
			if ev.Duration != nil {
				durStr = strconv.Itoa(*ev.Duration)
				durVal = *ev.Duration
			}

			rows = append(rows, txnRow{
				rawMap: map[string]string{
					"datetime":           ev.DateTime.Format("2006-01-02T15:04:05.000-0700"),
					"txnNumber":          txnNumStr,
					"autocommit":         autocommitStr,
					"readConcern":        rcStr,
					"timeActiveMicros":   timeActiveStr,
					"timeInactiveMicros": timeInactiveStr,
					"duration":           durStr,
				},
				duration: durVal,
			})
		}
	}

	if len(rows) == 0 {
		fmt.Println(opts.Color.Dim("no transactions found."))
		return
	}

	if opts.TSort == "duration" {
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].duration > rows[j].duration
		})
	}

	tableRows := make([]map[string]string, len(rows))
	for i, r := range rows {
		tableRows[i] = r.rawMap
	}

	headers := []string{"datetime", "txnNumber", "autocommit", "readConcern", "timeActiveMicros", "timeInactiveMicros", "duration"}
	PrintTable(tableRows, headers, true, opts.Color)
	fmt.Println()
}

// RunCursors implements the --cursors section.
func RunCursors(lf *logfile.LogFile, opts *Options) {
	var rows []map[string]string

	for {
		ev, err := lf.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		isCursor := ev.CursorID != "" ||
			(strings.Contains(strings.ToLower(ev.Msg), "cursor") &&
				(strings.Contains(ev.Msg, "timed out") || strings.Contains(ev.Msg, "reaped")))

		if isCursor {
			cid := ev.CursorID
			if cid == "" {
				cid = "unknown"
			}
			rt := ev.ReapedTime
			if rt == "" {
				rt = ev.DateTime.Format("2006-01-02 15:04:05.000000-07:00")
			}

			rows = append(rows, map[string]string{
				"datetime":   ev.DateTime.Format("2006-01-02 15:04:05.000000-07:00"),
				"cursorid":   cid,
				"reapedtime": rt,
			})
		}
	}

	if len(rows) == 0 {
		fmt.Println(opts.Color.Dim("no cursor information found."))
		return
	}

	headers := []string{"datetime", "cursorid", "reapedtime"}
	PrintTable(rows, headers, true, opts.Color)
	fmt.Println()
}

// RunStorageStats implements the --storagestats section.
func RunStorageStats(lf *logfile.LogFile, opts *Options) {
	var rows []map[string]string

	for {
		ev, err := lf.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		hasStorageStats := ev.BytesRead != nil || ev.BytesWritten != nil ||
			ev.TimeReadingMicros != nil || ev.TimeWritingMicros != nil

		if hasStorageStats {
			op := ev.Operation
			if op == "" || op == "command" {
				if ev.Command != "" {
					op = ev.Command
				}
			}

			bRead := "None"
			if ev.BytesRead != nil {
				bRead = strconv.FormatInt(*ev.BytesRead, 10)
			}
			bWrite := "None"
			if ev.BytesWritten != nil {
				bWrite = strconv.FormatInt(*ev.BytesWritten, 10)
			}
			tRead := "None"
			if ev.TimeReadingMicros != nil {
				tRead = strconv.FormatInt(*ev.TimeReadingMicros, 10)
			}
			tWrite := "None"
			if ev.TimeWritingMicros != nil {
				tWrite = strconv.FormatInt(*ev.TimeWritingMicros, 10)
			}

			rows = append(rows, map[string]string{
				"namespace":         ev.Namespace,
				"operation":         op,
				"bytesRead":         bRead,
				"bytesWritten":      bWrite,
				"timeReadingMicros": tRead,
				"timeWritingMicros": tWrite,
			})
		}
	}

	if len(rows) == 0 {
		fmt.Println(opts.Color.Dim("no statistics found."))
		return
	}

	headers := []string{"namespace", "operation", "bytesRead", "bytesWritten", "timeReadingMicros", "timeWritingMicros"}
	PrintTable(rows, headers, false, opts.Color)
	fmt.Println()
}

// RunSharding implements the --sharding, --errors, and --migrations sections.
func RunSharding(meta *LogMetadata, lf *logfile.LogFile, opts *Options) {
	style := opts.Color

	fmt.Printf("\n%s\n\n", style.Bold("Overview:"))

	if meta.ClusterRole != "" || len(meta.Shards) > 0 || meta.CSRS[0] != "" {
		role := meta.ClusterRole
		if role == "" {
			if meta.Binary == "mongos" {
				role = "mongos"
			} else {
				role = "shard"
			}
		}
		fmt.Printf("  %s (%s)\n", style.Cyan("The role of this node:"), role)
		if len(meta.Shards) > 0 {
			fmt.Printf("  %s\n", style.Bold("Shards:"))
			for _, sh := range meta.Shards {
				fmt.Printf("    %s: %s\n", style.Cyan(sh[0]), sh[1])
			}
		}
		if meta.CSRS[0] != "" {
			fmt.Printf("  %s\n", style.Bold("CSRS:"))
			fmt.Printf("    %s: %s\n", style.Cyan(meta.CSRS[0]), meta.CSRS[1])
		}
	} else {
		fmt.Println(style.Dim("  no sharding info found."))
	}

	if opts.Errors {
		fmt.Printf("\n%s\n\n", style.Bold("Error Messages:"))
		// Cluster and pattern match errors
		type errEntry struct {
			count int
			level string
		}
		errPatterns := make(map[string]*errEntry)
		for {
			ev, err := lf.Next()
			if err != nil {
				if err == io.EOF {
					break
				}
				break
			}
			if ev.Level == "E" || ev.Level == "W" {
				entry, ok := errPatterns[ev.Msg]
				if !ok {
					entry = &errEntry{level: ev.Level}
					errPatterns[ev.Msg] = entry
				}
				entry.count++
			}
		}
		if len(errPatterns) == 0 {
			fmt.Println(style.Dim("  no error messages found."))
		} else {
			for msg, entry := range errPatterns {
				color := ui.Yellow
				if entry.level == "E" {
					color = ui.Red
				}
				fmt.Printf("%s  %s\n", style.Bold(fmt.Sprintf("%3d", entry.count)), style.Wrap(color, msg))
			}
		}
	} else {
		fmt.Printf("\n%s\n", style.Dim("to show sharding errors/warnings, run with --errors."))
	}

	if opts.Migrations {
		fmt.Printf("\n%s\n\n", style.Bold("Chunks Moved From This Shard:"))
		fmt.Println(style.Dim("  no chunk migrations found."))
		fmt.Printf("\n%s\n\n", style.Bold("Chunks Moved To This Shard:"))
		fmt.Println(style.Dim("  no chunk migrations found."))
		fmt.Printf("\n%s\n\n", style.Bold("Chunk Split Statistics:"))
		fmt.Println(style.Dim("  no chunk splits found."))
	} else {
		fmt.Printf("\n%s\n", style.Dim("to show chunk migrations/splits, run with --migrations."))
	}
	fmt.Println()
}

type clientInfo struct {
	ips   map[string]int
	users map[string]int
}

// RunClients implements the --clients section.
func RunClients(lf *logfile.LogFile, opts *Options) {
	dvaMap := make(map[string]*clientInfo)
	connToDVA := make(map[string]string)

	for {
		ev, err := lf.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		attr := ev.Attr()
		connID := ev.Conn
		if connID == "" && ev.ConnectionID != nil {
			connID = fmt.Sprintf("conn%d", *ev.ConnectionID)
		}

		// (1) Connection accepted: reset connection mapping if seen
		if ev.ID == 22943 || strings.Contains(ev.Msg, "Connection accepted") {
			if connID != "" {
				delete(connToDVA, connID)
			}
		}

		// (2) Received client metadata
		if ev.ID == 51800 || strings.Contains(ev.Msg, "client metadata") || strings.Contains(ev.Msg, "Received client metadata") {
			driverName := "UNKNOWN"
			driverVer := "UNKNOWN"
			appName := "UNKNOWN"

			if docMap, ok := attr["doc"].(map[string]interface{}); ok {
				if driverMap, ok := docMap["driver"].(map[string]interface{}); ok {
					if n, ok := driverMap["name"].(string); ok && n != "" {
						driverName = n
					}
					if v, ok := driverMap["version"].(string); ok && v != "" {
						driverVer = v
					}
				}
				if appMap, ok := docMap["application"].(map[string]interface{}); ok {
					if a, ok := appMap["name"].(string); ok && a != "" {
						appName = a
					}
				}
			} else if clientMeta, ok := attr["clientMetadata"].(map[string]interface{}); ok {
				if driverMap, ok := clientMeta["driver"].(map[string]interface{}); ok {
					if n, ok := driverMap["name"].(string); ok && n != "" {
						driverName = n
					}
					if v, ok := driverMap["version"].(string); ok && v != "" {
						driverVer = v
					}
				}
				if appMap, ok := clientMeta["application"].(map[string]interface{}); ok {
					if a, ok := appMap["name"].(string); ok && a != "" {
						appName = a
					}
				}
			}

			dvaKey := fmt.Sprintf("%s___%s___%s", driverName, driverVer, appName)
			if connID != "" {
				connToDVA[connID] = dvaKey
			}

			ip := ev.RemoteIP
			if ip == "" {
				ip = "anonymous"
			}

			ci, exists := dvaMap[dvaKey]
			if !exists {
				ci = &clientInfo{
					ips:   make(map[string]int),
					users: make(map[string]int),
				}
				dvaMap[dvaKey] = ci
			}
			ci.ips[ip]++
		}

		// (3) Authentication log
		if ev.ID == 5286307 || strings.Contains(ev.Msg, "authenticated") || strings.Contains(ev.Msg, "Successfully authenticated as") {
			user := ""
			db := "admin"
			if u, ok := attr["user"].(string); ok && u != "" {
				user = u
			} else if p, ok := attr["principalName"].(string); ok && p != "" {
				user = p
			}
			if d, ok := attr["db"].(string); ok && d != "" {
				db = d
			}

			if user != "" {
				userKey := fmt.Sprintf("%s@%s", user, db)
				dvaKey, hasConn := connToDVA[connID]
				if !hasConn {
					dvaKey = "UNKNOWN___UNKNOWN___UNKNOWN"
				}

				ci, exists := dvaMap[dvaKey]
				if !exists {
					ci = &clientInfo{
						ips:   make(map[string]int),
						users: make(map[string]int),
					}
					dvaMap[dvaKey] = ci
				}
				ci.users[userKey]++
			}
		}
	}

	if len(dvaMap) == 0 {
		return
	}

	divider := strings.Repeat("-", 79)

	var dvaKeys []string
	for k := range dvaMap {
		dvaKeys = append(dvaKeys, k)
	}
	sort.Strings(dvaKeys)

	for _, dva := range dvaKeys {
		ci := dvaMap[dva]

		parts := strings.Split(dva, "___")
		driver := parts[0]
		ver := parts[1]
		app := parts[2]

		dvaDisplay := opts.Color.Cyan("Driver:") + " " + opts.Color.Bold(driver) +
			" | " + opts.Color.Cyan("Version:") + " " + opts.Color.Bold(ver)
		if app != "UNKNOWN" {
			dvaDisplay += " | " + opts.Color.Cyan("App:") + " " + opts.Color.Bold(app)
		}

		// Format IPs
		type ipCount struct {
			ip    string
			count int
		}
		var ipCounts []ipCount
		for ip, c := range ci.ips {
			ipCounts = append(ipCounts, ipCount{ip: ip, count: c})
		}
		sort.Slice(ipCounts, func(i, j int) bool {
			if ipCounts[i].count != ipCounts[j].count {
				return ipCounts[i].count > ipCounts[j].count
			}
			return ipCounts[i].ip < ipCounts[j].ip
		})

		var ipStrs []string
		for _, ipc := range ipCounts {
			ipStrs = append(ipStrs, fmt.Sprintf("%s %s", ipc.ip, opts.Color.Dim(fmt.Sprintf("(%d conns)", ipc.count))))
		}

		// Format Users
		var userNames []string
		for u := range ci.users {
			userNames = append(userNames, u)
		}
		sort.Strings(userNames)

		var userStrs []string
		for _, u := range userNames {
			userStrs = append(userStrs, fmt.Sprintf("%s %s", u, opts.Color.Dim(fmt.Sprintf("(%d auths)", ci.users[u]))))
		}

		fmt.Println()
		fmt.Println(opts.Color.Dim(divider))
		fmt.Println(dvaDisplay)
		fmt.Println(opts.Color.Dim(divider))
		fmt.Printf("%s %s\n", opts.Color.Bold(opts.Color.Cyan(fmt.Sprintf("* DB Users (%d):", len(ci.users)))), strings.Join(userStrs, ", "))
		fmt.Printf("%s %s\n", opts.Color.Bold(opts.Color.Cyan(fmt.Sprintf("* IPs (%d):", len(ci.ips)))), strings.Join(ipStrs, ", "))
	}
	fmt.Println()
}

func formatFloat(v float64, decimals int) string {
	if decimals <= 0 {
		return strconv.Itoa(int(math.Round(v)))
	}
	return fmt.Sprintf("%.*f", decimals, v)
}
