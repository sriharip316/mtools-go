package launch

// ProtocolVersion specifies the version format of .mlaunch_startup file.
const ProtocolVersion = 2

// StartupState represents the stored environment state in .mlaunch_startup.
type StartupState struct {
	ProtocolVersion int               `json:"protocol_version"`
	MtoolsVersion   string            `json:"mtools_version"`
	ParsedArgs      map[string]any    `json:"parsed_args"`
	UnknownArgs     []string          `json:"unknown_args"`
	StartupInfo     map[string]string `json:"startup_info"`
}

// NodeInfo represents the discovered state and metadata of a single MongoDB node.
type NodeInfo struct {
	ProcessType string   `json:"process_type"` // "mongod", "mongos", "config"
	Port        int      `json:"port"`
	Status      string   `json:"status"` // "running", "down"
	PID         int      `json:"pid"`
	Tags        []string `json:"tags"`
	Role        string   `json:"role"` // "primary", "secondary", "arbiter", "standalone", "router"
	ReplSetName string   `json:"replset_name,omitempty"`
	ShardName   string   `json:"shard_name,omitempty"`
	DBPath      string   `json:"dbpath,omitempty"`
	LogPath     string   `json:"logpath,omitempty"`
	StartupCmd  string   `json:"startup_cmd"`
}

// ClusterOptions defines the parameters for creating or operating on a cluster.
type ClusterOptions struct {
	// General options
	Dir        string
	Port       int
	BinaryPath string
	Verbose    bool
	Hostname   string
	BindIP     string

	// Topology options
	Single     bool
	ReplicaSet bool
	Nodes      int
	Arbiter    bool
	Name       string
	Priority   bool

	// Sharded options
	Sharded     []string
	ConfigNodes int
	MongosNodes int

	// Security options
	Auth      bool
	Username  string
	Password  string
	AuthDB    string
	AuthRoles []string

	// Pass-through arguments for mongod/mongos
	ExtraArgs []string
}
