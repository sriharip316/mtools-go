package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/sriharip316/mtools-go/internal/logfile"
	"github.com/sriharip316/mtools-go/internal/loginfo"
	"github.com/sriharip316/mtools-go/internal/ui"
)

// NewLoginfoCmd creates the loginfo command.
func NewLoginfoCmd() *cobra.Command {
	var (
		queries      bool
		distinct     bool
		distinctMin  int
		connections  bool
		connStats    bool
		restarts     bool
		rsState      bool
		rsInfo       bool
		transactions bool
		tSort        string
		cursors      bool
		storageStats bool
		sharding     bool
		errorsFlag   bool
		migrations   bool
		clients      bool
		sortField    string
		rounding     int
		verbose      bool
		debug        bool
		colorFlag    string
		noColorFlag  bool
	)

	cmd := &cobra.Command{
		Use:   "loginfo [logfile...] [flags]",
		Short: "Extract general information and statistics from MongoDB log files",
		Long: `loginfo parses MongoDB LogV2 log files and extracts general summary information
(such as time bounds, host, binary, versions, storage engine, replica set name)
as well as optional deep-dive statistics sections like queries, distinct messages,
connections, restarts, transactions, cursors, sharding, and client metadata.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				_ = cmd.Usage()
				return errors.New("at least one logfile argument must be provided")
			}

			enableColor, err := ui.ResolveEnabled(colorFlag, noColorFlag)
			if err != nil {
				return err
			}

			opts := &loginfo.Options{
				Queries:      queries,
				Distinct:     distinct,
				DistinctMin:  distinctMin,
				Connections:  connections,
				ConnStats:    connStats,
				Restarts:     restarts,
				RsState:      rsState,
				RsInfo:       rsInfo,
				Transactions: transactions,
				TSort:        tSort,
				Cursors:      cursors,
				StorageStats: storageStats,
				Sharding:     sharding,
				Errors:       errorsFlag,
				Migrations:   migrations,
				Clients:      clients,
				Sort:         sortField,
				Rounding:     rounding,
				Verbose:      verbose,
				Debug:        debug,
				Color:        ui.Style{Enabled: enableColor},
			}

			style := opts.Color
			for i, path := range args {
				if i > 0 {
					fmt.Printf("\n %s\n\n", style.Dim("------------------------------------------"))
				}

				lf, err := logfile.Open(path)
				if err != nil {
					return fmt.Errorf("failed to open %s: %w", path, err)
				}

				analysisErr := loginfo.RunAnalysis(lf, opts)
				closeErr := lf.Close()
				if analysisErr != nil {
					return analysisErr
				}
				if closeErr != nil {
					return closeErr
				}
			}

			return nil
		},
	}

	// Register section flags
	cmd.Flags().BoolVar(&queries, "queries", false, "outputs statistics about query patterns")
	cmd.Flags().StringVar(&sortField, "sort", "sum", "sort queries by: namespace, pattern, count, min, max, mean, 95%, sum")
	cmd.Flags().IntVar(&rounding, "rounding", 1, "number of decimal places for rounding of calculated stats (0-4)")

	cmd.Flags().BoolVar(&distinct, "distinct", false, "outputs distinct list of all log lines by message")
	cmd.Flags().IntVar(&distinctMin, "distinctmin", 5, "minimum number of occurrences to include in --distinct")

	cmd.Flags().BoolVar(&connections, "connections", false, "outputs information about opened and closed connections")
	cmd.Flags().BoolVar(&connStats, "connstats", false, "outputs statistics for connection duration (min/max/avg)")

	cmd.Flags().BoolVar(&restarts, "restarts", false, "outputs information about every detected restart")
	cmd.Flags().BoolVar(&rsState, "rsstate", false, "outputs information about replica set state changes")
	cmd.Flags().BoolVar(&rsInfo, "rsinfo", false, "outputs replica set config information")

	cmd.Flags().BoolVar(&transactions, "transactions", false, "outputs statistics about transactions")
	cmd.Flags().StringVar(&tSort, "tsort", "", "sort transactions by: duration")

	cmd.Flags().BoolVar(&cursors, "cursors", false, "outputs statistics about reaped cursors")
	cmd.Flags().BoolVar(&storageStats, "storagestats", false, "outputs storage statistics for operations")

	cmd.Flags().BoolVar(&sharding, "sharding", false, "outputs sharding related information")
	cmd.Flags().BoolVar(&errorsFlag, "errors", false, "toggle output for sharding related errors/warnings")
	cmd.Flags().BoolVar(&migrations, "migrations", false, "toggle output for shard chunk migrations/splits")

	cmd.Flags().BoolVar(&clients, "clients", false, "outputs client driver and authentication information")

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show more verbose output")
	cmd.Flags().BoolVar(&debug, "debug", false, "show debug output")

	cmd.Flags().StringVar(&colorFlag, "color", "auto", "colorize output: auto, always, never")
	cmd.Flags().Lookup("color").NoOptDefVal = "always"
	cmd.Flags().BoolVar(&noColorFlag, "no-color", false, "disable colorized output")

	return cmd
}
