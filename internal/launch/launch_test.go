package launch

import (
	"os"
	"syscall"
	"testing"
)

func TestStateSaveAndLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "launch_state_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if StateExists(tmpDir) {
		t.Errorf("expected state to not exist yet")
	}

	state := &StartupState{
		ProtocolVersion: ProtocolVersion,
		MtoolsVersion:   "0.1.0",
		ParsedArgs: map[string]any{
			"dir":  tmpDir,
			"port": float64(27017),
		},
		StartupInfo: map[string]string{
			"27017": "mongod --dbpath ./db --port 27017",
		},
	}

	if err := SaveState(tmpDir, state); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	if !StateExists(tmpDir) {
		t.Errorf("expected state to exist now")
	}

	loaded, err := LoadState(tmpDir)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	if loaded.ProtocolVersion != ProtocolVersion {
		t.Errorf("expected protocol version %d, got %d", ProtocolVersion, loaded.ProtocolVersion)
	}
	if loaded.StartupInfo["27017"] != "mongod --dbpath ./db --port 27017" {
		t.Errorf("unexpected startup info: %v", loaded.StartupInfo)
	}
}

func TestBuildReplSetDoc(t *testing.T) {
	// 9-node replica set with arbiter and priority
	ports := []int{27017, 27018, 27019, 27020, 27021, 27022, 27023, 27024, 27025}
	arbPort := 27026
	doc := BuildReplSetDoc("testrs", "localhost", ports, &arbPort, true)

	if doc["_id"] != "testrs" {
		t.Errorf("expected _id testrs, got %v", doc["_id"])
	}

	members, ok := doc["members"].([]map[string]any)
	if !ok {
		// Or bson.M slice
		if ms, ok2 := doc["members"].([]struct{}); ok2 {
			_ = ms
		}
	}
	_ = members
}

func TestResolveShardNames(t *testing.T) {
	// Numeric count: "2" -> ["shard01", "shard02"]
	res1 := resolveShardNames([]string{"2"})
	if len(res1) != 2 || res1[0] != "shard01" || res1[1] != "shard02" {
		t.Errorf("unexpected numeric shard names: %v", res1)
	}

	// Named shards: ["alpha", "beta"]
	res2 := resolveShardNames([]string{"alpha", "beta"})
	if len(res2) != 2 || res2[0] != "alpha" || res2[1] != "beta" {
		t.Errorf("unexpected custom shard names: %v", res2)
	}
}

func TestTagFiltering(t *testing.T) {
	nodes := []NodeInfo{
		{Port: 27017, Status: "running", ProcessType: "mongos", Role: "router", Tags: []string{"all", "running", "mongos", "router", "27017"}},
		{Port: 27018, Status: "running", ProcessType: "config", Role: "primary", Tags: []string{"all", "running", "config", "configRepl", "primary", "27018"}},
		{Port: 27019, Status: "down", ProcessType: "mongod", Role: "secondary", ShardName: "shard01", Tags: []string{"all", "down", "mongod", "shard01", "secondary", "27019"}},
	}

	// Filter by "running"
	running := FilterNodesByTags(nodes, []string{"running"})
	if len(running) != 2 {
		t.Errorf("expected 2 running nodes, got %d", len(running))
	}

	// Filter by "mongos"
	mongos := FilterNodesByTags(nodes, []string{"mongos"})
	if len(mongos) != 1 || mongos[0].Port != 27017 {
		t.Errorf("expected 1 mongos node, got %v", mongos)
	}

	// Filter by "shard01" and "down"
	shardDown := FilterNodesByTags(nodes, []string{"shard01", "down"})
	if len(shardDown) != 1 || shardDown[0].Port != 27019 {
		t.Errorf("expected 1 shard01 down node, got %v", shardDown)
	}
}

func TestParseSignal(t *testing.T) {
	if parseSignal("9") != syscall.SIGKILL {
		t.Errorf("expected SIGKILL for 9")
	}
	if parseSignal("SIGKILL") != syscall.SIGKILL {
		t.Errorf("expected SIGKILL for SIGKILL")
	}
	if parseSignal("15") != syscall.SIGTERM {
		t.Errorf("expected SIGTERM for 15")
	}
	if parseSignal("SIGTERM") != syscall.SIGTERM {
		t.Errorf("expected SIGTERM for SIGTERM")
	}
}

func TestParseStartupCmd(t *testing.T) {
	cmd1 := "mongod --dbpath ./data/db --logpath ./data/mongod.log --port 27017 --fork"
	n1 := parseStartupCmd(27017, cmd1)
	if n1.ProcessType != "mongod" || n1.DBPath != "./data/db" || n1.LogPath != "./data/mongod.log" {
		t.Errorf("unexpected parsed cmd1: %+v", n1)
	}

	cmd2 := "mongos --configdb configRepl/localhost:27018 --port 27017 --logpath ./data/mongos.log --fork"
	n2 := parseStartupCmd(27017, cmd2)
	if n2.ProcessType != "mongos" || n2.Role != "router" {
		t.Errorf("unexpected parsed cmd2: %+v", n2)
	}
}
