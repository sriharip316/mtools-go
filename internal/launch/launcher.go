package launch

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Init initializes a new MongoDB deployment.
func Init(opts *ClusterOptions) error {
	if opts.Dir == "" {
		opts.Dir = "./data"
	}
	if opts.Port == 0 {
		opts.Port = 27017
	}
	if opts.Hostname == "" {
		opts.Hostname = "localhost"
	}
	if opts.Name == "" {
		opts.Name = "replset"
	}
	if opts.Nodes == 0 {
		opts.Nodes = 3
	}
	if opts.ConfigNodes == 0 {
		opts.ConfigNodes = 1
	}
	if opts.MongosNodes == 0 {
		opts.MongosNodes = 1
	}

	// Validate directory
	if StateExists(opts.Dir) {
		return fmt.Errorf("a .mlaunch_startup environment already exists in %s", opts.Dir)
	}

	// Find binaries
	mongodPath, err := findBinary("mongod", opts.BinaryPath)
	if err != nil {
		return err
	}
	var mongosPath string
	if len(opts.Sharded) > 0 {
		mongosPath, err = findBinary("mongos", opts.BinaryPath)
		if err != nil {
			return err
		}
	}

	// Pre-validate that target ports are available
	if err := checkPortsAvailable(opts); err != nil {
		return err
	}

	state := &StartupState{
		ProtocolVersion: ProtocolVersion,
		MtoolsVersion:   "0.1.0",
		ParsedArgs:      make(map[string]any),
		StartupInfo:     make(map[string]string),
	}

	state.ParsedArgs["dir"] = opts.Dir
	state.ParsedArgs["port"] = opts.Port
	if opts.BinaryPath != "" {
		state.ParsedArgs["binarypath"] = opts.BinaryPath
	}

	keyfilePath := ""
	if opts.Auth {
		keyfilePath, err = GenerateKeyfile(opts.Dir)
		if err != nil {
			return err
		}
		state.ParsedArgs["auth"] = true
	}

	if opts.Single {
		if err := initSingle(opts, mongodPath, keyfilePath, state); err != nil {
			return err
		}
	} else if opts.ReplicaSet {
		if err := initReplicaSet(opts, mongodPath, keyfilePath, state); err != nil {
			return err
		}
	} else if len(opts.Sharded) > 0 {
		if err := initSharded(opts, mongodPath, mongosPath, keyfilePath, state); err != nil {
			return err
		}
	} else {
		// Default to single if nothing specified
		opts.Single = true
		if err := initSingle(opts, mongodPath, keyfilePath, state); err != nil {
			return err
		}
	}

	// Save state
	if err := SaveState(opts.Dir, state); err != nil {
		return err
	}

	// For sharded authenticated clusters, restart after initialization to enable auth
	if len(opts.Sharded) > 0 && opts.Auth {
		if opts.Verbose {
			fmt.Println("Restarting sharded cluster to enable authentication...")
		}
		if err := Restart(opts.Dir, opts.BinaryPath, nil, opts.Verbose); err != nil {
			return fmt.Errorf("failed to restart cluster with auth enabled: %w", err)
		}
	}

	return nil
}

func checkPortsAvailable(opts *ClusterOptions) error {
	var portsToCheck []int

	if opts.Single {
		portsToCheck = append(portsToCheck, opts.Port)
	} else if opts.ReplicaSet {
		for i := 0; i < opts.Nodes; i++ {
			portsToCheck = append(portsToCheck, opts.Port+i)
		}
		if opts.Arbiter {
			portsToCheck = append(portsToCheck, opts.Port+opts.Nodes)
		}
	} else if len(opts.Sharded) > 0 {
		shards := resolveShardNames(opts.Sharded)
		cur := opts.Port
		for i := 0; i < opts.MongosNodes; i++ {
			portsToCheck = append(portsToCheck, cur)
			cur++
		}
		for range shards {
			for i := 0; i < opts.Nodes; i++ {
				portsToCheck = append(portsToCheck, cur)
				cur++
			}
			if opts.Arbiter {
				portsToCheck = append(portsToCheck, cur)
				cur++
			}
		}
		for i := 0; i < opts.ConfigNodes; i++ {
			portsToCheck = append(portsToCheck, cur)
			cur++
		}
	} else {
		portsToCheck = append(portsToCheck, opts.Port)
	}

	for _, p := range portsToCheck {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			return fmt.Errorf("port %d is already in use by another process: %w", p, err)
		}
		_ = ln.Close()
	}

	return nil
}

func initSingle(opts *ClusterOptions, mongodPath, keyfilePath string, state *StartupState) error {
	dbPath := filepath.Join(opts.Dir, "db")
	logPath := filepath.Join(opts.Dir, "mongod.log")

	_ = os.MkdirAll(dbPath, 0755)

	cmdArgs := []string{
		mongodPath,
		"--dbpath", dbPath,
		"--logpath", logPath,
		"--port", strconv.Itoa(opts.Port),
		"--fork",
	}
	if opts.BindIP != "" {
		cmdArgs = append(cmdArgs, "--bind_ip", opts.BindIP)
	}
	if keyfilePath != "" {
		cmdArgs = append(cmdArgs, "--keyFile", keyfilePath)
	}
	cmdArgs = append(cmdArgs, opts.ExtraArgs...)

	cmdStr := strings.Join(cmdArgs, " ")
	state.StartupInfo[strconv.Itoa(opts.Port)] = cmdStr

	if err := runCommand(cmdArgs); err != nil {
		return fmt.Errorf("failed to start standalone mongod on port %d: %w", opts.Port, err)
	}

	if err := WaitForHosts(opts.Hostname, []int{opts.Port}, 30*time.Second, true); err != nil {
		return err
	}

	if opts.Auth {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = CreateAdminUser(ctx, opts.Hostname, opts.Port, opts.Username, opts.Password, opts.AuthDB, opts.AuthRoles)
	}

	return nil
}

func initReplicaSet(opts *ClusterOptions, mongodPath, keyfilePath string, state *StartupState) error {
	var ports []int
	var launchedPorts []int

	for i := 0; i < opts.Nodes; i++ {
		p := opts.Port + i
		ports = append(ports, p)
		launchedPorts = append(launchedPorts, p)

		subPath := fmt.Sprintf("%s/rs%d", opts.Name, i+1)
		dbPath := filepath.Join(opts.Dir, subPath, "db")
		logPath := filepath.Join(opts.Dir, subPath, "mongod.log")
		_ = os.MkdirAll(dbPath, 0755)

		cmdArgs := []string{
			mongodPath,
			"--dbpath", dbPath,
			"--logpath", logPath,
			"--port", strconv.Itoa(p),
			"--replSet", opts.Name,
			"--fork",
		}
		if opts.BindIP != "" {
			cmdArgs = append(cmdArgs, "--bind_ip", opts.BindIP)
		}
		if keyfilePath != "" {
			cmdArgs = append(cmdArgs, "--keyFile", keyfilePath)
		}
		cmdArgs = append(cmdArgs, opts.ExtraArgs...)

		state.StartupInfo[strconv.Itoa(p)] = strings.Join(cmdArgs, " ")
		if err := runCommand(cmdArgs); err != nil {
			return fmt.Errorf("failed to start replica set node on port %d: %w", p, err)
		}
	}

	var arbiterPort *int
	if opts.Arbiter {
		ap := opts.Port + opts.Nodes
		arbiterPort = &ap
		launchedPorts = append(launchedPorts, ap)

		subPath := fmt.Sprintf("%s/arb", opts.Name)
		dbPath := filepath.Join(opts.Dir, subPath, "db")
		logPath := filepath.Join(opts.Dir, subPath, "mongod.log")
		_ = os.MkdirAll(dbPath, 0755)

		cmdArgs := []string{
			mongodPath,
			"--dbpath", dbPath,
			"--logpath", logPath,
			"--port", strconv.Itoa(ap),
			"--replSet", opts.Name,
			"--fork",
		}
		if opts.BindIP != "" {
			cmdArgs = append(cmdArgs, "--bind_ip", opts.BindIP)
		}
		if keyfilePath != "" {
			cmdArgs = append(cmdArgs, "--keyFile", keyfilePath)
		}
		cmdArgs = append(cmdArgs, opts.ExtraArgs...)

		state.StartupInfo[strconv.Itoa(ap)] = strings.Join(cmdArgs, " ")
		if err := runCommand(cmdArgs); err != nil {
			return fmt.Errorf("failed to start arbiter on port %d: %w", ap, err)
		}
	}

	if err := WaitForHosts(opts.Hostname, launchedPorts, 30*time.Second, true); err != nil {
		return err
	}

	// Initiate replica set
	configDoc := BuildReplSetDoc(opts.Name, opts.Hostname, ports, arbiterPort, opts.Priority)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := InitiateReplSet(ctx, opts.Hostname, ports[0], configDoc); err != nil {
		return err
	}

	if err := WaitForPrimary(opts.Hostname, ports[0], 30*time.Second); err != nil {
		return err
	}

	if opts.Auth {
		ctxUser, cancelUser := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelUser()
		_ = CreateAdminUser(ctxUser, opts.Hostname, ports[0], opts.Username, opts.Password, opts.AuthDB, opts.AuthRoles)
	}

	return nil
}

func initSharded(opts *ClusterOptions, mongodPath, mongosPath, keyfilePath string, state *StartupState) error {
	shardNames := resolveShardNames(opts.Sharded)
	currentPort := opts.Port

	// 1. Mongos ports
	var mongosPorts []int
	for i := 0; i < opts.MongosNodes; i++ {
		mongosPorts = append(mongosPorts, currentPort)
		currentPort++
	}

	// 2. Shards
	type shardInfo struct {
		name    string
		ports   []int
		arbPort *int
		connStr string
	}
	var shards []shardInfo
	var allShardPorts []int

	for _, sName := range shardNames {
		var sPorts []int
		for i := 0; i < opts.Nodes; i++ {
			sPorts = append(sPorts, currentPort)
			allShardPorts = append(allShardPorts, currentPort)
			currentPort++
		}
		var sArb *int
		if opts.Arbiter {
			ap := currentPort
			sArb = &ap
			allShardPorts = append(allShardPorts, currentPort)
			currentPort++
		}

		var hostList []string
		for _, p := range sPorts {
			hostList = append(hostList, fmt.Sprintf("%s:%d", opts.Hostname, p))
		}
		connStr := fmt.Sprintf("%s/%s", sName, strings.Join(hostList, ","))

		shards = append(shards, shardInfo{
			name:    sName,
			ports:   sPorts,
			arbPort: sArb,
			connStr: connStr,
		})
	}

	// 3. Config Server Replica Set (CSRS)
	var configPorts []int
	for i := 0; i < opts.ConfigNodes; i++ {
		configPorts = append(configPorts, currentPort)
		currentPort++
	}

	var configHostList []string
	for _, p := range configPorts {
		configHostList = append(configHostList, fmt.Sprintf("%s:%d", opts.Hostname, p))
	}
	configReplConnStr := fmt.Sprintf("configRepl/%s", strings.Join(configHostList, ","))

	// Launch Config Servers (without keyfile initially if auth is enabled)
	for i, cp := range configPorts {
		subPath := fmt.Sprintf("configRepl/rs%d", i+1)
		dbPath := filepath.Join(opts.Dir, subPath, "db")
		logPath := filepath.Join(opts.Dir, subPath, "mongod.log")
		_ = os.MkdirAll(dbPath, 0755)

		// Command line stored in startup_info (with keyfile if auth)
		persistedArgs := []string{
			mongodPath,
			"--configsvr",
			"--replSet", "configRepl",
			"--dbpath", dbPath,
			"--logpath", logPath,
			"--port", strconv.Itoa(cp),
			"--fork",
		}
		if opts.BindIP != "" {
			persistedArgs = append(persistedArgs, "--bind_ip", opts.BindIP)
		}
		if keyfilePath != "" {
			persistedArgs = append(persistedArgs, "--keyFile", keyfilePath)
		}
		persistedArgs = append(persistedArgs, opts.ExtraArgs...)
		state.StartupInfo[strconv.Itoa(cp)] = strings.Join(persistedArgs, " ")

		// For initial launch, do not pass keyfile so shards can be added without auth deadlock
		launchArgs := []string{
			mongodPath,
			"--configsvr",
			"--replSet", "configRepl",
			"--dbpath", dbPath,
			"--logpath", logPath,
			"--port", strconv.Itoa(cp),
			"--fork",
		}
		if opts.BindIP != "" {
			launchArgs = append(launchArgs, "--bind_ip", opts.BindIP)
		}
		launchArgs = append(launchArgs, opts.ExtraArgs...)

		if err := runCommand(launchArgs); err != nil {
			return fmt.Errorf("failed to start config server on port %d: %w", cp, err)
		}
	}

	if err := WaitForHosts(opts.Hostname, configPorts, 30*time.Second, true); err != nil {
		return err
	}

	// Initiate configRepl
	cfgDoc := BuildReplSetDoc("configRepl", opts.Hostname, configPorts, nil, false)
	ctxCfg, cancelCfg := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelCfg()
	if err := InitiateReplSet(ctxCfg, opts.Hostname, configPorts[0], cfgDoc); err != nil {
		return fmt.Errorf("failed to initiate configRepl: %w", err)
	}
	if err := WaitForPrimary(opts.Hostname, configPorts[0], 30*time.Second); err != nil {
		return err
	}

	// Launch all Shards (without keyfile initially)
	for _, shard := range shards {
		for i, sp := range shard.ports {
			subPath := fmt.Sprintf("%s/rs%d", shard.name, i+1)
			dbPath := filepath.Join(opts.Dir, subPath, "db")
			logPath := filepath.Join(opts.Dir, subPath, "mongod.log")
			_ = os.MkdirAll(dbPath, 0755)

			persistedArgs := []string{
				mongodPath,
				"--shardsvr",
				"--replSet", shard.name,
				"--dbpath", dbPath,
				"--logpath", logPath,
				"--port", strconv.Itoa(sp),
				"--fork",
			}
			if opts.BindIP != "" {
				persistedArgs = append(persistedArgs, "--bind_ip", opts.BindIP)
			}
			if keyfilePath != "" {
				persistedArgs = append(persistedArgs, "--keyFile", keyfilePath)
			}
			persistedArgs = append(persistedArgs, opts.ExtraArgs...)
			state.StartupInfo[strconv.Itoa(sp)] = strings.Join(persistedArgs, " ")

			launchArgs := []string{
				mongodPath,
				"--shardsvr",
				"--replSet", shard.name,
				"--dbpath", dbPath,
				"--logpath", logPath,
				"--port", strconv.Itoa(sp),
				"--fork",
			}
			if opts.BindIP != "" {
				launchArgs = append(launchArgs, "--bind_ip", opts.BindIP)
			}
			launchArgs = append(launchArgs, opts.ExtraArgs...)

			if err := runCommand(launchArgs); err != nil {
				return fmt.Errorf("failed to start shard %s on port %d: %w", shard.name, sp, err)
			}
		}

		if shard.arbPort != nil {
			subPath := fmt.Sprintf("%s/arb", shard.name)
			dbPath := filepath.Join(opts.Dir, subPath, "db")
			logPath := filepath.Join(opts.Dir, subPath, "mongod.log")
			_ = os.MkdirAll(dbPath, 0755)

			persistedArgs := []string{
				mongodPath,
				"--shardsvr",
				"--replSet", shard.name,
				"--dbpath", dbPath,
				"--logpath", logPath,
				"--port", strconv.Itoa(*shard.arbPort),
				"--fork",
			}
			if opts.BindIP != "" {
				persistedArgs = append(persistedArgs, "--bind_ip", opts.BindIP)
			}
			if keyfilePath != "" {
				persistedArgs = append(persistedArgs, "--keyFile", keyfilePath)
			}
			persistedArgs = append(persistedArgs, opts.ExtraArgs...)
			state.StartupInfo[strconv.Itoa(*shard.arbPort)] = strings.Join(persistedArgs, " ")

			launchArgs := []string{
				mongodPath,
				"--shardsvr",
				"--replSet", shard.name,
				"--dbpath", dbPath,
				"--logpath", logPath,
				"--port", strconv.Itoa(*shard.arbPort),
				"--fork",
			}
			if opts.BindIP != "" {
				launchArgs = append(launchArgs, "--bind_ip", opts.BindIP)
			}
			launchArgs = append(launchArgs, opts.ExtraArgs...)

			if err := runCommand(launchArgs); err != nil {
				return fmt.Errorf("failed to start arbiter for %s on port %d: %w", shard.name, *shard.arbPort, err)
			}
		}
	}

	if err := WaitForHosts(opts.Hostname, allShardPorts, 30*time.Second, true); err != nil {
		return err
	}

	// Initiate all shard replica sets
	for _, shard := range shards {
		sDoc := BuildReplSetDoc(shard.name, opts.Hostname, shard.ports, shard.arbPort, false)
		ctxShard, cancelShard := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancelShard()
		if err := InitiateReplSet(ctxShard, opts.Hostname, shard.ports[0], sDoc); err != nil {
			return fmt.Errorf("failed to initiate replica set for %s: %w", shard.name, err)
		}
		if err := WaitForPrimary(opts.Hostname, shard.ports[0], 30*time.Second); err != nil {
			return err
		}
	}

	// Launch mongos routers (without keyfile initially)
	for _, mp := range mongosPorts {
		logPath := filepath.Join(opts.Dir, fmt.Sprintf("mongos_%d.log", mp))
		persistedArgs := []string{
			mongosPath,
			"--configdb", configReplConnStr,
			"--logpath", logPath,
			"--port", strconv.Itoa(mp),
			"--fork",
		}
		if opts.BindIP != "" {
			persistedArgs = append(persistedArgs, "--bind_ip", opts.BindIP)
		}
		if keyfilePath != "" {
			persistedArgs = append(persistedArgs, "--keyFile", keyfilePath)
		}
		persistedArgs = append(persistedArgs, opts.ExtraArgs...)
		state.StartupInfo[strconv.Itoa(mp)] = strings.Join(persistedArgs, " ")

		launchArgs := []string{
			mongosPath,
			"--configdb", configReplConnStr,
			"--logpath", logPath,
			"--port", strconv.Itoa(mp),
			"--fork",
		}
		if opts.BindIP != "" {
			launchArgs = append(launchArgs, "--bind_ip", opts.BindIP)
		}
		launchArgs = append(launchArgs, opts.ExtraArgs...)

		if err := runCommand(launchArgs); err != nil {
			return fmt.Errorf("failed to start mongos on port %d: %w", mp, err)
		}
	}

	if err := WaitForHosts(opts.Hostname, mongosPorts, 30*time.Second, true); err != nil {
		return err
	}

	// Add shards via primary mongos (mongosPorts[0])
	for _, shard := range shards {
		if err := AddShard(opts.Hostname, mongosPorts[0], shard.connStr, shard.name, 30*time.Second); err != nil {
			return err
		}
	}

	if err := WaitForShards(opts.Hostname, mongosPorts[0], len(shards), 30*time.Second); err != nil {
		return err
	}

	if opts.Auth {
		ctxUser, cancelUser := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelUser()
		if err := CreateAdminUser(ctxUser, opts.Hostname, mongosPorts[0], opts.Username, opts.Password, opts.AuthDB, opts.AuthRoles); err != nil {
			return fmt.Errorf("failed to create admin user on mongos: %w", err)
		}

		// Also create on shard primaries
		for _, shard := range shards {
			ctxShardUser, cancelShardUser := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancelShardUser()
			_ = CreateAdminUser(ctxShardUser, opts.Hostname, shard.ports[0], opts.Username, opts.Password, opts.AuthDB, opts.AuthRoles)
		}
	}

	return nil
}

func resolveShardNames(shardedArg []string) []string {
	if len(shardedArg) == 1 {
		if n, err := strconv.Atoi(shardedArg[0]); err == nil && n > 0 {
			var names []string
			for i := 1; i <= n; i++ {
				names = append(names, fmt.Sprintf("shard%02d", i))
			}
			return names
		}
	}
	return shardedArg
}

// Start starts all or matching tagged nodes in an existing cluster.
func Start(dir string, binaryPath string, tags []string, verbose bool) error {
	state, err := LoadState(dir)
	if err != nil {
		return err
	}

	nodes := DiscoverNodes(state, "localhost")
	targets := FilterNodesByTags(nodes, tags)

	var configNodes []NodeInfo
	var mongodNodes []NodeInfo
	var mongosNodes []NodeInfo

	for _, n := range targets {
		if n.Status == "down" {
			switch n.ProcessType {
			case "config":
				configNodes = append(configNodes, n)
			case "mongos":
				mongosNodes = append(mongosNodes, n)
			default:
				mongodNodes = append(mongodNodes, n)
			}
		}
	}

	if len(configNodes) == 0 && len(mongodNodes) == 0 && len(mongosNodes) == 0 {
		if verbose {
			fmt.Println("All targeted nodes are already running.")
		}
		return nil
	}

	// 1. Start config servers first and wait
	if len(configNodes) > 0 {
		var ports []int
		for _, n := range configNodes {
			if err := startNode(n, binaryPath); err != nil {
				return err
			}
			ports = append(ports, n.Port)
		}
		if err := WaitForHosts("localhost", ports, 30*time.Second, true); err != nil {
			return fmt.Errorf("timeout waiting for config servers: %w", err)
		}
	}

	// 2. Start data/shard mongod nodes and wait
	if len(mongodNodes) > 0 {
		var ports []int
		for _, n := range mongodNodes {
			if err := startNode(n, binaryPath); err != nil {
				return err
			}
			ports = append(ports, n.Port)
		}
		if err := WaitForHosts("localhost", ports, 30*time.Second, true); err != nil {
			return fmt.Errorf("timeout waiting for data/shard nodes: %w", err)
		}
	}

	// 3. Start mongos routers and wait
	if len(mongosNodes) > 0 {
		var ports []int
		for _, n := range mongosNodes {
			if err := startNode(n, binaryPath); err != nil {
				return err
			}
			ports = append(ports, n.Port)
		}
		if err := WaitForHosts("localhost", ports, 30*time.Second, true); err != nil {
			return fmt.Errorf("timeout waiting for mongos routers: %w", err)
		}
	}

	return nil
}

func startNode(n NodeInfo, binaryPath string) error {
	parts := strings.Fields(n.StartupCmd)
	if binaryPath != "" && len(parts) > 0 {
		parts[0] = filepath.Join(binaryPath, filepath.Base(parts[0]))
	}
	if err := runCommand(parts); err != nil {
		return fmt.Errorf("failed to start node on port %d: %w", n.Port, err)
	}
	return nil
}

// Stop stops all or matching tagged running nodes.
func Stop(dir string, tags []string, verbose bool) error {
	return Kill(dir, "15", tags, verbose)
}

// Restart stops and starts nodes in an existing cluster.
func Restart(dir string, binaryPath string, tags []string, verbose bool) error {
	if err := Stop(dir, tags, verbose); err != nil {
		return err
	}
	return Start(dir, binaryPath, tags, verbose)
}

// Kill terminates running processes matching tags using the given signal.
func Kill(dir string, signalStr string, tags []string, verbose bool) error {
	state, err := LoadState(dir)
	if err != nil {
		return err
	}

	nodes := DiscoverNodes(state, "localhost")
	targets := FilterNodesByTags(nodes, tags)

	sig := parseSignal(signalStr)

	var runningPorts []int
	var runningPIDs []int

	for _, n := range targets {
		if n.Status == "running" && n.PID > 0 {
			proc, err := os.FindProcess(n.PID)
			if err == nil {
				_ = proc.Signal(sig)
				runningPorts = append(runningPorts, n.Port)
				runningPIDs = append(runningPIDs, n.PID)
			}
		}
	}

	if len(runningPorts) == 0 {
		return nil
	}

	// 1. Wait for TCP ports to close
	_ = WaitForHosts("localhost", runningPorts, 30*time.Second, false)

	// 2. Wait until processes completely exit at OS level (releasing file locks)
	waitForPIDsExit(runningPIDs, 15*time.Second)

	return nil
}

func waitForPIDsExit(pids []int, timeout time.Duration) {
	start := time.Now()
	for {
		allDead := !slices.ContainsFunc(pids, isPIDAlive)
		if allDead || time.Since(start) > timeout {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func isPIDAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}

// List prints a summary table or JSON list of cluster nodes.
func List(dir string, jsonOut, showTags, showStartup, verbose bool) error {
	state, err := LoadState(dir)
	if err != nil {
		return err
	}

	nodes := DiscoverNodes(state, "localhost")

	if jsonOut {
		return PrintJSON(nodes)
	}

	PrintTable(nodes, showTags, showStartup, verbose)
	return nil
}

func findBinary(name, binaryPath string) (string, error) {
	if binaryPath != "" {
		p := filepath.Join(binaryPath, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("binary %s not found in %s", name, binaryPath)
	}

	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("binary %s not found in PATH or specified --binarypath", name)
	}
	return p, nil
}

func runCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("empty command")
	}
	cmd := exec.Command(args[0], args[1:]...)
	return cmd.Run()
}

func parseSignal(sigStr string) syscall.Signal {
	switch strings.ToUpper(sigStr) {
	case "9", "SIGKILL", "KILL":
		return syscall.SIGKILL
	case "2", "SIGINT", "INT":
		return syscall.SIGINT
	case "15", "SIGTERM", "TERM":
		return syscall.SIGTERM
	default:
		if n, err := strconv.Atoi(sigStr); err == nil {
			return syscall.Signal(n)
		}
		return syscall.SIGTERM
	}
}
