package options

// ProfilingOptions defines the settings for continuous profiling with Pyroscope
type ProfilingOptions struct {
	Enabled       bool   `json:"enabled"`        // Enable continuous profiling
	ServerAddress string `json:"server_address"` // Pyroscope server URL (e.g., "http://localhost:4040")

	// Authentication (optional, required for Grafana Cloud)
	BasicAuthUser     string `json:"basic_auth_user"`     // Basic auth username
	BasicAuthPassword string `json:"basic_auth_password"` // Basic auth password
	TenantID          string `json:"tenant_id"`           // Tenant ID for multi-tenancy (optional)

	// Profile types - enable/disable specific profiling types
	ProfileCPU           bool `json:"profile_cpu"`            // CPU profiling (default: true)
	ProfileAllocObjects  bool `json:"profile_alloc_objects"`  // Allocation objects profiling (default: true)
	ProfileAllocSpace    bool `json:"profile_alloc_space"`    // Allocation space profiling (default: true)
	ProfileInuseObjects  bool `json:"profile_inuse_objects"`  // In-use objects profiling (default: true)
	ProfileInuseSpace    bool `json:"profile_inuse_space"`    // In-use space profiling (default: true)
	ProfileGoroutines    bool `json:"profile_goroutines"`     // Goroutines profiling (default: false)
	ProfileMutexCount    bool `json:"profile_mutex_count"`    // Mutex count profiling (default: false)
	ProfileMutexDuration bool `json:"profile_mutex_duration"` // Mutex duration profiling (default: false)
	ProfileBlockCount    bool `json:"profile_block_count"`    // Block count profiling (default: false)
	ProfileBlockDuration bool `json:"profile_block_duration"` // Block duration profiling (default: false)

	// Profile rates
	MutexProfileRate int `json:"mutex_profile_rate"` // Mutex profile fraction (e.g., 5 = 1/5 events reported)
	BlockProfileRate int `json:"block_profile_rate"` // Block profile rate in nanoseconds (e.g., 5)

	// Custom tags (optional)
	Tags map[string]string `json:"tags"` // Additional tags to attach to profiles
}
