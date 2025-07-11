package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/config/exporters"
)

// Config contains configuration settings for the overlay-engine API and its dependencies.
type Config struct {
	Server server.Config `mapstructure:"server"`
}

// Export writes the configuration to the file at the specified path.
// It formats the file content based on the file extension:
// - JSON for ".json" files
// - Environment variables for ".env" or ".dotenv" files
// - YAML for ".yaml" or ".yml" files
func (c *Config) Export(path string) error {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	var err error
	switch ext {
	case "json":
		err = exporters.ToJSONFile(c, path)
	case "env", "dotenv":
		err = exporters.ToEnvFile(c, path, strings.Replace(c.Server.AppName, " ", "_", -1))
	default: // yaml, yml
		err = exporters.ToYAMLFile(c, path)
	}

	if err != nil {
		return fmt.Errorf("failed to export configuration: %w", err)
	}
	return nil
}

// NewDefault returns a Config with default HTTP server and MongoDB settings.
func NewDefault() Config {
	return Config{
		Server: server.DefaultConfig,
	}
}
