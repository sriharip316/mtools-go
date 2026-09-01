// Package ui provides shared terminal styling helpers: ANSI codes, TTY
// detection, color-mode resolution, and a Style type for wrapping text.
package ui

import "os"

// ANSI escape codes for terminal styling.
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Grey    = "\033[37m"
	Blue    = "\033[1;34m"
	Cyan    = "\033[36m"
	Magenta = "\033[35m"
)

// IsTerminal checks if the given file handle is an interactive terminal.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}
