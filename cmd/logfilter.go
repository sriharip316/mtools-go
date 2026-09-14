package cmd

import (
	"container/heap"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/sriharip316/mtools-go/internal/filter"
	"github.com/sriharip316/mtools-go/internal/logevent"
	"github.com/sriharip316/mtools-go/internal/logfile"
	"github.com/sriharip316/mtools-go/internal/ui"
)

type mergeItem struct {
	ev       *logevent.LogEvent
	fileIdx  int
	marker   string
	tzOffset int
}

type mergeHeap []mergeItem

func (h mergeHeap) Len() int           { return len(h) }
func (h mergeHeap) Less(i, j int) bool { return h[i].ev.DateTime.Before(h[j].ev.DateTime) }
func (h mergeHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *mergeHeap) Push(x any) {
	*h = append(*h, x.(mergeItem))
}
func (h *mergeHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// NormalizeFlags rewrites optional-value flags like `--slow 500` to `--slow=500`
// so pflag parses the integer argument rather than treating it as a positional argument.
func NormalizeFlags(args []string) []string {
	var out []string
	optionalFlags := map[string]bool{
		"--slow":    true,
		"--fast":    true,
		"--shorten": true,
	}
	reInt := regexp.MustCompile(`^-?\d+$`)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if optionalFlags[arg] {
			if i+1 < len(args) && reInt.MatchString(args[i+1]) {
				out = append(out, arg+"="+args[i+1])
				i++
				continue
			}
		}
		if arg == "--color" {
			if i+1 < len(args) && (args[i+1] == "auto" || args[i+1] == "always" || args[i+1] == "never") {
				out = append(out, arg+"="+args[i+1])
				i++
				continue
			}
		}
		out = append(out, arg)
	}
	return out
}

// NewLogfilterCmd creates the logfilter cobra command.
func NewLogfilterCmd() *cobra.Command {
	var (
		verbose      bool
		shortenVal   int
		exclude      bool
		human        bool
		jsonOut      bool
		pretty       bool
		markersFlag  []string
		timezoneFlag []int
		fromStr      string
		toStr        string
		slowMs       int
		fastMs       int
		scan         bool
		word         []string
		component    []string
		level        []string
		namespace    []string
		operation    []string
		thread       []string
		patternStr   string
		command      []string
		planSummary  []string
		transactions bool
		maskPath     string
		maskSize     int
		maskCenter   string
		colorFlag    string
		noColorFlag  bool
	)

	cmd := &cobra.Command{
		Use:   "logfilter [logfile...] [flags]",
		Short: "Filter MongoDB 6.0+ LogV2 log files",
		Long: `mongod/mongos log file parser for MongoDB 6.0+ LogV2 JSON logs.
Use parameters to enable filters. A line only gets printed if it passes
all enabled filters. If several log files are provided, their lines are
merged by timestamp.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Open log files or stdin
			var logfiles []*logfile.LogFile
			isStdin := false

			if len(args) == 0 {
				isStdin = true
				logfiles = append(logfiles, logfile.NewFromReader(os.Stdin, "stdin"))
			} else {
				for _, path := range args {
					lf, err := logfile.Open(path)
					if err != nil {
						return fmt.Errorf("failed to open logfile %s: %w", path, err)
					}
					defer lf.Close()
					logfiles = append(logfiles, lf)
				}
			}

			// 2. Setup timezone adjustments
			timezones := make([]int, len(logfiles))
			if len(timezoneFlag) == 1 {
				for i := range timezones {
					timezones[i] = timezoneFlag[0]
				}
			} else if len(timezoneFlag) == len(logfiles) {
				copy(timezones, timezoneFlag)
			} else if len(timezoneFlag) > 1 {
				return fmt.Errorf("invalid number of timezone parameters: expected 1 or %d, got %d", len(logfiles), len(timezoneFlag))
			}

			// 3. Setup markers
			markers := make([]string, len(logfiles))
			if len(logfiles) > 1 {
				markerMode := "filename"
				if len(markersFlag) == 1 {
					markerMode = markersFlag[0]
				}

				if len(markersFlag) == len(logfiles) {
					copy(markers, markersFlag)
				} else {
					switch markerMode {
					case "none":
						// empty strings
					case "enum":
						for i := range markers {
							markers[i] = fmt.Sprintf("{%d}", i+1)
						}
					case "alpha":
						for i := range markers {
							markers[i] = fmt.Sprintf("{%c}", 'a'+i)
						}
					case "filename":
						for i := range markers {
							markers[i] = fmt.Sprintf("{%s}", logfiles[i].Name)
						}
					default:
						if len(markersFlag) > 0 {
							return fmt.Errorf("number of markers (%d) does not match number of files (%d)", len(markersFlag), len(logfiles))
						}
						for i := range markers {
							markers[i] = fmt.Sprintf("{%s}", logfiles[i].Name)
						}
					}
				}
			}

			// 4. Calculate global bounds for DateTimeFilter
			var globalStart, globalEnd time.Time
			if !isStdin {
				for i, lf := range logfiles {
					if !lf.Start.IsZero() {
						st := lf.Start.Add(time.Duration(timezones[i]) * time.Hour)
						if globalStart.IsZero() || st.Before(globalStart) {
							globalStart = st
						}
					}
					if !lf.End.IsZero() {
						et := lf.End.Add(time.Duration(timezones[i]) * time.Hour)
						if globalEnd.IsZero() || et.After(globalEnd) {
							globalEnd = et
						}
					}
				}
			}

			// 5. Build active filters
			var activeFilters []filter.Filter

			if fromStr != "start" || toStr != "end" {
				dtFilter, err := filter.NewDateTimeFilter(fromStr, toStr, globalStart, globalEnd, isStdin)
				if err != nil {
					return fmt.Errorf("invalid --from / --to: %w", err)
				}
				activeFilters = append(activeFilters, dtFilter)
			}

			if maskPath != "" {
				mf, err := filter.NewMaskFilter(maskPath, maskSize, maskCenter)
				if err != nil {
					return fmt.Errorf("invalid --mask: %w", err)
				}
				activeFilters = append(activeFilters, mf)
			}

			if cmd.Flags().Changed("slow") {
				activeFilters = append(activeFilters, &filter.SlowFilter{ThresholdMs: slowMs})
			}
			if cmd.Flags().Changed("fast") {
				activeFilters = append(activeFilters, &filter.FastFilter{ThresholdMs: fastMs})
			}
			if scan {
				activeFilters = append(activeFilters, &filter.TableScanFilter{})
			}
			if len(word) > 0 {
				wf, err := filter.NewWordFilter(word)
				if err != nil {
					return fmt.Errorf("invalid --word regex: %w", err)
				}
				activeFilters = append(activeFilters, wf)
			}
			if transactions {
				activeFilters = append(activeFilters, &filter.TransactionFilter{})
			}

			llFilter, err := filter.NewLogLineFilter(
				component, level, namespace, operation, thread, command, patternStr, planSummary,
			)
			if err != nil {
				return fmt.Errorf("invalid logline filter parameter: %w", err)
			}
			if llFilter.Active() {
				activeFilters = append(activeFilters, llFilter)
			}

			if verbose {
				fmt.Println("command line arguments:")
				fmt.Printf("    from: %s, to: %s\n", fromStr, toStr)
				fmt.Printf("    active filters: %d\n", len(activeFilters))
				fmt.Println("====================")
			}

			// 6. Fast-forward logfiles if not in exclude mode
			if !exclude {
				for _, f := range activeFilters {
					if sl, ok := f.(filter.StartLimiter); ok {
						limit := sl.StartLimit()
						if !limit.IsZero() {
							for i, lf := range logfiles {
								// Adjust limit by inverse timezone for that file
								fileLimit := limit.Add(-time.Duration(timezones[i]) * time.Hour)
								lf.FastForward(fileLimit)
							}
						}
					}
				}
			}

			// 7. Output shortening setting
			var shortenPtr *int
			if cmd.Flags().Changed("shorten") {
				shortenPtr = &shortenVal
			}

			// Color resolution
			enableColor, err := ui.ResolveEnabled(colorFlag, noColorFlag)
			if err != nil {
				return err
			}

			// 8. Stream events (single file or k-way merge)
			if len(logfiles) == 1 {
				lf := logfiles[0]
				tz := timezones[0]
				marker := markers[0]

				for {
					ev, err := lf.Next()
					if err != nil {
						if err == io.EOF {
							break
						}
						return err
					}

					if tz != 0 && !ev.DateTime.IsZero() {
						ev.DateTime = ev.DateTime.Add(time.Duration(tz) * time.Hour)
					}

					if shouldOutput(ev, activeFilters, exclude) {
						outputEvent(ev, marker, shortenPtr, human, jsonOut, pretty, activeFilters, enableColor)
					}

					if checkEarlyExit(activeFilters) {
						break
					}
				}
			} else {
				// K-way merge using min-heap
				h := &mergeHeap{}
				heap.Init(h)

				for i, lf := range logfiles {
					ev, err := lf.Next()
					if err == nil && ev != nil {
						if timezones[i] != 0 && !ev.DateTime.IsZero() {
							ev.DateTime = ev.DateTime.Add(time.Duration(timezones[i]) * time.Hour)
						}
						heap.Push(h, mergeItem{
							ev:       ev,
							fileIdx:  i,
							marker:   markers[i],
							tzOffset: timezones[i],
						})
					}
				}

				for h.Len() > 0 {
					item := heap.Pop(h).(mergeItem)
					ev := item.ev

					if shouldOutput(ev, activeFilters, exclude) {
						outputEvent(ev, item.marker, shortenPtr, human, jsonOut, pretty, activeFilters, enableColor)
					}

					if checkEarlyExit(activeFilters) {
						break
					}

					// Read next from the same file
					nextEv, err := logfiles[item.fileIdx].Next()
					if err == nil && nextEv != nil {
						if item.tzOffset != 0 && !nextEv.DateTime.IsZero() {
							nextEv.DateTime = nextEv.DateTime.Add(time.Duration(item.tzOffset) * time.Hour)
						}
						heap.Push(h, mergeItem{
							ev:       nextEv,
							fileIdx:  item.fileIdx,
							marker:   item.marker,
							tzOffset: item.tzOffset,
						})
					}
				}
			}

			return nil
		},
	}

	// Register flags
	cmd.Flags().BoolVar(&verbose, "verbose", false, "outputs information about the parser and arguments")
	cmd.Flags().IntVar(&shortenVal, "shorten", 200, "shortens long lines by cutting characters out of the middle")
	cmd.Flags().Lookup("shorten").NoOptDefVal = "200"
	cmd.Flags().BoolVar(&exclude, "exclude", false, "if set, excludes matching lines rather than includes them")
	cmd.Flags().BoolVar(&human, "human", false, "outputs large numbers with commas and durations formatted as hr,min,sec,ms")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "outputs all matching lines in JSON format")
	cmd.Flags().BoolVar(&pretty, "pretty", false, "print JSON output with multi-line indentation")
	cmd.Flags().StringSliceVar(&markersFlag, "markers", []string{"filename"}, "markers when merging several files (none, enum, alpha, filename, or list)")
	cmd.Flags().IntSliceVarP(&timezoneFlag, "timezone", "z", []int{0}, "timezone adjustments (hours) per file or global")

	cmd.Flags().StringVar(&fromStr, "from", "start", "output starting at FROM")
	cmd.Flags().StringVar(&toStr, "to", "end", "output up to TO")
	cmd.Flags().StringVar(&maskPath, "mask", "", "source log file to create the filter mask")
	cmd.Flags().IntVar(&maskSize, "mask-size", 60, "mask size in seconds around each filter point (default: 60)")
	cmd.Flags().StringVar(&maskCenter, "mask-center", "end", "mask center point for events with duration: start, end, both (default: end)")
	cmd.Flags().IntVar(&slowMs, "slow", 1000, "only output lines with query times longer than SLOW ms")
	cmd.Flags().Lookup("slow").NoOptDefVal = "1000"
	cmd.Flags().IntVar(&fastMs, "fast", 1000, "only output lines with query times shorter than FAST ms")
	cmd.Flags().Lookup("fast").NoOptDefVal = "1000"
	cmd.Flags().BoolVar(&scan, "scan", false, "only output lines with poor index usage")
	cmd.Flags().StringSliceVar(&word, "word", nil, "only output lines matching any of WORD regexes")
	cmd.Flags().StringSliceVar(&component, "component", nil, "only output log lines with component CM")
	cmd.Flags().StringSliceVar(&level, "level", nil, "only output log lines with loglevel LL")
	cmd.Flags().StringSliceVar(&namespace, "namespace", nil, "only output log lines on namespace NS")
	cmd.Flags().StringSliceVarP(&operation, "operation", "o", nil, "only output log lines of type OP")
	cmd.Flags().StringSliceVar(&thread, "thread", nil, "only output log lines of thread THREAD")
	cmd.Flags().StringVar(&patternStr, "pattern", "", "only output log lines with a query pattern PATTERN")
	cmd.Flags().StringSliceVar(&command, "command", nil, "only output log lines which are commands of given type")
	cmd.Flags().StringSliceVar(&planSummary, "planSummary", nil, "only output log lines matching plan summary values")
	cmd.Flags().BoolVar(&transactions, "transactions", false, "only output lines containing logs of transactions")
	cmd.Flags().StringVar(&colorFlag, "color", "auto", "colorize output: auto, always, never")
	cmd.Flags().Lookup("color").NoOptDefVal = "always"
	cmd.Flags().BoolVar(&noColorFlag, "no-color", false, "disable colorized output")

	return cmd
}

func shouldOutput(ev *logevent.LogEvent, activeFilters []filter.Filter, exclude bool) bool {
	if len(activeFilters) == 0 {
		return !exclude
	}

	allAgree := true
	for _, f := range activeFilters {
		if !f.Accept(ev) {
			allAgree = false
			break
		}
	}

	if exclude {
		return !allAgree
	}
	return allAgree
}

func checkEarlyExit(activeFilters []filter.Filter) bool {
	for _, f := range activeFilters {
		if f.SkipRemaining() {
			return true
		}
	}
	return false
}

func outputEvent(ev *logevent.LogEvent, marker string, shorten *int, human, jsonOut, pretty bool, activeFilters []filter.Filter, enableColor bool) {
	line := ev.LineStr

	if jsonOut || pretty {
		line = ev.ToJSON(pretty)
	}

	if marker != "" {
		line = marker + " " + line
	}

	if shorten != nil && *shorten > 4 && len(line) > *shorten {
		half := *shorten / 2
		line = line[:half-2] + "..." + line[len(line)-half+1:]
	}

	if human {
		if ev.Duration != nil && *ev.Duration >= 1000 {
			line = line + " (" + formatMs(*ev.Duration) + ")"
		}
		line = formatCommasInCounters(line)
	}

	if enableColor {
		baseColor := filter.GetBaseColor(ev.Level)
		var spans []filter.Span
		for _, f := range activeFilters {
			if h, ok := f.(filter.Highlighter); ok {
				spans = append(spans, h.HighlightSpans(line, ev)...)
			}
		}
		line = filter.Colorize(line, baseColor, spans)
	}

	fmt.Println(line)
}

func formatMs(ms int) string {
	hr := ms / 3600000
	ms %= 3600000
	min := ms / 60000
	ms %= 60000
	sec := ms / 1000
	mill := ms % 1000
	return fmt.Sprintf("%dhr %dmin %dsecs %dms", hr, min, sec, mill)
}

var reCounterNumbers = regexp.MustCompile(`("?(?:keysExamined|docsExamined|nreturned|nReturned|nMatched|nModified|nInserted|nDeleted|numYields|durationMillis|cursorid|bytesRead|bytesWritten)"?\s*:\s*)(\d{4,})`)

func formatCommasInCounters(line string) string {
	return reCounterNumbers.ReplaceAllStringFunc(line, func(s string) string {
		sub := reCounterNumbers.FindStringSubmatch(s)
		if len(sub) == 3 {
			n, err := strconv.ParseInt(sub[2], 10, 64)
			if err == nil {
				return sub[1] + commaFormat(n)
			}
		}
		return s
	})
}

func commaFormat(n int64) string {
	in := strconv.FormatInt(n, 10)
	var out strings.Builder
	l := len(in)
	for i, c := range in {
		if i > 0 && (l-i)%3 == 0 {
			out.WriteRune(',')
		}
		out.WriteRune(c)
	}
	return out.String()
}
