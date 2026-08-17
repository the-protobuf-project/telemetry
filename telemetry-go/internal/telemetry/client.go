package telemetry

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc/credentials"
)

// Telemetry provides a unified interface for OpenTelemetry logging, metrics, and tracing.
// It simplifies the integration of observability into applications by providing a single
// entry point for all telemetry operations.
type Telemetry struct {
	serviceName string
	resource    *resource.Resource

	// OpenTelemetry providers
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	loggerProvider *sdklog.LoggerProvider

	// Public interfaces for users
	Logger  *Logger
	Metrics *Metrics
	tracer  *Tracer

	// Shutdown function
	shutdownFuncs []func(context.Context) error
}

// noopErrorHandler silently discards OpenTelemetry internal errors
// (e.g. exporter connection refused) to avoid polluting application output.
type noopErrorHandler struct{}

func (n noopErrorHandler) Handle(_ error) {}

// New creates a new Telemetry instance with OpenTelemetry SDK configured
// based on the provided service and telemetry options.
func New(ctx context.Context, serviceOpts options.ServiceOptions, telemetryOpts options.OpenTelemetryOptions) (*Telemetry, error) {
	// Suppress noisy OTel exporter errors (e.g. connection refused)
	otel.SetErrorHandler(noopErrorHandler{})

	t := &Telemetry{
		serviceName:   serviceOpts.Name,
		shutdownFuncs: make([]func(context.Context) error, 0),
	}

	// Create resource with service information
	res, err := t.createResource(serviceOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}
	t.resource = res

	// Initialize tracing
	if telemetryOpts.Tracing.Enabled {
		if err := t.initTracing(ctx, telemetryOpts); err != nil {
			return nil, fmt.Errorf("failed to initialize tracing: %w", err)
		}
	}

	// Initialize metrics
	if telemetryOpts.Metrics.Enabled {
		if err := t.initMetrics(ctx, telemetryOpts); err != nil {
			return nil, fmt.Errorf("failed to initialize metrics: %w", err)
		}
	}

	// Initialize logging
	if telemetryOpts.Logging.Enabled {
		if err := t.initLogging(ctx, telemetryOpts); err != nil {
			return nil, fmt.Errorf("failed to initialize logging: %w", err)
		}
	}

	return t, nil
}

// createResource creates an OpenTelemetry resource with service metadata
func (t *Telemetry) createResource(serviceOpts options.ServiceOptions) (*resource.Resource, error) {
	// Build base attributes
	attrs := []attribute.KeyValue{
		semconv.ServiceName(serviceOpts.Name),
		semconv.ServiceVersion(serviceOpts.Version),
		attribute.String("service.description", serviceOpts.Description),
		attribute.String("environment", string(serviceOpts.Environment)),
	}

	// Add global labels (e.g., robot.id, device.id)
	for key, value := range serviceOpts.Labels {
		attrs = append(attrs, attribute.String(key, value))
	}

	// Create resource with service attributes
	// Note: We don't specify SchemaURL to avoid conflicts with resource.Default()
	customResource, err := resource.New(
		context.Background(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		return nil, err
	}

	// Merge with default resource
	return resource.Merge(
		resource.Default(),
		customResource,
	)
}

// resolveOTLPConfig resolves the OTLP endpoint and headers from the options.
// Supports both new (endpoint, auth_token) and legacy (host, port, headers) config.
func resolveOTLPConfig(otlp *options.OTLPOptions) (endpoint string, headers map[string]string, secure bool) {
	headers = make(map[string]string)

	// Copy existing headers
	for k, v := range otlp.Headers {
		headers[k] = v
	}

	// Add auth token as Authorization header if provided
	if otlp.AuthToken != "" {
		headers["Authorization"] = "Bearer " + otlp.AuthToken
	}

	// Resolve endpoint: prefer new Endpoint field, fallback to Host:Port
	if otlp.Endpoint != "" {
		endpoint = otlp.Endpoint
		// Add default port if not specified
		if !strings.Contains(endpoint, ":") {
			if otlp.UseHTTP {
				endpoint = endpoint + ":4318"
			} else {
				endpoint = endpoint + ":4317"
			}
		}
	} else if otlp.Host != "" { //nolint:staticcheck // Deprecated but kept for backward compatibility
		if otlp.Port > 0 {
			endpoint = fmt.Sprintf("%s:%d", otlp.Host, otlp.Port) //nolint:staticcheck // Deprecated
		} else {
			// Auto-detect port based on protocol
			if otlp.UseHTTP {
				endpoint = fmt.Sprintf("%s:4318", otlp.Host) //nolint:staticcheck // Deprecated
			} else {
				endpoint = fmt.Sprintf("%s:4317", otlp.Host) //nolint:staticcheck // Deprecated
			}
		}
	}

	// Use explicit secure setting only - don't auto-detect
	// Most OTLP collectors (including otel.the-protobuf-project.dev) use insecure gRPC on port 4317
	secure = otlp.Secure

	return endpoint, headers, secure
}

// initTracing initializes the OpenTelemetry tracing pipeline
func (t *Telemetry) initTracing(ctx context.Context, opts options.OpenTelemetryOptions) error {
	var exporter sdktrace.SpanExporter
	var err error

	if opts.OTLP.Enabled {
		endpoint, headers, secure := resolveOTLPConfig(&opts.OTLP)
		if opts.OTLP.UseHTTP {
			// Use HTTP exporter
			httpOpts := []otlptracehttp.Option{
				otlptracehttp.WithEndpoint(endpoint),
			}
			if !secure {
				httpOpts = append(httpOpts, otlptracehttp.WithInsecure())
			}
			if len(headers) > 0 {
				httpOpts = append(httpOpts, otlptracehttp.WithHeaders(headers))
			}
			exporter, err = otlptracehttp.New(ctx, httpOpts...)
		} else {
			// Use gRPC exporter
			grpcOpts := []otlptracegrpc.Option{
				otlptracegrpc.WithEndpoint(endpoint),
			}
			if secure {
				grpcOpts = append(grpcOpts, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(&tls.Config{})))
			} else {
				grpcOpts = append(grpcOpts, otlptracegrpc.WithInsecure())
			}
			if len(headers) > 0 {
				grpcOpts = append(grpcOpts, otlptracegrpc.WithHeaders(headers))
			}
			exporter, err = otlptracegrpc.New(ctx, grpcOpts...)
		}
	} else {
		// No OTLP exporter - use no-op tracer to avoid nil panics
		t.tracer = NewTracer(otel.GetTracerProvider().Tracer(t.serviceName))
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create tracer provider
	t.tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(t.resource),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// Set global tracer provider
	otel.SetTracerProvider(t.tracerProvider)

	// Add shutdown function
	t.shutdownFuncs = append(t.shutdownFuncs, t.tracerProvider.Shutdown)

	// Create tracer wrapper
	t.tracer = NewTracer(t.tracerProvider.Tracer(t.serviceName))

	return nil
}

// initMetrics initializes the OpenTelemetry metrics pipeline
func (t *Telemetry) initMetrics(ctx context.Context, opts options.OpenTelemetryOptions) error {
	var exporter sdkmetric.Exporter
	var err error

	if opts.OTLP.Enabled {
		endpoint, headers, secure := resolveOTLPConfig(&opts.OTLP)
		if opts.OTLP.UseHTTP {
			// Use HTTP exporter
			httpOpts := []otlpmetrichttp.Option{
				otlpmetrichttp.WithEndpoint(endpoint),
			}
			if !secure {
				httpOpts = append(httpOpts, otlpmetrichttp.WithInsecure())
			}
			if len(headers) > 0 {
				httpOpts = append(httpOpts, otlpmetrichttp.WithHeaders(headers))
			}
			exporter, err = otlpmetrichttp.New(ctx, httpOpts...)
		} else {
			// Use gRPC exporter
			grpcOpts := []otlpmetricgrpc.Option{
				otlpmetricgrpc.WithEndpoint(endpoint),
			}
			if secure {
				grpcOpts = append(grpcOpts, otlpmetricgrpc.WithTLSCredentials(credentials.NewTLS(&tls.Config{})))
			} else {
				grpcOpts = append(grpcOpts, otlpmetricgrpc.WithInsecure())
			}
			if len(headers) > 0 {
				grpcOpts = append(grpcOpts, otlpmetricgrpc.WithHeaders(headers))
			}
			exporter, err = otlpmetricgrpc.New(ctx, grpcOpts...)
		}
	} else {
		// No OTLP exporter - use no-op meter to avoid nil panics
		t.Metrics = NewMetrics(otel.GetMeterProvider().Meter(t.serviceName))
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to create metric exporter: %w", err)
	}

	// Create meter provider
	t.meterProvider = sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter,
			sdkmetric.WithInterval(time.Duration(opts.Metrics.ExportIntervalSeconds)*time.Second),
		)),
		sdkmetric.WithResource(t.resource),
	)

	// Set global meter provider
	otel.SetMeterProvider(t.meterProvider)

	// Add shutdown function
	t.shutdownFuncs = append(t.shutdownFuncs, t.meterProvider.Shutdown)

	// Create metrics wrapper
	t.Metrics = NewMetrics(t.meterProvider.Meter(t.serviceName))

	return nil
}

// initLogging initializes the OpenTelemetry logging pipeline
func (t *Telemetry) initLogging(ctx context.Context, opts options.OpenTelemetryOptions) error {
	var processors []sdklog.Processor

	// Only add OTLP exporter if enabled (for Loki/remote logging)
	// Console output is handled by the charmbracelet logger
	if opts.OTLP.Enabled {
		endpoint, headers, secure := resolveOTLPConfig(&opts.OTLP)
		var otlpExporter sdklog.Exporter
		var err error
		if opts.OTLP.UseHTTP {
			// Use HTTP exporter
			httpOpts := []otlploghttp.Option{
				otlploghttp.WithEndpoint(endpoint),
			}
			if !secure {
				httpOpts = append(httpOpts, otlploghttp.WithInsecure())
			}
			if len(headers) > 0 {
				httpOpts = append(httpOpts, otlploghttp.WithHeaders(headers))
			}
			otlpExporter, err = otlploghttp.New(ctx, httpOpts...)
		} else {
			// Use gRPC exporter
			grpcOpts := []otlploggrpc.Option{
				otlploggrpc.WithEndpoint(endpoint),
			}
			if secure {
				grpcOpts = append(grpcOpts, otlploggrpc.WithTLSCredentials(credentials.NewTLS(&tls.Config{})))
			} else {
				grpcOpts = append(grpcOpts, otlploggrpc.WithInsecure())
			}
			if len(headers) > 0 {
				grpcOpts = append(grpcOpts, otlploggrpc.WithHeaders(headers))
			}
			otlpExporter, err = otlploggrpc.New(ctx, grpcOpts...)
		}
		if err != nil {
			return fmt.Errorf("failed to create OTLP log exporter: %w", err)
		}
		processors = append(processors, sdklog.NewBatchProcessor(otlpExporter))
	}

	// Create logger provider with all processors
	processorOptions := make([]sdklog.LoggerProviderOption, 0, len(processors)+1)
	for _, processor := range processors {
		processorOptions = append(processorOptions, sdklog.WithProcessor(processor))
	}
	processorOptions = append(processorOptions, sdklog.WithResource(t.resource))

	t.loggerProvider = sdklog.NewLoggerProvider(processorOptions...)

	// Set global logger provider
	global.SetLoggerProvider(t.loggerProvider)

	// Add shutdown function
	t.shutdownFuncs = append(t.shutdownFuncs, t.loggerProvider.Shutdown)

	// Create logger wrapper
	t.Logger = NewLogger(t.loggerProvider.Logger(t.serviceName), opts.Logging)

	return nil
}

// GetLogger returns the underlying OpenTelemetry logger
func (t *Telemetry) GetLogger() log.Logger {
	if t.loggerProvider != nil {
		return t.loggerProvider.Logger(t.serviceName)
	}
	return nil
}

// GetMetrics returns the metrics wrapper
func (t *Telemetry) GetMetrics() *Metrics {
	return t.Metrics
}

// GetTracer returns the tracer wrapper
func (t *Telemetry) GetTracer() *Tracer {
	return t.tracer
}

// Shutdown gracefully shuts down all telemetry providers
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs []error

	// Force flush tracer provider first to ensure all spans are exported
	if t.tracerProvider != nil {
		if err := t.tracerProvider.ForceFlush(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracer force flush: %w", err))
		}
	}

	// Force flush meter provider
	if t.meterProvider != nil {
		if err := t.meterProvider.ForceFlush(ctx); err != nil {
			errs = append(errs, fmt.Errorf("meter force flush: %w", err))
		}
	}

	// Force flush logger provider
	if t.loggerProvider != nil {
		if err := t.loggerProvider.ForceFlush(ctx); err != nil {
			errs = append(errs, fmt.Errorf("logger force flush: %w", err))
		}
	}

	// Now shutdown all providers
	for _, fn := range t.shutdownFuncs {
		if err := fn(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	return nil
}
