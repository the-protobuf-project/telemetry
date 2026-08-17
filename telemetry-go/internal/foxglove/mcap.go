package foxglove

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/foxglove/mcap/go/mcap"
	"github.com/the-protobuf-project/telemetry/telemetry-go/options"
)

// UnifiedMcapWriter manages a single MCAP file with multiple schemas and channels
// for both logging and metrics
type UnifiedMcapWriter struct {
	writer   *mcap.Writer
	file     *os.File
	mu       sync.Mutex
	filePath string
	closed   bool

	// Schema management
	registry     *SchemaRegistry
	schemaIDs    map[string]uint16 // schema name -> schema ID
	nextSchemaID uint16

	// Channel tracking
	channels    map[string]uint16 // topic -> channel ID
	nextChannel uint16
}

// NewUnifiedMcapWriter creates a unified MCAP writer for logs and metrics
func NewUnifiedMcapWriter(serviceOpts options.ServiceOptions, foxgloveOpts options.FoxgloveOptions) (*UnifiedMcapWriter, error) {
	if foxgloveOpts.McapPath == "" {
		return nil, fmt.Errorf("MCAP file path not specified")
	}

	// Create directory if needed
	dir := filepath.Dir(foxgloveOpts.McapPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Create MCAP file
	file, err := os.Create(foxgloveOpts.McapPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCAP file: %w", err)
	}

	// Create MCAP writer
	writer, err := mcap.NewWriter(file, &mcap.WriterOptions{
		Chunked:     true,
		ChunkSize:   1024 * 1024,
		Compression: mcap.CompressionZSTD,
		IncludeCRC:  true,
	})
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to create MCAP writer: %w", err)
	}

	// Write header
	if err := writer.WriteHeader(&mcap.Header{
		Profile: serviceOpts.Name,
		Library: "github.com/the-protobuf-project/",
	}); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to write header: %w", err)
	}

	unified := &UnifiedMcapWriter{
		writer:       writer,
		file:         file,
		filePath:     foxgloveOpts.McapPath,
		registry:     NewSchemaRegistry(),
		schemaIDs:    make(map[string]uint16),
		channels:     make(map[string]uint16),
		nextSchemaID: 1,
		nextChannel:  1,
	}

	// Register built-in schemas
	if err := unified.registerBuiltInSchemas(); err != nil {
		_ = file.Close()
		return nil, err
	}

	return unified, nil
}

// registerBuiltInSchemas registers the built-in schemas (foxglove.Log and mahcanirobotics.metric)
func (u *UnifiedMcapWriter) registerBuiltInSchemas() error {
	for _, schemaName := range []string{"foxglove.Log", "mahcanirobotics.metric"} {
		if err := u.RegisterSchema(schemaName); err != nil {
			return err
		}
	}
	return nil
}

// RegisterSchema registers a schema from the registry to the MCAP file
func (u *UnifiedMcapWriter) RegisterSchema(schemaName string) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	// Check if already registered
	if _, exists := u.schemaIDs[schemaName]; exists {
		return nil
	}

	// Get schema from registry
	schemaData, exists := u.registry.Get(schemaName)
	if !exists {
		return fmt.Errorf("schema %s not found in registry", schemaName)
	}

	// Assign schema ID and write to MCAP
	schemaID := u.nextSchemaID
	if err := u.writer.WriteSchema(&mcap.Schema{
		ID:       schemaID,
		Name:     schemaName,
		Encoding: "jsonschema",
		Data:     []byte(schemaData),
	}); err != nil {
		return fmt.Errorf("failed to write schema %s: %w", schemaName, err)
	}

	u.schemaIDs[schemaName] = schemaID
	u.nextSchemaID++
	return nil
}

// AddCustomSchema adds a custom schema to the registry and registers it to the MCAP file
func (u *UnifiedMcapWriter) AddCustomSchema(name, schema string) error {
	u.registry.Register(name, schema)
	return u.RegisterSchema(name)
}

// CreateLogChannel creates a channel for logs using the foxglove.Log schema
func (u *UnifiedMcapWriter) CreateLogChannel(topic string, metadata map[string]string) (uint16, error) {
	return u.CreateChannel(topic, "foxglove.Log", metadata)
}

// CreateMetricChannel creates a channel for a specific metric using the mahcanirobotics.metric schema
func (u *UnifiedMcapWriter) CreateMetricChannel(topic string, metadata map[string]string) (uint16, error) {
	return u.CreateChannel(topic, "mahcanirobotics.metric", metadata)
}

// CreateChannel creates a channel with a specific schema
func (u *UnifiedMcapWriter) CreateChannel(topic, schemaName string, metadata map[string]string) (uint16, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	// Check if channel already exists
	if channelID, exists := u.channels[topic]; exists {
		return channelID, nil
	}

	// Get schema ID
	schemaID, exists := u.schemaIDs[schemaName]
	if !exists {
		return 0, fmt.Errorf("schema %s not registered", schemaName)
	}

	// Create channel
	channelID := u.nextChannel
	if err := u.writer.WriteChannel(&mcap.Channel{
		ID:              channelID,
		SchemaID:        schemaID,
		Topic:           topic,
		MessageEncoding: "json",
		Metadata:        metadata,
	}); err != nil {
		return 0, fmt.Errorf("failed to create channel: %w", err)
	}

	u.channels[topic] = channelID
	u.nextChannel++
	return channelID, nil
}

// WriteMessage writes a message to a specific channel
func (u *UnifiedMcapWriter) WriteMessage(channelID uint16, data []byte, logTime, publishTime uint64) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.writer.WriteMessage(&mcap.Message{
		ChannelID:   channelID,
		Sequence:    0,
		LogTime:     logTime,
		PublishTime: publishTime,
		Data:        data,
	})
}

// Close closes the MCAP writer
func (u *UnifiedMcapWriter) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.closed {
		return nil
	}

	if err := u.writer.Close(); err != nil {
		_ = u.file.Close()
		return fmt.Errorf("failed to close MCAP writer: %w", err)
	}

	if err := u.file.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}

	u.closed = true
	return nil
}

// IsClosed returns whether the writer is closed
func (u *UnifiedMcapWriter) IsClosed() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.closed
}

// GetFilePath returns the path to the MCAP file
func (u *UnifiedMcapWriter) GetFilePath() string {
	return u.filePath
}
