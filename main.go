package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/sriharip316/mtools-go/cmd"
)

var version = "0.1.0"

func main() {
	rootCmd := &cobra.Command{
		Use:     "mtools",
		Short:   "MongoDB log analysis and environment management tools",
		Version: version,
	}

	rootCmd.AddCommand(cmd.NewLogfilterCmd())
	rootCmd.AddCommand(cmd.NewLaunchCmd())
	rootCmd.AddCommand(cmd.NewLoginfoCmd())
	rootCmd.AddCommand(cmd.NewLoadCmd())

	// Normalize arguments (e.g. "--slow 500" -> "--slow=500")
	normalizedArgs := cmd.NormalizeFlags(os.Args[1:])
	rootCmd.SetArgs(normalizedArgs)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
