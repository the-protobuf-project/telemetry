package telemetry

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/foxglove"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/logging"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/metrics"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/profiling"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/telemetry"
	"github.com/the-protobuf-project/telemetry/telemetry-go/internal/tracing"
	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
)

// Span is a type alias for tracing.Span to avoid exposing internal packages
type Span = tracing.Span

// LogLevel is a type alias for options.LogLevel so modules can use telemetry.Level1, etc.
type LogLevel = options.LogLevel

// Re-export OpenTelemetry metric functions from internal metrics package
var WithAttributes = metrics.WithAttributes

// Re-export OpenTelemetry attribute creation functions from internal metrics package
var StringAttribute = metrics.StringAttribute

// Log level constants for modules.
//
//   - ModuleLevel_1 (Error)  — stable, production-ready module
//   - ModuleLevel_2 (Info)   — normal operation
//   - ModuleLevel_3 (Debug)  — active development, full observability
const (
	ModuleLevel_1 = options.ModuleLevel_1
	ModuleLevel_2 = options.ModuleLevel_2
	ModuleLevel_3 = options.ModuleLevel_3
)

// Telemetry is the main framework struct that provides access to all telemetry services.
// It supports both the legacy logging interface and the new unified OpenTelemetry-based telemetry.
type Telemetry struct {
	// Logger is the main logging client.
	Logger *logging.Logger
	// Metrics is the main metrics client.
	Metrics *metrics.Metrics
	// Tracing is the main tracing client.
	Tracing *tracing.Tracing
	// Profiler is the main profiler client.
	Profiler *profiling.Profiler

	// Unified OpenTelemetry-based telemetry
	telemetry *telemetry.Telemetry
	// Unified MCAP writer for both logs and metrics
	unifiedMcap *foxglove.UnifiedMcapWriter
	// Internal context for background operations
	ctx context.Context
	// shutdownTimeout bounds Close (WithShutdownTimeout)
	shutdownTimeout time.Duration
}

// DefaultShutdownTimeout bounds Close when WithShutdownTimeout is not given:
// long enough to flush to a collector that is there, short enough that one
// that is not cannot hold up a process's exit.
const DefaultShutdownTimeout = 5 * time.Second

// Builder provides a fluent API for configuring and creating a Telemetry instance.
type Builder struct {
	serviceOpts   options.ServiceOptions
	telemetryOpts options.TelemetryOptions
	configPath    string
	secureSet     bool // WithOTLPSecure decided TLS; autoConfigureOTLP leaves it
	shutdown      time.Duration
	err           error
}

// New creates a new Telemetry Builder with sensible defaults.
// Use the builder methods to configure, then call Build() to create the Telemetry instance.
//
// Example:
//
//	p, err := telemetry.New().
//	    WithService("my-service", "1.0.0").
//	    WithConfig("config.yaml").
//	    Build()
func New() *Builder {
	// Auto-discover and load config on creation
	telemetryOpts, serviceOpts, _ := options.LoadConfigWithDefaults("")
	return &Builder{
		serviceOpts:   *serviceOpts,
		telemetryOpts: *telemetryOpts,
	}
}

// WithConfig loads configuration from a specific file (YAML, JSON, or TOML).
// Use this to override the auto-discovered config.
// Environment variables with TELEMETRY_ prefix override file values.
func (b *Builder) WithConfig(configPath string) *Builder {
	if b.err != nil {
		return b
	}
	if configPath == "" {
		return b // Already loaded via auto-discovery
	}
	b.configPath = configPath
	telemetryOpts, serviceOpts, err := options.LoadConfigWithDefaults(configPath)
	if err != nil {
		b.err = err
		return b
	}
	b.telemetryOpts = *telemetryOpts
	b.serviceOpts = *serviceOpts
	return b
}

// WithService sets the service name and version.
// When this is called, it clears any service-level configuration from the config file
// to avoid collisions between config file and code-level settings.
func (b *Builder) WithService(name, version string) *Builder {
	if b.err != nil {
		return b
	}
	// Reset service options to defaults, ignoring config file values
	b.serviceOpts = options.ServiceOptions{
		Name:        name,
		Version:     version,
		Description: "",
		Environment: options.Development,
		Labels:      make(map[string]string),
	}
	return b
}

// WithDescription sets the service description.
func (b *Builder) WithDescription(description string) *Builder {
	if b.err != nil {
		return b
	}
	b.serviceOpts.Description = description
	return b
}

// WithEnvironment sets the deployment environment.
func (b *Builder) WithEnvironment(env options.Environment) *Builder {
	if b.err != nil {
		return b
	}
	b.serviceOpts.Environment = env
	return b
}

// WithLogLevel sets the log level for this service/module.
// This acts as the code-level default. It can be overridden by the config file
// via [logging.modules.<service-name>] or env vars (TELEMETRY_LOGGING_MODULES_<NAME>_LEVEL).
//
// Priority chain (highest to lowest):
//
//	env var > TOML per-module override > WithLogLevel() > environment-based default
//
// Example:
//
//	p, err := telemetry.New().
//	    WithService("vision", "1.0.0").
//	    WithLogLevel(telemetry.ModuleLevel_3). // "I'm a level 3 module"
//	    Build()
func (b *Builder) WithLogLevel(level LogLevel) *Builder {
	if b.err != nil {
		return b
	}
	// Store as a per-module override keyed by service name.
	// This will be checked in resolveLogLevel via the Modules map.
	// If the TOML also has a module override for this name, the TOML wins
	// because config is loaded first and WithLogLevel is applied only
	// when no config override exists.
	if b.telemetryOpts.Logging.Modules == nil {
		b.telemetryOpts.Logging.Modules = make(map[string]options.ModuleOptions)
	}
	// Only set if config didn't already provide a per-module override for this service
	if _, exists := b.telemetryOpts.Logging.Modules[b.serviceOpts.Name]; !exists {
		b.telemetryOpts.Logging.Modules[b.serviceOpts.Name] = options.ModuleOptions{
			Level: level,
		}
	}
	return b
}

// WithOTLP configures the OTLP endpoint. TLS follows the same rule a
// configured endpoint does (see autoConfigureOTLP): on for a remote host on
// 443 or 4318, the ports a TLS collector serves, and off otherwise -- so a
// plaintext collector beside the service, reached by a host name such as a
// Compose service or a cluster Service, receives what it is sent. Call
// WithOTLPSecure to decide it explicitly.
func (b *Builder) WithOTLP(host string, port int) *Builder {
	if b.err != nil {
		return b
	}
	b.telemetryOpts.Telemetry.OTLP.Host = host //nolint:staticcheck // Deprecated but kept for backward compatibility
	b.telemetryOpts.Telemetry.OTLP.Port = port
	b.telemetryOpts.Telemetry.OTLP.Enabled = true
	b.telemetryOpts.Telemetry.OTLP.Secure = !isLocalHost(host) && (port == 443 || port == 4318)
	return b
}

// WithOTLPSecure turns TLS to the OTLP collector on or off, whatever WithOTLP
// or the configuration inferred.
func (b *Builder) WithOTLPSecure(secure bool) *Builder {
	if b.err != nil {
		return b
	}
	b.telemetryOpts.Telemetry.OTLP.Secure = secure
	b.secureSet = true
	return b
}

// WithOTLPHeaders sets custom headers for OTLP requests (e.g., Authorization).
func (b *Builder) WithOTLPHeaders(headers map[string]string) *Builder {
	if b.err != nil {
		return b
	}
	b.telemetryOpts.Telemetry.OTLP.Headers = headers
	return b
}

// WithPrometheus turns on the Prometheus exporter: every metric the pipeline
// records — the application's own, and those of any library recording through
// the global meter provider — is served in the Prometheus text format by
// [Telemetry.MetricsHandler], for the application to mount at /metrics. It
// works with OTLP export or without it. OTEL_METRICS_EXPORTER=prometheus or
// telemetry.metrics.prometheus = true does the same from the environment or
// the config file; OTEL_SDK_DISABLED=true turns it off.
func (b *Builder) WithPrometheus() *Builder {
	if b.err != nil {
		return b
	}
	b.telemetryOpts.Telemetry.Metrics.Enabled = true
	b.telemetryOpts.Telemetry.Metrics.Prometheus = true
	return b
}

// WithShutdownTimeout bounds how long Close waits to flush and shut down the
// exporters; d <= 0 keeps [DefaultShutdownTimeout]. Whatever is not exported
// by then is dropped, and Close returns the error saying so.
func (b *Builder) WithShutdownTimeout(d time.Duration) *Builder {
	if b.err != nil {
		return b
	}
	b.shutdown = d
	return b
}

// WithMCAP enables MCAP file logging at the specified path.
func (b *Builder) WithMCAP(path string) *Builder {
	if b.err != nil {
		return b
	}
	b.telemetryOpts.Foxglove.Enabled = true
	b.telemetryOpts.Foxglove.McapPath = path
	return b
}

// WithProfiling enables continuous profiling with Pyroscope.
func (b *Builder) WithProfiling(serverAddress string) *Builder {
	if b.err != nil {
		return b
	}
	b.telemetryOpts.Profiling.Enabled = true
	b.telemetryOpts.Profiling.ServerAddress = serverAddress
	return b
}

// WithTracing enables distributed tracing.
func (b *Builder) WithTracing() *Builder {
	if b.err != nil {
		return b
	}
	b.telemetryOpts.Tracing.Enabled = true
	b.telemetryOpts.Telemetry.Tracing.Enabled = true
	return b
}

// WithLabels sets global labels that are added to all telemetry data.
// Use this for constant identifiers like robot.id, device.id, fleet.id, etc.
// These labels will appear on all logs, metrics, and traces.
//
// Example:
//
//	p, err := telemetry.New().
//	    WithService("robot-controller", "1.0.0").
//	    WithLabels(map[string]string{
//	        "robot.id":    "robot-001",
//	        "fleet.id":    "fleet-alpha",
//	        "location.id": "warehouse-1",
//	    }).
//	    Build()
func (b *Builder) WithLabels(labels map[string]string) *Builder {
	if b.err != nil {
		return b
	}
	if b.serviceOpts.Labels == nil {
		b.serviceOpts.Labels = make(map[string]string)
	}
	for k, v := range labels {
		b.serviceOpts.Labels[k] = v
	}
	return b
}

// WithLabel sets a single global label.
// Convenience method for adding one label at a time.
func (b *Builder) WithLabel(key, value string) *Builder {
	if b.err != nil {
		return b
	}
	if b.serviceOpts.Labels == nil {
		b.serviceOpts.Labels = make(map[string]string)
	}
	b.serviceOpts.Labels[key] = value
	return b
}

// otlp is the OTLP configuration Build uses: auto-configured from the host,
// keeping a TLS choice WithOTLPSecure made.
func (b *Builder) otlp() options.OTLPOptions {
	otlp := b.telemetryOpts.Telemetry.OTLP
	secure := otlp.Secure
	autoConfigureOTLP(&otlp)
	if b.secureSet {
		otlp.Secure = secure
	}
	return otlp
}

// Build creates the Telemetry instance with the configured options.
func (b *Builder) Build() (*Telemetry, error) {
	if b.err != nil {
		return nil, b.err
	}

	b.telemetryOpts.Telemetry.OTLP = b.otlp()
	applySDKDisabled(&b.telemetryOpts.Telemetry)

	ctx := context.Background()

	// Initialize unified telemetry service
	tel, err := telemetry.New(ctx, b.serviceOpts, b.telemetryOpts.Telemetry)
	if err != nil {
		return nil, err
	}

	// Initialize unified MCAP writer if Foxglove is enabled
	var unifiedMcap *foxglove.UnifiedMcapWriter
	if b.telemetryOpts.Foxglove.Enabled && b.telemetryOpts.Foxglove.McapPath != "" {
		unifiedMcap, err = foxglove.NewUnifiedMcapWriter(b.serviceOpts, b.telemetryOpts.Foxglove)
		if err != nil {
			return nil, err
		}
	}

	p := &Telemetry{
		ctx:             ctx,
		shutdownTimeout: b.shutdown,
		telemetry:       tel,
		unifiedMcap:     unifiedMcap,
		Logger:          logging.NewLogger(b.serviceOpts, b.telemetryOpts.Logging, unifiedMcap, tel.GetLogger()),
		Metrics:         metrics.NewMetrics(b.serviceOpts, unifiedMcap, tel.GetMetrics()),
		Tracing:         tracing.NewTracing(b.serviceOpts, b.telemetryOpts.Tracing, unifiedMcap, tel.GetTracer()),
		Profiler:        profiling.NewProfiler(b.serviceOpts, b.telemetryOpts.Profiling, unifiedMcap),
	}

	return p, nil
}

// isLocalHost checks if the host is a local address (localhost, 127.x.x.x, or private IP).
func isLocalHost(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || host == "" {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		// Not an IP, check if it's a domain
		return false
	}

	// Check for loopback (127.x.x.x)
	if ip.IsLoopback() {
		return true
	}

	// Check for private IPs (10.x.x.x, 172.16-31.x.x, 192.168.x.x)
	if ip.IsPrivate() {
		return true
	}

	return false
}

// autoConfigureOTLP decides whether OTLP exports, and applies transport
// defaults based on the configured endpoint or host.
//
// OTLP is on when an endpoint or a host is configured, or when it is enabled
// explicitly; enabled with neither, it exports to the OTLP default,
// localhost:4317. Nothing configured, it is off, so a process (a test above
// all) never dials a collector it was not pointed at. The environment's
// OTEL_EXPORTER_OTLP_ENABLED=false or OTEL_SDK_DISABLED=true turns it off
// whatever else says.
//
// Remote hosts using ports 443 or 4318 enable TLS and HTTP; port 4317 retains
// its configured security setting.
func autoConfigureOTLP(otlp *options.OTLPOptions) {
	if options.OTLPDisabledByEnv() {
		otlp.Enabled = false
		return
	}

	//nolint:staticcheck // Host is deprecated but kept for backward compatibility
	if otlp.Endpoint != "" || otlp.Host != "" {
		otlp.Enabled = true
	}
	if !otlp.Enabled {
		return
	}
	if otlp.Endpoint == "" && otlp.Host == "" { //nolint:staticcheck // Deprecated
		otlp.Host = "localhost" //nolint:staticcheck // Deprecated
		if otlp.Port == 0 {
			otlp.Port = 4317
		}
	}
	if otlp.Host == "" { //nolint:staticcheck // Deprecated
		return
	}

	isLocal := isLocalHost(otlp.Host) //nolint:staticcheck // Deprecated but kept for backward compatibility

	// Auto-set secure based on host (unless explicitly set)
	if !otlp.Secure && !isLocal {
		// Remote host on standard OTLP ports - check if it needs TLS
		switch otlp.Port {
		case 443, 4318:
			otlp.Secure = true
			otlp.UseHTTP = true // Port 443 typically needs HTTP
		case 4317:
			// Standard gRPC port - may or may not need TLS
			// Keep as-is, user should configure if needed
		}
	}
}

// applySDKDisabled turns every exporter off when OTEL_SDK_DISABLED is true.
func applySDKDisabled(opts *options.OpenTelemetryOptions) {
	if options.SDKDisabledByEnv() {
		opts.OTLP.Enabled = false
		opts.Metrics.Prometheus = false
	}
}

// NewLegacy creates a new Telemetry instance using the legacy API (for backward compatibility).
// NewLegacy creates telemetry clients from an explicit context and configuration.
// Deprecated: Use New().WithConfig().Build() instead.
func NewLegacy(ctx context.Context, serviceOpts options.ServiceOptions, opts options.TelemetryOptions) (*Telemetry, error) {
	// Auto-configure OTLP settings
	autoConfigureOTLP(&opts.Telemetry.OTLP)
	applySDKDisabled(&opts.Telemetry)

	// Initialize unified telemetry service
	tel, err := telemetry.New(ctx, serviceOpts, opts.Telemetry)
	if err != nil {
		return nil, err
	}

	// Initialize unified MCAP writer if Foxglove is enabled
	var unifiedMcap *foxglove.UnifiedMcapWriter
	if opts.Foxglove.Enabled && opts.Foxglove.McapPath != "" {
		unifiedMcap, err = foxglove.NewUnifiedMcapWriter(serviceOpts, opts.Foxglove)
		if err != nil {
			return nil, err
		}
	}

	p := &Telemetry{
		ctx:         ctx,
		telemetry:   tel,
		unifiedMcap: unifiedMcap,
		Logger:      logging.NewLogger(serviceOpts, opts.Logging, unifiedMcap, tel.GetLogger()),
		Metrics:     metrics.NewMetrics(serviceOpts, unifiedMcap, tel.GetMetrics()),
		Tracing:     tracing.NewTracing(serviceOpts, opts.Tracing, unifiedMcap, tel.GetTracer()),
		Profiler:    profiling.NewProfiler(serviceOpts, opts.Profiling, unifiedMcap),
	}

	return p, nil
}

// Close gracefully shuts down all telemetry services, flushing what the
// exporters hold. It takes at most the shutdown timeout
// ([DefaultShutdownTimeout] unless WithShutdownTimeout says otherwise), so a
// collector that is not listening cannot hold up the process's exit.
func (p *Telemetry) Close() error {
	// Stop profiler first to flush remaining data
	if p.Profiler != nil {
		if err := p.Profiler.Stop(); err != nil {
			// Log error but continue with shutdown
			if p.Logger != nil {
				p.Logger.Warn("Failed to stop profiler", map[string]interface{}{"error": err.Error()})
			}
		}
	}

	// Close unified MCAP writer first (before logger tries to log about it)
	if p.unifiedMcap != nil {
		_ = p.unifiedMcap.Close() // Ignore error during shutdown
	}

	// Close metrics (no-op since unified writer is already closed)
	if p.Metrics != nil {
		_ = p.Metrics.Close() // Ignore error during shutdown
	}

	// Close logger (no-op since unified writer is already closed)
	if p.Logger != nil {
		_ = p.Logger.Close() // Ignore error during shutdown
	}

	// Close tracing (no-op since unified writer is already closed)
	if p.Tracing != nil {
		_ = p.Tracing.Close() // Ignore error during shutdown
	}

	if p.telemetry != nil {
		ctx := p.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		timeout := p.shutdownTimeout
		if timeout <= 0 {
			timeout = DefaultShutdownTimeout
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		return p.telemetry.Shutdown(ctx)
	}
	return nil
}

// MetricsHandler serves every metric the pipeline records in the Prometheus
// text format, for the application to mount — conventionally at /metrics:
//
//	if h := t.MetricsHandler(); h != nil {
//	    mux.Handle("GET /metrics", h)
//	}
//
// It is nil unless the Prometheus exporter is on ([Builder.WithPrometheus],
// OTEL_METRICS_EXPORTER=prometheus, or telemetry.metrics.prometheus = true).
func (p *Telemetry) MetricsHandler() http.Handler {
	if p == nil || p.telemetry == nil {
		return nil
	}
	return p.telemetry.MetricsHandler()
}
