package cmd

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/sriharip316/mtools-go/internal/load"
)

// NewLoadCmd creates the `load` cobra command for test data simulation.
func NewLoadCmd() *cobra.Command {
	var (
		uri          string
		database     string
		schemaFile   string
		loadDurStr   string
		durationStr  string
		rateStr      string
		minRate      float64
		maxRate      float64
		intervalStr  string
		batchSize    int
		workers      int
		drop         bool
		dryRun       bool
		jsonOut      bool
	)

	cmd := &cobra.Command{
		Use:   "load [flags]",
		Short: "MongoDB synthetic test data generator and CRUD workload simulator",
		Long: `load generates and inserts realistic synthetic test data into MongoDB.
It operates in two phases:
  1. Initial Load: Pre-populates target collections with documents generated
     from a JSON schema (default 2 collections: users and orders) for --load-duration (default 60s).
  2. CRUD Simulation: Continuously executes a weighted mix of Create, Read
     (including cache/lookup misses), Update, and Delete operations against
     the active working set at a varying rate (--rate min-max), running indefinitely
     by default or for a specified --duration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse rate bounds
			finalMinRate, finalMaxRate, err := parseRateBounds(rateStr, minRate, maxRate)
			if err != nil {
				return err
			}

			// Parse load-duration
			loadDuration, err := parseFlexibleDuration(loadDurStr)
			if err != nil {
				return fmt.Errorf("invalid --load-duration: %w", err)
			}

			// Parse simulation duration
			duration, err := parseFlexibleDuration(durationStr)
			if err != nil {
				return fmt.Errorf("invalid --duration: %w", err)
			}

			// Parse stats reporting interval
			interval, err := parseFlexibleDuration(intervalStr)
			if err != nil {
				return fmt.Errorf("invalid --interval: %w", err)
			}
			if interval <= 0 {
				interval = 1 * time.Second
			}

			opts := load.SimulationOptions{
				URI:          uri,
				Database:     database,
				SchemaFile:   schemaFile,
				LoadDuration: loadDuration,
				Duration:     duration,
				MinRate:      finalMinRate,
				MaxRate:      finalMaxRate,
				Interval:     interval,
				BatchSize:    batchSize,
				Workers:      workers,
				Drop:         drop,
				DryRun:       dryRun,
				JSON:         jsonOut,
			}

			return load.Run(opts)
		},
	}

	cmd.Flags().StringVarP(&uri, "uri", "u", "mongodb://localhost:27017", "MongoDB connection URI")
	cmd.Flags().StringVar(&database, "database", "test", "target database name")
	cmd.Flags().StringVar(&database, "db", "test", "alias for --database")
	_ = cmd.Flags().MarkHidden("db")

	cmd.Flags().StringVarP(&schemaFile, "schema", "s", "", "path to external JSON schema file (defaults to baked-in 2-collection schema)")
	cmd.Flags().StringVar(&loadDurStr, "load-duration", "60s", "duration for initial load phase (e.g. 60s, 2m; set to 0 to skip)")
	cmd.Flags().StringVarP(&durationStr, "duration", "d", "0", "duration for CRUD simulation phase (e.g. 30s, 5m, 1h; 0 = run forever)")
	cmd.Flags().StringVar(&rateStr, "rate", "10-50", "CRUD simulation rate in ops/sec (e.g. '10-50', '100', or '20:80')")
	cmd.Flags().Float64Var(&minRate, "min-rate", 0, "minimum CRUD operations per second")
	cmd.Flags().Float64Var(&maxRate, "max-rate", 0, "maximum CRUD operations per second")

	cmd.Flags().StringVarP(&intervalStr, "interval", "i", "1s", "statistics reporting interval (e.g. 1s, 5s)")
	cmd.Flags().IntVarP(&batchSize, "batch-size", "b", 1, "batch size per insertMany in initial load phase")
	cmd.Flags().IntVarP(&workers, "workers", "w", 4, "number of concurrent worker goroutines")
	cmd.Flags().BoolVar(&drop, "drop", false, "drop target collections before starting initial load")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "generate and preview sample documents and CRUD operations without connecting to MongoDB")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output live statistics in JSON format")

	return cmd
}

var rateRangeRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(?:-|:|\.\.)\s*(\d+(?:\.\d+)?)$`)

func parseRateBounds(rateStr string, explicitMin, explicitMax float64) (float64, float64, error) {
	if explicitMin > 0 || explicitMax > 0 {
		minR := explicitMin
		maxR := explicitMax
		if minR <= 0 {
			minR = maxR
		}
		if maxR <= 0 {
			maxR = minR
		}
		if maxR < minR {
			maxR = minR
		}
		return minR, maxR, nil
	}

	rateStr = strings.TrimSpace(rateStr)
	if rateStr == "" {
		return 10, 50, nil
	}

	// Check range format (e.g. "10-50", "10:50", "10..50")
	if matches := rateRangeRe.FindStringSubmatch(rateStr); len(matches) == 3 {
		minVal, err1 := strconv.ParseFloat(matches[1], 64)
		maxVal, err2 := strconv.ParseFloat(matches[2], 64)
		if err1 == nil && err2 == nil {
			if maxVal < minVal {
				maxVal = minVal
			}
			return minVal, maxVal, nil
		}
	}

	// Check single fixed rate (e.g. "100")
	fixedVal, err := strconv.ParseFloat(rateStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid rate format %q: expected single value or range (e.g. '100' or '10-50')", rateStr)
	}

	return fixedVal, fixedVal, nil
}

func parseFlexibleDuration(durStr string) (time.Duration, error) {
	durStr = strings.TrimSpace(durStr)
	if durStr == "" || durStr == "0" || durStr == "0s" {
		return 0, nil
	}

	// If purely numeric without units, default to seconds
	if num, err := strconv.Atoi(durStr); err == nil {
		return time.Duration(num) * time.Second, nil
	}

	return time.ParseDuration(durStr)
}
