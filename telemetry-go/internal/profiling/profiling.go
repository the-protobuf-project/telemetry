package profiling

import (
	"context"
	"fmt"
	"runtime"

	"github.com/grafana/pyroscope-go"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/foxglove"
	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
)

// Profiler wraps the Pyroscope profiler for continuous profiling
type Profiler struct {
	profiler    *pyroscope.Profiler
	enabled     bool
	mcapWriter  *foxglove.UnifiedMcapWriter
	serviceName string
}

// NewProfiler creates and starts a new Pyroscope profiler instance
// Returns nil if profiling is disabled
func NewProfiler(serviceOpts options.ServiceOptions, opts options.ProfilingOptions, unifiedMcap *foxglove.UnifiedMcapWriter) *Profiler {
	if !opts.Enabled {
		return &Profiler{enabled: false}
	}

	// Set mutex and block profile rates if enabled
	if opts.MutexProfileRate > 0 {
		runtime.SetMutexProfileFraction(opts.MutexProfileRate)
	}
	if opts.BlockProfileRate > 0 {
		runtime.SetBlockProfileRate(opts.BlockProfileRate)
	}

	// Build application name with service metadata
	appName := fmt.Sprintf("%s.%s", serviceOpts.Name, serviceOpts.Environment)

	// Build tags from service metadata
	tags := map[string]string{
		"service":     serviceOpts.Name,
		"version":     serviceOpts.Version,
		"environment": string(serviceOpts.Environment),
	}

	// Add custom tags if provided
	for k, v := range opts.Tags {
		tags[k] = v
	}

	// Configure Pyroscope
	config := pyroscope.Config{
		ApplicationName: appName,
		ServerAddress:   opts.ServerAddress,
		Logger:          nil, // Disable debug logging
		Tags:            tags,
		ProfileTypes:    buildProfileTypes(opts),
	}

	// Add authentication if provided
	if opts.BasicAuthUser != "" {
		config.BasicAuthUser = opts.BasicAuthUser
		config.BasicAuthPassword = opts.BasicAuthPassword
	}

	// Add tenant ID if provided (for multi-tenancy)
	if opts.TenantID != "" {
		config.TenantID = opts.TenantID
	}

	// Start profiler
	profiler, err := pyroscope.Start(config)
	if err != nil {
		return nil
	}

	p := &Profiler{
		profiler:    profiler,
		enabled:     true,
		mcapWriter:  unifiedMcap,
		serviceName: serviceOpts.Name,
	}

	// TODO: Implement profiling data export to MCAP
	// This could include periodic snapshots of:
	// - CPU profiles
	// - Memory allocations
	// - Goroutine counts
	// - Mutex contention

	return p
}

// Stop gracefully stops the profiler and flushes any remaining data
func (p *Profiler) Stop() error {
	if !p.enabled || p.profiler == nil {
		return nil
	}

	if err := p.profiler.Stop(); err != nil {
		return fmt.Errorf("failed to stop profiler: %w", err)
	}

	return nil
}

// TagWrapper adds dynamic tags to a specific code section
// This is useful for adding contextual information to profiles
func (p *Profiler) TagWrapper(ctx context.Context, labels map[string]string, fn func(context.Context)) {
	if !p.enabled {
		fn(ctx)
		return
	}

	// Convert map to pyroscope.Labels format
	labelPairs := make([]string, 0, len(labels)*2)
	for k, v := range labels {
		labelPairs = append(labelPairs, k, v)
	}

	pyroscope.TagWrapper(ctx, pyroscope.Labels(labelPairs...), fn)
}

// buildProfileTypes constructs the list of profile types based on options
func buildProfileTypes(opts options.ProfilingOptions) []pyroscope.ProfileType {
	types := []pyroscope.ProfileType{}

	// Add enabled profile types
	if opts.ProfileCPU {
		types = append(types, pyroscope.ProfileCPU)
	}
	if opts.ProfileAllocObjects {
		types = append(types, pyroscope.ProfileAllocObjects)
	}
	if opts.ProfileAllocSpace {
		types = append(types, pyroscope.ProfileAllocSpace)
	}
	if opts.ProfileInuseObjects {
		types = append(types, pyroscope.ProfileInuseObjects)
	}
	if opts.ProfileInuseSpace {
		types = append(types, pyroscope.ProfileInuseSpace)
	}
	if opts.ProfileGoroutines {
		types = append(types, pyroscope.ProfileGoroutines)
	}
	if opts.ProfileMutexCount {
		types = append(types, pyroscope.ProfileMutexCount)
	}
	if opts.ProfileMutexDuration {
		types = append(types, pyroscope.ProfileMutexDuration)
	}
	if opts.ProfileBlockCount {
		types = append(types, pyroscope.ProfileBlockCount)
	}
	if opts.ProfileBlockDuration {
		types = append(types, pyroscope.ProfileBlockDuration)
	}

	return types
}
