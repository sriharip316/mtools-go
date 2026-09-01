package launch

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// PingHost sends a MongoDB ping command to the host:port.
func PingHost(host string, port int, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	uri := fmt.Sprintf("mongodb://%s:%d/?directConnection=true&serverSelectionTimeoutMS=%d", host, port, timeout.Milliseconds())
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return false
	}
	defer client.Disconnect(ctx)

	var result bson.M
	err = client.Database("admin").RunCommand(ctx, bson.D{{Key: "ping", Value: 1}}).Decode(&result)
	return err == nil
}

// IsRunning checks if a MongoDB instance is responding on host:port.
func IsRunning(host string, port int) bool {
	// First fast TCP probe
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()

	// Then verify MongoDB ping
	return PingHost(host, port, 1*time.Second)
}

// WaitForHosts polls the given ports concurrently until all reach the expected state.
func WaitForHosts(host string, ports []int, timeout time.Duration, toStart bool) error {
	start := time.Now()
	var wg sync.WaitGroup
	errCh := make(chan int, len(ports))

	for _, port := range ports {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			for {
				running := IsRunning(host, p)
				if (toStart && running) || (!toStart && !running) {
					return
				}
				if time.Since(start) > timeout {
					errCh <- p
					return
				}
				time.Sleep(500 * time.Millisecond)
			}
		}(port)
	}

	wg.Wait()
	close(errCh)

	var failedPorts []int
	for p := range errCh {
		failedPorts = append(failedPorts, p)
	}

	if len(failedPorts) > 0 {
		action := "start"
		if !toStart {
			action = "stop"
		}
		return fmt.Errorf("timeout (%v) waiting for ports to %s: %v", timeout, action, failedPorts)
	}

	return nil
}

// DiscoverNodes inspects the StartupState and resolves the runtime status of all nodes.
func DiscoverNodes(state *StartupState, defaultHost string) []NodeInfo {
	if defaultHost == "" {
		defaultHost = "localhost"
	}

	pidsByPort := FindRunningProcesses()

	var nodes []NodeInfo

	for portStr, cmdStr := range state.StartupInfo {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			continue
		}

		info := parseStartupCmd(port, cmdStr)

		// Check runtime status
		if IsRunning(defaultHost, port) {
			info.Status = "running"
			if pid, ok := pidsByPort[port]; ok {
				info.PID = pid
			}
			// Resolve replica set role if applicable
			resolveNodeRole(defaultHost, &info)
		} else {
			info.Status = "down"
			info.PID = 0
		}

		// Generate tags
		info.Tags = generateTags(&info)
		nodes = append(nodes, info)
	}

	return nodes
}

func parseStartupCmd(port int, cmdStr string) NodeInfo {
	info := NodeInfo{
		Port:       port,
		StartupCmd: cmdStr,
	}

	parts := strings.Fields(cmdStr)
	if len(parts) > 0 {
		bin := filepath.Base(parts[0])
		if strings.Contains(bin, "mongos") {
			info.ProcessType = "mongos"
			info.Role = "router"
		} else if strings.Contains(cmdStr, "--configsvr") {
			info.ProcessType = "config"
		} else {
			info.ProcessType = "mongod"
		}
	}

	for i := 0; i < len(parts); i++ {
		switch parts[i] {
		case "--dbpath":
			if i+1 < len(parts) {
				info.DBPath = parts[i+1]
			}
		case "--logpath":
			if i+1 < len(parts) {
				info.LogPath = parts[i+1]
			}
		case "--replSet":
			if i+1 < len(parts) {
				info.ReplSetName = parts[i+1]
			}
		case "--shardsvr":
			if info.ReplSetName != "" {
				info.ShardName = info.ReplSetName
			}
		}
	}

	// Try extracting shard name from dbpath if not yet set
	if info.ShardName == "" && strings.Contains(info.DBPath, "shard") {
		parts := strings.Split(info.DBPath, string(filepath.Separator))
		for _, part := range parts {
			if strings.HasPrefix(part, "shard") {
				info.ShardName = part
				break
			}
		}
	}

	return info
}

func resolveNodeRole(host string, info *NodeInfo) {
	if info.ProcessType == "mongos" {
		info.Role = "router"
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	uri := fmt.Sprintf("mongodb://%s:%d/?directConnection=true", host, info.Port)
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return
	}
	defer client.Disconnect(ctx)

	var hello bson.M
	if err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello); err == nil {
		if isWritablePrimary, ok := hello["isWritablePrimary"].(bool); ok && isWritablePrimary {
			info.Role = "primary"
			return
		}
		if isSecondary, ok := hello["secondary"].(bool); ok && isSecondary {
			info.Role = "secondary"
			return
		}
		if isArbiter, ok := hello["arbiterOnly"].(bool); ok && isArbiter {
			info.Role = "arbiter"
			return
		}
		if msg, ok := hello["msg"].(string); ok && msg == "isdbgrid" {
			info.Role = "router"
			return
		}
	}

	if info.ReplSetName == "" {
		info.Role = "standalone"
	}
}

func generateTags(info *NodeInfo) []string {
	tagMap := make(map[string]bool)

	tagMap["all"] = true
	tagMap[info.Status] = true
	tagMap[strconv.Itoa(info.Port)] = true

	if info.ProcessType != "" {
		tagMap[info.ProcessType] = true
	}
	if info.Role != "" {
		tagMap[info.Role] = true
	}
	if info.ReplSetName != "" {
		tagMap[info.ReplSetName] = true
	}
	if info.ShardName != "" {
		tagMap[info.ShardName] = true
	}

	var tags []string
	for t := range tagMap {
		tags = append(tags, t)
	}
	return tags
}

// FilterNodesByTags returns nodes that match any (or all) of the given tag filters.
func FilterNodesByTags(nodes []NodeInfo, tags []string) []NodeInfo {
	if len(tags) == 0 {
		return nodes
	}

	var filtered []NodeInfo
	for _, node := range nodes {
		nodeTagMap := make(map[string]bool, len(node.Tags))
		for _, t := range node.Tags {
			nodeTagMap[strings.ToLower(t)] = true
		}

		matched := true
		for _, reqTag := range tags {
			req := strings.ToLower(reqTag)
			if !nodeTagMap[req] {
				matched = false
				break
			}
		}

		if matched {
			filtered = append(filtered, node)
		}
	}

	return filtered
}

// FindRunningProcesses scans /proc to find PIDs of running mongod/mongos by port.
func FindRunningProcesses() map[int]int {
	result := make(map[int]int)

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return result
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		cmdlineBytes, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}

		cmdline := string(cmdlineBytes)
		if !strings.Contains(cmdline, "mongod") && !strings.Contains(cmdline, "mongos") {
			continue
		}

		tokens := strings.Split(cmdline, "\x00")
		for i, tok := range tokens {
			if tok == "--port" && i+1 < len(tokens) {
				if port, err := strconv.Atoi(tokens[i+1]); err == nil {
					result[port] = pid
				}
			}
		}
	}

	return result
}
