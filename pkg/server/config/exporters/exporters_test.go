package exporters_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/config/exporters"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/config/loaders"
	"github.com/stretchr/testify/require"
)

func TestToEnvFile(t *testing.T) {
	// given:
	configFilePath := fmt.Sprintf("%s/exported_config.env", t.TempDir())

	// when:
	err := exporters.ToEnvFile(NewExporterTestConfig(), configFilePath, "TEST")

	// then:
	require.NoError(t, err)

	data, err := os.ReadFile(configFilePath)
	require.NoError(t, err)

	content := string(data)
	require.Contains(t, content, `TEST_A="default_hello"`)
	require.Contains(t, content, `TEST_B_WITH_LONG_NAME="1"`)
	require.Contains(t, content, `TEST_C_SUB_CONFIG_D_NESTED_FIELD="default_world"`)
}

func TestToEnvFile_WithEmptyPrefix(t *testing.T) {
	// given:
	configFilePath := fmt.Sprintf("%s/exported_config.env", t.TempDir())

	// when:
	err := exporters.ToEnvFile(NewExporterTestConfig(), configFilePath, "")

	// then:
	require.NoError(t, err)

	data, err := os.ReadFile(configFilePath)
	require.NoError(t, err)

	content := string(data)
	require.Contains(t, content, `A="default_hello"`)
	require.Contains(t, content, `B_WITH_LONG_NAME="1"`)
	require.NotContains(t, content, `_C_SUB_CONFIG_D_NESTED_FIELD="default_world"`)
}

func TestToJSONFile(t *testing.T) {
	// given:
	configFilePath := fmt.Sprintf("%s/exported_config.json", t.TempDir())

	// when:
	err := exporters.ToJSONFile(NewExporterTestConfig(), configFilePath)

	// then:
	require.NoError(t, err)

	data, err := os.ReadFile(configFilePath)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	expected := map[string]any{
		"a":                "default_hello",
		"b_with_long_name": float64(1),
		"c_sub_config": map[string]any{
			"d_nested_field": "default_world",
		},
	}

	require.Equal(t, expected, result)
}

func TestToYAMLFile(t *testing.T) {
	// given:
	configFilePath := fmt.Sprintf("%s/exported_config.yaml", t.TempDir())

	// when:
	err := exporters.ToYAMLFile(NewExporterTestConfig(), configFilePath)

	// then:
	require.NoError(t, err)

	yamlFile, err := os.ReadFile(configFilePath)
	require.NoError(t, err)

	require.Contains(t, string(yamlFile), "a: default_hello")
	require.Contains(t, string(yamlFile), "b_with_long_name: 1")
	require.Contains(t, string(yamlFile), "d_nested_field: default_world")
}

func TestExportToYAML_ShouldWriteFile_WhenConfigIsValid(t *testing.T) {
	// given:
	l := loaders.NewLoader(NewExporterTestConfig, "OVERLAY")
	_, err := l.Load()
	require.NoError(t, err)

	// when:
	tmpFile := filepath.Join(t.TempDir(), "config.yaml")
	err = exporters.ToYAMLFile(NewExporterTestConfig(), tmpFile)

	// then:
	require.NoError(t, err)

	data, err := os.ReadFile(tmpFile)
	require.NoError(t, err)

	require.Contains(t, string(data), "a: default_hello")
	require.Contains(t, string(data), "d_nested_field: default_world")
}

type ExporterTestConfig struct {
	A string                `mapstructure:"a"`
	B int                   `mapstructure:"b_with_long_name"`
	C ExporterTestSubConfig `mapstructure:"c_sub_config"`
}

type ExporterTestSubConfig struct {
	D string `mapstructure:"d_nested_field"`
}

func NewExporterTestConfig() ExporterTestConfig {
	return ExporterTestConfig{
		A: "default_hello",
		B: 1,
		C: ExporterTestSubConfig{
			D: "default_world",
		},
	}
}
