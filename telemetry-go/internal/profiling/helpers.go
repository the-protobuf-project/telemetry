package profiling

import (
	"context"
	"fmt"
	"time"
)

// ProfiledFunc wraps a function with profiling and timing
func (p *Profiler) ProfiledFunc(ctx context.Context, operation string, fn func() error) error {
	if !p.enabled {
		return fn()
	}

	start := time.Now()
	var err error

	p.TagWrapper(ctx, map[string]string{
		"operation": operation,
	}, func(ctx context.Context) {
		err = fn()
	})

	duration := time.Since(start)

	// Add error tag if function failed
	if err != nil {
		p.TagWrapper(ctx, map[string]string{
			"operation": operation,
			"status":    "error",
			"duration":  fmt.Sprintf("%dms", duration.Milliseconds()),
		}, func(ctx context.Context) {
			// Just tag the error, don't execute anything
		})
	}

	return err
}

// ProfiledFuncWithTiming wraps a function with profiling and timing, returns duration
func (p *Profiler) ProfiledFuncWithTiming(ctx context.Context, operation string, fn func() error) (time.Duration, error) {
	if !p.enabled {
		start := time.Now()
		err := fn()
		return time.Since(start), err
	}

	start := time.Now()
	var err error

	p.TagWrapper(ctx, map[string]string{
		"operation": operation,
	}, func(ctx context.Context) {
		err = fn()
	})

	duration := time.Since(start)

	// Add timing and status tags
	status := "success"
	if err != nil {
		status = "error"
	}

	p.TagWrapper(ctx, map[string]string{
		"operation": operation,
		"status":    status,
		"duration":  fmt.Sprintf("%dms", duration.Milliseconds()),
	}, func(ctx context.Context) {
		// Just tag the result
	})

	return duration, err
}

// ProfileSection marks a code section with custom tags
func (p *Profiler) ProfileSection(ctx context.Context, section string, tags map[string]string, fn func(context.Context)) {
	if !p.enabled {
		fn(ctx)
		return
	}

	// Merge section name with custom tags
	allTags := make(map[string]string)
	allTags["section"] = section
	for k, v := range tags {
		allTags[k] = v
	}

	p.TagWrapper(ctx, allTags, fn)
}

// ProfileDatabaseQuery profiles database query operations
func (p *Profiler) ProfileDatabaseQuery(ctx context.Context, queryType string, table string, fn func(context.Context) error) error {
	if !p.enabled {
		return fn(ctx)
	}

	var err error
	start := time.Now()

	p.TagWrapper(ctx, map[string]string{
		"operation":  "database_query",
		"query_type": queryType,
		"table":      table,
	}, func(ctx context.Context) {
		err = fn(ctx)
	})

	duration := time.Since(start)

	status := "success"
	if err != nil {
		status = "error"
	}

	// Add performance metrics
	p.TagWrapper(ctx, map[string]string{
		"operation":  "database_query",
		"query_type": queryType,
		"table":      table,
		"status":     status,
		"duration":   fmt.Sprintf("%dms", duration.Milliseconds()),
	}, func(ctx context.Context) {})

	return err
}

// ProfileCacheOperation profiles cache operations
func (p *Profiler) ProfileCacheOperation(ctx context.Context, operation string, key string, fn func(context.Context) error) error {
	if !p.enabled {
		return fn(ctx)
	}

	var err error
	start := time.Now()

	p.TagWrapper(ctx, map[string]string{
		"operation":       "cache_operation",
		"cache_operation": operation,
		"cache_key":       key,
	}, func(ctx context.Context) {
		err = fn(ctx)
	})

	duration := time.Since(start)

	status := "success"
	if err != nil {
		status = "error"
	}

	// Add performance metrics
	p.TagWrapper(ctx, map[string]string{
		"operation":       "cache_operation",
		"cache_operation": operation,
		"status":          status,
		"duration":        fmt.Sprintf("%dms", duration.Milliseconds()),
	}, func(ctx context.Context) {})

	return err
}

// ProfileHTTPRequest profiles HTTP request handling
func (p *Profiler) ProfileHTTPRequest(ctx context.Context, method string, path string, fn func(context.Context) error) error {
	if !p.enabled {
		return fn(ctx)
	}

	var err error
	start := time.Now()

	p.TagWrapper(ctx, map[string]string{
		"operation": "http_request",
		"method":    method,
		"path":      path,
	}, func(ctx context.Context) {
		err = fn(ctx)
	})

	duration := time.Since(start)

	status := "success"
	statusCode := 200
	if err != nil {
		status = "error"
		statusCode = 500
	}

	// Add performance metrics
	p.TagWrapper(ctx, map[string]string{
		"operation":   "http_request",
		"method":      method,
		"path":        path,
		"status":      status,
		"status_code": fmt.Sprintf("%d", statusCode),
		"duration":    fmt.Sprintf("%dms", duration.Milliseconds()),
	}, func(ctx context.Context) {})

	return err
}

// ProfileExternalAPI profiles external API calls
func (p *Profiler) ProfileExternalAPI(ctx context.Context, service string, endpoint string, fn func(context.Context) error) error {
	if !p.enabled {
		return fn(ctx)
	}

	var err error
	start := time.Now()

	p.TagWrapper(ctx, map[string]string{
		"operation": "external_api",
		"service":   service,
		"endpoint":  endpoint,
	}, func(ctx context.Context) {
		err = fn(ctx)
	})

	duration := time.Since(start)

	status := "success"
	if err != nil {
		status = "error"
	}

	// Add performance metrics
	p.TagWrapper(ctx, map[string]string{
		"operation": "external_api",
		"service":   service,
		"endpoint":  endpoint,
		"status":    status,
		"duration":  fmt.Sprintf("%dms", duration.Milliseconds()),
	}, func(ctx context.Context) {})

	return err
}

// ProfileComputation profiles CPU-intensive computations
func (p *Profiler) ProfileComputation(ctx context.Context, computationType string, fn func(context.Context)) {
	if !p.enabled {
		fn(ctx)
		return
	}

	start := time.Now()

	p.TagWrapper(ctx, map[string]string{
		"operation":        "computation",
		"computation_type": computationType,
	}, func(ctx context.Context) {
		fn(ctx)
	})

	duration := time.Since(start)

	// Add timing information
	p.TagWrapper(ctx, map[string]string{
		"operation":        "computation",
		"computation_type": computationType,
		"duration":         fmt.Sprintf("%dms", duration.Milliseconds()),
	}, func(ctx context.Context) {})
}

// ProfileMemoryOperation profiles memory-intensive operations
func (p *Profiler) ProfileMemoryOperation(ctx context.Context, operationType string, sizeBytes int64, fn func(context.Context)) {
	if !p.enabled {
		fn(ctx)
		return
	}

	p.TagWrapper(ctx, map[string]string{
		"operation":      "memory_operation",
		"operation_type": operationType,
		"size_mb":        fmt.Sprintf("%.2f", float64(sizeBytes)/(1024*1024)),
	}, func(ctx context.Context) {
		fn(ctx)
	})
}
