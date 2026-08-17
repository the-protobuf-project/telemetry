package options

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

var k = koanf.New(".")

// unmarshalConf configures koanf to use json struct tags
var unmarshalConf = koanf.UnmarshalConf{
	Tag: "json",
}

// Default config file search paths (in order of priority)
var defaultConfigPaths = []string{
	"telemetry.toml",
	"telemetry.yaml",
	"telemetry.yml",
	"telemetry.json",
	".config/telemetry.toml",
	".config/telemetry.yaml",
	".config/telemetry.yml",
	".config/telemetry.json",
}

// discoverConfigPath finds a config file automatically.
// Priority: TELEMETRY_CONFIG_PATH env var > telemetry.toml > telemetry.yaml > .config/telemetry.toml > etc.
func discoverConfigPath() string {
	// Check TELEMETRY_CONFIG_PATH environment variable first
	if envPath := os.Getenv("TELEMETRY_CONFIG_PATH"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}

	// Search in default locations
	for _, path := range defaultConfigPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// getParser returns the appropriate parser based on file extension.
// Supports: .yaml, .yml, .json, .toml
func getParser(configPath string) (koanf.Parser, error) {
	ext := strings.ToLower(filepath.Ext(configPath))
	switch ext {
	case ".yaml", ".yml":
		return yaml.Parser(), nil
	case ".json":
		return json.Parser(), nil
	case ".toml":
		return toml.Parser(), nil
	default:
		return nil, fmt.Errorf("unsupported config file format: %s (supported: .yaml, .yml, .json, .toml)", ext)
	}
}

// LoadConfig loads configuration from a config file (YAML, JSON, or TOML) and environment variables.
// The file format is auto-detected from the extension.
// Environment variables override file values. Env vars should be prefixed with TELEMETRY_
// and use underscores for nesting (e.g., TELEMETRY_TELEMETRY_OTLP_HOST).
func LoadConfig(configPath string) (*TelemetryOptions, *ServiceOptions, error) {
	// Load from config file if provided
	if configPath != "" {
		parser, err := getParser(configPath)
		if err != nil {
			return nil, nil, err
		}
		if err := k.Load(file.Provider(configPath), parser); err != nil {
			return nil, nil, fmt.Errorf("error loading config file: %w", err)
		}
	}

	// Load environment variables with TELEMETRY_ prefix
	// TELEMETRY_TELEMETRY_OTLP_HOST -> telemetry.otlp.host
	if err := k.Load(env.Provider("TELEMETRY_", ".", func(s string) string {
		return strings.ReplaceAll(
			strings.ToLower(strings.TrimPrefix(s, "TELEMETRY_")),
			"_", ".")
	}), nil); err != nil {
		return nil, nil, fmt.Errorf("error loading env vars: %w", err)
	}

	// Unmarshal into options structs using json tags
	var telemetryOpts TelemetryOptions
	if err := k.UnmarshalWithConf("", &telemetryOpts, unmarshalConf); err != nil {
		return nil, nil, fmt.Errorf("error unmarshalling telemetry options: %w", err)
	}

	var serviceOpts ServiceOptions
	if err := k.UnmarshalWithConf("service", &serviceOpts, unmarshalConf); err != nil {
		return nil, nil, fmt.Errorf("error unmarshalling service options: %w", err)
	}

	return &telemetryOpts, &serviceOpts, nil
}

// LoadConfigWithDefaults loads configuration and merges with defaults.
// If configPath is empty, auto-discovers config from:
//   - TELEMETRY_CONFIG_PATH env var
//   - telemetry.toml, telemetry.yaml, telemetry.json in current directory
//   - .config/telemetry.toml, .config/telemetry.yaml, .config/telemetry.json
//
// Priority: defaults < config file < environment variables
func LoadConfigWithDefaults(configPath string) (*TelemetryOptions, *ServiceOptions, error) {
	// Start with defaults
	telemetryOpts := Default()
	serviceOpts := ServiceOptions{
		Name:        "telemetry-service",
		Version:     "1.0.0",
		Environment: Development,
	}

	// Auto-discover config file if not provided
	if configPath == "" {
		configPath = discoverConfigPath()
	}

	// Load from config file if found
	if configPath != "" {
		parser, err := getParser(configPath)
		if err != nil {
			return nil, nil, err
		}
		if err := k.Load(file.Provider(configPath), parser); err != nil {
			return nil, nil, fmt.Errorf("error loading config file: %w", err)
		}
	}

	// Load environment variables with TELEMETRY_ prefix
	if err := k.Load(env.Provider("TELEMETRY_", ".", func(s string) string {
		return strings.ReplaceAll(
			strings.ToLower(strings.TrimPrefix(s, "TELEMETRY_")),
			"_", ".")
	}), nil); err != nil {
		return nil, nil, fmt.Errorf("error loading env vars: %w", err)
	}

	// Unmarshal and merge with defaults using json tags
	if err := k.UnmarshalWithConf("", &telemetryOpts, unmarshalConf); err != nil {
		return nil, nil, fmt.Errorf("error unmarshalling telemetry options: %w", err)
	}

	if err := k.UnmarshalWithConf("service", &serviceOpts, unmarshalConf); err != nil {
		return nil, nil, fmt.Errorf("error unmarshalling service options: %w", err)
	}

	return &telemetryOpts, &serviceOpts, nil
}

// MustLoadConfig loads configuration and panics on error.
func MustLoadConfig(configPath string) (*TelemetryOptions, *ServiceOptions) {
	telemetryOpts, serviceOpts, err := LoadConfigWithDefaults(configPath)
	if err != nil {
		panic(err)
	}
	return telemetryOpts, serviceOpts
}

// GetString returns a string value from the loaded config by key path.
func GetString(key string) string {
	return k.String(key)
}

// GetInt returns an int value from the loaded config by key path.
func GetInt(key string) int {
	return k.Int(key)
}

// GetBool returns a bool value from the loaded config by key path.
func GetBool(key string) bool {
	return k.Bool(key)
}
