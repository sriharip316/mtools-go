package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func executeLaunchCommand(args ...string) (string, error) {
	cmd := NewLaunchCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	normalizedArgs := NormalizeFlags(args)
	cmd.SetArgs(normalizedArgs)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	defer func() { _ = r.Close() }()
	os.Stdout = w

	err := cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout

	var outBuf bytes.Buffer
	_, _ = outBuf.ReadFrom(r)

	return outBuf.String(), err
}

func getMongoDBBinaryPath(t *testing.T) string {
	t.Helper()

	// 1. Check if mongod is available under PATH
	if p, err := exec.LookPath("mongod"); err == nil {
		return filepath.Dir(p)
	}

	// 2. Check .local/mongodb/bin in project dir
	candidates := []string{
		".local/mongodb/bin",
		"../.local/mongodb/bin",
	}

	for _, c := range candidates {
		absPath, err := filepath.Abs(c)
		if err == nil {
			if _, err := os.Stat(filepath.Join(absPath, "mongod")); err == nil {
				return absPath
			}
		}
	}

	t.Skip("mongod binary not found in PATH or .local/mongodb/bin, skipping live test")
	return ""
}

func TestLaunch_SingleLifecycle(t *testing.T) {
	binPath := getMongoDBBinaryPath(t)
	tmpDir := t.TempDir()

	port := "27200"

	// 1. Init single instance
	out, err := executeLaunchCommand("init", "--single", "--dir", tmpDir, "--port", port, "--binarypath", binPath)
	if err != nil {
		t.Fatalf("init failed: %v\nOutput: %s", err, out)
	}

	// 2. List instances
	outList, err := executeLaunchCommand("list", "--dir", tmpDir)
	if err != nil {
		t.Fatalf("list failed: %v\nOutput: %s", err, outList)
	}
	if !strings.Contains(outList, "running") || !strings.Contains(outList, port) {
		t.Errorf("expected running node on port %s in list output:\n%s", port, outList)
	}

	// 3. Stop instance
	outStop, err := executeLaunchCommand("stop", "--dir", tmpDir)
	if err != nil {
		t.Fatalf("stop failed: %v\nOutput: %s", err, outStop)
	}

	// 4. List after stop -> down
	outListDown, err := executeLaunchCommand("list", "--dir", tmpDir)
	if err != nil {
		t.Fatalf("list after stop failed: %v\nOutput: %s", err, outListDown)
	}
	if !strings.Contains(outListDown, "down") {
		t.Errorf("expected down node in list output:\n%s", outListDown)
	}

	// 5. Start instance
	outStart, err := executeLaunchCommand("start", "--dir", tmpDir, "--binarypath", binPath)
	if err != nil {
		t.Fatalf("start failed: %v\nOutput: %s", err, outStart)
	}

	// 6. Kill instance with signal 9
	outKill, err := executeLaunchCommand("kill", "--dir", tmpDir, "--signal", "9")
	if err != nil {
		t.Fatalf("kill failed: %v\nOutput: %s", err, outKill)
	}
}

func TestLaunch_ReplicaSetLifecycle(t *testing.T) {
	binPath := getMongoDBBinaryPath(t)
	tmpDir := t.TempDir()

	port := "27210"

	// 1. Init 3-node replica set
	out, err := executeLaunchCommand("init", "--replicaset", "--nodes", "3", "--name", "mytestrs", "--dir", tmpDir, "--port", port, "--binarypath", binPath)
	if err != nil {
		t.Fatalf("replica set init failed: %v\nOutput: %s", err, out)
	}

	// 2. List instances
	outList, err := executeLaunchCommand("list", "--dir", tmpDir, "--tags")
	if err != nil {
		t.Fatalf("list failed: %v\nOutput: %s", err, outList)
	}
	if !strings.Contains(outList, "mytestrs") || !strings.Contains(outList, "primary") {
		t.Errorf("expected primary and replica set in list output:\n%s", outList)
	}

	// 3. Stop all
	_, _ = executeLaunchCommand("kill", "--dir", tmpDir, "--signal", "9")
}

func TestLaunch_ShardedClusterLifecycle(t *testing.T) {
	binPath := getMongoDBBinaryPath(t)
	tmpDir := t.TempDir()

	port := "27230"

	// Init 2-shard cluster (each shard 1 node, 1 config server, 1 mongos)
	out, err := executeLaunchCommand("init", "--sharded", "2", "--nodes", "1", "--config", "1", "--mongos", "1", "--dir", tmpDir, "--port", port, "--binarypath", binPath)
	if err != nil {
		t.Fatalf("sharded cluster init failed: %v\nOutput: %s", err, out)
	}

	// List instances
	outList, err := executeLaunchCommand("list", "--dir", tmpDir, "--tags")
	if err != nil {
		t.Fatalf("list failed: %v\nOutput: %s", err, outList)
	}
	if !strings.Contains(outList, "mongos") || !strings.Contains(outList, "shard01") || !strings.Contains(outList, "shard02") {
		t.Errorf("expected mongos and shards in list output:\n%s", outList)
	}

	// Clean up
	_, _ = executeLaunchCommand("kill", "--dir", tmpDir, "--signal", "9")
}
