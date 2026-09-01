package loginfo

import (
	"fmt"
	"strings"

	"github.com/sriharip316/mtools-go/internal/ui"
)

// cellStyle returns the color code for a cell value, or "" for plain.
// Matching is done on the plain value so alignment math is unaffected.
func cellStyle(val string) string {
	switch {
	case val == "":
		return ui.Dim
	case strings.Contains(val, "COLLSCAN"):
		return ui.Yellow
	default:
		return ""
	}
}

// PrintTable formats rows as a columnar table matching Python mtools print_table:
// - Columns separated by 4 spaces
// - Rightmost column not padded
// - Optional uppercase header row followed by an empty line
//
// Column widths are computed from plain text; color wraps the already
// padded cells so ANSI codes never break alignment.
func PrintTable(rows []map[string]string, headers []string, uppercaseHeaders bool, style ui.Style) {
	if len(rows) == 0 || len(headers) == 0 {
		return
	}

	// Determine header display strings
	displayHeaders := make([]string, len(headers))
	for i, h := range headers {
		if uppercaseHeaders {
			displayHeaders[i] = strings.ToUpper(h)
		} else {
			displayHeaders[i] = h
		}
	}

	// Calculate maximum width for each column
	widths := make([]int, len(headers))
	for i, h := range displayHeaders {
		widths[i] = len(h)
	}

	for _, row := range rows {
		for i, h := range headers {
			val := row[h]
			if len(val) > widths[i] {
				widths[i] = len(val)
			}
		}
	}

	// Print Header
	var hdrParts []string
	for i, h := range displayHeaders {
		if i == len(headers)-1 {
			hdrParts = append(hdrParts, style.Bold(h))
		} else {
			hdrParts = append(hdrParts, style.Bold(fmt.Sprintf("%-*s", widths[i], h)))
		}
	}
	fmt.Println(strings.Join(hdrParts, "    "))
	fmt.Println()

	// Print Data Rows
	for _, row := range rows {
		var rowParts []string
		for i, h := range headers {
			val := row[h]
			code := cellStyle(val)
			if val == "" {
				val = "None"
			}
			cell := val
			if i != len(headers)-1 {
				cell = fmt.Sprintf("%-*s", widths[i], val)
			}
			rowParts = append(rowParts, style.Wrap(code, cell))
		}
		fmt.Println(strings.Join(rowParts, "    "))
	}
}
