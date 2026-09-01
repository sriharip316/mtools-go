package ui

import (
	"fmt"
	"os"
	"strings"
)

// ResolveEnabled determines whether color output should be enabled based on
// the --color flag value and the --no-color flag. The NO_COLOR environment
// variable always forces color off. mode accepts auto, always, never (plus
// common boolean spellings); auto enables color only on a TTY.
func ResolveEnabled(mode string, noColor bool) (bool, error) {
	if noColor || os.Getenv("NO_COLOR") != "" {
		return false, nil
	}
	switch strings.ToLower(mode) {
	case "always", "true", "1", "yes":
		return true, nil
	case "never", "false", "0", "no":
		return false, nil
	case "auto":
		return IsTerminal(os.Stdout), nil
	default:
		return false, fmt.Errorf("invalid --color value %q: expected 'auto', 'always', or 'never'", mode)
	}
}
