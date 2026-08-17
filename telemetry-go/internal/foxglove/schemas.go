package foxglove

// SchemaRegistry manages MCAP schemas for different data types
type SchemaRegistry struct {
	schemas map[string]string
}

// NewSchemaRegistry creates a new schema registry with built-in schemas
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{
		schemas: map[string]string{
			"foxglove.Log":           foxgloveLogSchema,
			"mahcanirobotics.metric": shokkiMetricSchema,
			"foxglove.Plot":          foxglovePlotSchema,
		},
	}
}

// Register adds a new schema to the registry
func (r *SchemaRegistry) Register(name, schema string) {
	r.schemas[name] = schema
}

// Get retrieves a schema by name
func (r *SchemaRegistry) Get(name string) (string, bool) {
	schema, exists := r.schemas[name]
	return schema, exists
}

// List returns all registered schema names
func (r *SchemaRegistry) List() []string {
	names := make([]string, 0, len(r.schemas))
	for name := range r.schemas {
		names = append(names, name)
	}
	return names
}

// foxgloveLogSchema defines the Foxglove Log schema
// https://github.com/foxglove/schemas/blob/main/schemas/jsonschema/Log.json
const foxgloveLogSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "Log",
  "description": "A log message with timestamp, level, and structured data",
  "type": "object",
  "properties": {
    "timestamp": {
      "type": "object",
      "properties": {
        "sec": {"type": "integer"},
        "nsec": {"type": "integer"}
      },
      "required": ["sec", "nsec"]
    },
    "level": {"type": "integer", "description": "Log level (1=DEBUG, 2=INFO, 3=WARN, 4=ERROR, 5=FATAL)"},
    "message": {"type": "string", "description": "Log message"},
    "name": {"type": "string", "description": "Logger name"},
    "file": {"type": "string", "description": "Source file"},
    "line": {"type": "integer", "minimum": 0, "description": "Line number"},
    "service_version": {"type": "string", "description": "Service version"},
    "service_environment": {"type": "string", "description": "Service environment"},
    "data": {"type": "object", "description": "Additional structured data"}
  },
  "required": ["timestamp", "level", "message", "name", "file", "line", "service_version", "service_environment"]
}`

// shokkiMetricSchema defines the Shokki Engineering metric schema for time-series data
// This schema is optimized for Foxglove's Plot panel - the 'value' field
// will be automatically plotted as a time series
const shokkiMetricSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "mahcanirobotics.metric",
  "description": "A metric value with timestamp for time-series visualization",
  "type": "object",
  "properties": {
    "timestamp": {
      "type": "object",
      "title": "time",
      "properties": {
        "sec": {"type": "integer", "minimum": 0},
        "nsec": {"type": "integer", "minimum": 0, "maximum": 999999999}
      },
      "required": ["sec", "nsec"],
      "description": "Timestamp of the metric sample"
    },
    "name": {"type": "string", "description": "Metric name"},
    "value": {"type": "number", "description": "Metric value (plotted on Y-axis)"}
  },
  "required": ["timestamp", "name", "value"]
}`

// foxglovePlotSchema defines the Foxglove Plot schema for explicit plotting
// Note: Our telemetry.Metric schema already works with Plot panel, but this is
// provided for compatibility with Foxglove's official Plot schema
// https://github.com/foxglove/schemas/blob/main/schemas/jsonschema/Plot.json
const foxglovePlotSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "foxglove.Plot",
  "description": "A series of values for plotting",
  "type": "object",
  "properties": {
    "timestamp": {
      "type": "object",
      "title": "time",
      "properties": {
        "sec": {"type": "integer", "minimum": 0},
        "nsec": {"type": "integer", "minimum": 0, "maximum": 999999999}
      },
      "required": ["sec", "nsec"]
    },
    "x": {"type": "number", "description": "X-axis value"},
    "y": {"type": "number", "description": "Y-axis value"}
  },
  "required": ["timestamp", "x", "y"]
}`
