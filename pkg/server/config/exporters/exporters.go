// Package exporters provides utilities for exporting configuration to various file formats.
package exporters

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mitchellh/mapstructure"
	"gopkg.in/yaml.v3"
)

const ownerReadWriteAccess = 0o600

// ErrConfigEmptyOrUnsupported is returned when the configuration appears empty or unsupported.
var ErrConfigEmptyOrUnsupported = errors.New("config appears empty or unsupported, nothing to write")

// ToEnvFile writes the configuration to an environment file at the specified path.
// It formats the configuration as key-value pairs and writes them to the file.
// Returns an error if decoding the config, flattening, or file writing fails.
func ToEnvFile(cfg any, filename, envPrefix string) error {
	var m map[string]any
	if err := mapstructure.Decode(cfg, &m); err != nil {
		return fmt.Errorf("failed to decode config to map: %w", err)
	}
	if len(m) == 0 {
		return ErrConfigEmptyOrUnsupported
	}

	flat := make(map[string]string)
	flattenMap(strings.ToUpper(envPrefix), m, flat)

	lines := make([]string, 0, len(flat))
	for k, v := range flat {
		lines = append(lines, fmt.Sprintf(`%s="%s"`, k, v))
	}
	sort.Strings(lines)

	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filename, []byte(content), ownerReadWriteAccess); err != nil {
		return fmt.Errorf("failed to write env to file: %w", err)
	}
	return nil
}

// ToYAMLFile writes the configuration to a YAML file at the specified path.
// Returns an error if decoding the config, marshaling to YAML, or file writing fails.
func ToYAMLFile(config any, filename string) error {
	var m map[string]any
	err := mapstructure.Decode(config, &m)
	if err != nil {
		return fmt.Errorf("failed to decode config to map: %w", err)
	}
	if len(m) == 0 {
		return ErrConfigEmptyOrUnsupported
	}

	bb, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshal map to yaml: %w", err)
	}
	err = os.WriteFile(filename, bb, ownerReadWriteAccess)
	if err != nil {
		return fmt.Errorf("failed to write yaml to file: %w", err)
	}
	return nil
}

// ToJSONFile writes the configuration to a JSON file at the specified path.
// Returns an error if decoding the config, marshaling to JSON, or file writing fails.
func ToJSONFile(cfg any, filename string) error {
	var m map[string]any
	err := mapstructure.Decode(cfg, &m)
	if err != nil {
		return fmt.Errorf("failed to decode config to map: %w", err)
	}
	if len(m) == 0 {
		return ErrConfigEmptyOrUnsupported
	}

	bb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal map to json: %w", err)
	}
	err = os.WriteFile(filename, bb, ownerReadWriteAccess)
	if err != nil {
		return fmt.Errorf("failed to write json to file: %w", err)
	}
	return nil
}

func flattenMap(prefix string, input map[string]any, out map[string]string) {
	for k, v := range input {
		key := strings.ToUpper(k)
		if prefix != "" {
			key = prefix + "_" + key
		}

		switch val := v.(type) {
		case map[string]any:
			flattenMap(key, val, out)
		default:
			out[key] = fmt.Sprintf("%v", val)
		}
	}
}
