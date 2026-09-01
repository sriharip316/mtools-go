package launch

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

// PrintJSON formats the list of nodes as indented JSON.
func PrintJSON(nodes []NodeInfo) error {
	data, err := json.MarshalIndent(nodes, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// PrintTable formats the list of nodes into a clean ASCII table.
func PrintTable(nodes []NodeInfo, showTags, showStartup, verbose bool) {
	if len(nodes) == 0 {
		fmt.Println("No nodes found.")
		return
	}

	// Sort nodes by port
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Port < nodes[j].Port
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)

	headers := []string{"PROCESS", "PORT", "STATUS", "PID"}
	if showTags || verbose {
		headers = append(headers, "TAGS")
	}
	if showStartup || verbose {
		headers = append(headers, "STARTUP COMMAND")
	}

	fmt.Fprintln(w, strings.Join(headers, "\t"))

	for _, n := range nodes {
		procName := n.ProcessType
		if n.ShardName != "" {
			procName = n.ShardName
		} else if n.ReplSetName != "" {
			procName = n.ReplSetName
		}

		pidStr := "-"
		if n.PID > 0 {
			pidStr = strconv.Itoa(n.PID)
		}

		row := []string{
			procName,
			strconv.Itoa(n.Port),
			n.Status,
			pidStr,
		}

		if showTags || verbose {
			sort.Strings(n.Tags)
			row = append(row, strings.Join(n.Tags, ", "))
		}
		if showStartup || verbose {
			row = append(row, n.StartupCmd)
		}

		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	w.Flush()
}
