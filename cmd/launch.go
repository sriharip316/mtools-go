package cmd

import (
	"github.com/spf13/cobra"
	"github.com/sriharip316/mtools-go/internal/launch"
)

// NewLaunchCmd creates the launch command with all subcommands.
func NewLaunchCmd() *cobra.Command {
	var (
		dir         string
		port        int
		binaryPath  string
		verbose     bool
		hostname    string
		bindIP      string
		single      bool
		replicaset  bool
		nodes       int
		arbiter     bool
		name        string
		priority    bool
		sharded     []string
		configNodes int
		mongosNodes int
		auth        bool
		username    string
		password    string
		authDB      string
		authRoles   []string
		signalStr   string
		jsonOut     bool
		showTags    bool
		showStartup bool
	)

	cmd := &cobra.Command{
		Use:   "launch [command] [flags]",
		Short: "MongoDB environment launcher and process manager",
		Long: `launch enables rapid local deployment, lifecycle management, and discovery
of MongoDB standalone instances, replica sets, and sharded clusters.`,
		// If no subcommand is given, default to init (or list if environment already exists and no init flags)
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := &launch.ClusterOptions{
				Dir:         dir,
				Port:        port,
				BinaryPath:  binaryPath,
				Verbose:     verbose,
				Hostname:    hostname,
				BindIP:      bindIP,
				Single:      single,
				ReplicaSet:  replicaset,
				Nodes:       nodes,
				Arbiter:     arbiter,
				Name:        name,
				Priority:    priority,
				Sharded:     sharded,
				ConfigNodes: configNodes,
				MongosNodes: mongosNodes,
				Auth:        auth,
				Username:    username,
				Password:    password,
				AuthDB:      authDB,
				AuthRoles:   authRoles,
			}
			return launch.Init(opts)
		},
	}

	// Persistent flags (shared by launch and its subcommands)
	cmd.PersistentFlags().StringVarP(&dir, "dir", "d", "./data", "base data directory")
	cmd.PersistentFlags().StringVar(&binaryPath, "binarypath", "", "path to directory containing MongoDB binaries (mongod, mongos)")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	// Init flags on root command
	addInitFlags(cmd, &port, &hostname, &bindIP, &single, &replicaset, &nodes, &arbiter,
		&name, &priority, &sharded, &configNodes, &mongosNodes, &auth, &username, &password, &authDB, &authRoles)

	// Subcommands
	initCmd := &cobra.Command{
		Use:   "init [flags]",
		Short: "Initialize and start a new MongoDB environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := &launch.ClusterOptions{
				Dir:         dir,
				Port:        port,
				BinaryPath:  binaryPath,
				Verbose:     verbose,
				Hostname:    hostname,
				BindIP:      bindIP,
				Single:      single,
				ReplicaSet:  replicaset,
				Nodes:       nodes,
				Arbiter:     arbiter,
				Name:        name,
				Priority:    priority,
				Sharded:     sharded,
				ConfigNodes: configNodes,
				MongosNodes: mongosNodes,
				Auth:        auth,
				Username:    username,
				Password:    password,
				AuthDB:      authDB,
				AuthRoles:   authRoles,
			}
			return launch.Init(opts)
		},
	}
	addInitFlags(initCmd, &port, &hostname, &bindIP, &single, &replicaset, &nodes, &arbiter,
		&name, &priority, &sharded, &configNodes, &mongosNodes, &auth, &username, &password, &authDB, &authRoles)

	startCmd := &cobra.Command{
		Use:   "start [tags...]",
		Short: "Start stopped instances in an existing environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return launch.Start(dir, binaryPath, args, verbose)
		},
	}

	stopCmd := &cobra.Command{
		Use:   "stop [tags...]",
		Short: "Stop running instances in an environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return launch.Stop(dir, args, verbose)
		},
	}

	restartCmd := &cobra.Command{
		Use:   "restart [tags...]",
		Short: "Restart running instances in an environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return launch.Restart(dir, binaryPath, args, verbose)
		},
	}

	killCmd := &cobra.Command{
		Use:   "kill [tags...]",
		Short: "Send a termination signal to instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			return launch.Kill(dir, signalStr, args, verbose)
		},
	}
	killCmd.Flags().StringVar(&signalStr, "signal", "15", "signal to send (15/SIGTERM, 9/SIGKILL, 2/SIGINT)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all instances and their status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return launch.List(dir, jsonOut, showTags, showStartup, verbose)
		},
	}
	listCmd.Flags().BoolVar(&jsonOut, "json", false, "output in JSON format")
	listCmd.Flags().BoolVar(&showTags, "tags", false, "show tags in output")
	listCmd.Flags().BoolVar(&showStartup, "startup", false, "show startup command in output")

	cmd.AddCommand(initCmd, startCmd, stopCmd, restartCmd, killCmd, listCmd)
	return cmd
}

func addInitFlags(cmd *cobra.Command, port *int, hostname, bindIP *string,
	single, replicaset *bool, nodes *int, arbiter *bool, name *string,
	priority *bool, sharded *[]string, configNodes, mongosNodes *int,
	auth *bool, username, password, authDB *string, authRoles *[]string) {

	cmd.Flags().IntVarP(port, "port", "p", 27017, "starting port number")
	cmd.Flags().StringVar(hostname, "hostname", "localhost", "override hostname for replica set members")
	cmd.Flags().StringVar(bindIP, "bind_ip", "", "ip interface to bind to")

	cmd.Flags().BoolVar(single, "single", false, "deploy a single standalone instance")
	cmd.Flags().BoolVar(replicaset, "replicaset", false, "deploy a replica set")
	cmd.Flags().IntVarP(nodes, "nodes", "n", 3, "number of data nodes per replica set")
	cmd.Flags().BoolVar(arbiter, "arbiter", false, "add an arbiter to replica set")
	cmd.Flags().StringVar(name, "name", "replset", "name of the replica set")
	cmd.Flags().BoolVar(priority, "priority", false, "give primary (node 0) higher priority")

	cmd.Flags().StringSliceVar(sharded, "sharded", nil, "deploy sharded cluster (number of shards or list of shard names)")
	cmd.Flags().StringSliceVar(sharded, "shards", nil, "alias for --sharded")
	cmd.Flags().IntVar(configNodes, "config", 1, "number of config servers in CSRS")
	cmd.Flags().IntVar(mongosNodes, "mongos", 1, "number of mongos query routers")

	cmd.Flags().BoolVar(auth, "auth", false, "enable authentication and generate keyfile")
	cmd.Flags().StringVar(username, "username", "user", "admin username")
	cmd.Flags().StringVar(password, "password", "password", "admin password")
	cmd.Flags().StringVar(authDB, "auth-db", "admin", "admin auth db")
	cmd.Flags().StringSliceVar(authRoles, "auth-roles", nil, "roles granted to admin user")
}
