package loaders

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// DefaultConfigFilePath is the default path for the configuration file.
const DefaultConfigFilePath = "config.yaml"

// Loader is a generic struct that loads configuration of type T.
// It handles reading the config file, supporting various file formats,
// and applies an environment variable prefix for nested keys.
// It uses Viper for configuration management and supports multiple file extensions.
type Loader[T any] struct {
	cfg            T            // The loaded configuration of type T.
	envPrefix      string       // Prefix for environment variable keys.
	configFilePath string       // Path to the configuration file.
	configFileExt  string       // Extension of the configuration file (e.g., yaml, json).
	viper          *viper.Viper // Viper instance used for loading configuration.
	supportedExts  []string     // List of supported file extensions (e.g., yaml, json, env).
}

// NewLoader returns a loader instance of type T using the default configuration
// provided by the defaults function. The envPrefix is used as a prefix for environment
// variable keys, serving as an alias for nested fields.
// This prefix is required for .env files, which do not support dots in key names.
// Supported file extensions include: yaml, yml, json, dotenv, and env.
func NewLoader[T any](defaults func() T, envPrefix string) *Loader[T] {
	return &Loader[T]{
		cfg:            defaults(),
		envPrefix:      envPrefix,
		configFilePath: DefaultConfigFilePath,
		viper:          viper.New(),
		supportedExts:  []string{"yaml", "yml", "json", "dotenv", "env"},
	}
}

// SetConfigFilePath sets the configuration file path to the given value.
// It returns an error if the file extension is not supported by the loader.
func (l *Loader[T]) SetConfigFilePath(path string) error {
	ext := filepath.Ext(path)
	if len(ext) > 1 {
		ext = ext[1:]
	}

	if !slices.Contains(l.supportedExts, ext) {
		return fmt.Errorf("unsupported config file extension: %s", ext)
	}

	l.configFilePath = path
	l.configFileExt = ext
	return nil
}

// Load loads the configuration from the environment and the config file.
// NOTE: The priority of the values is as follows:
// 1. Environment variables
// 2. Config file (supported types: "yaml", "yml", "json", "env", "dotenv")
// 3. Default values
//
// NOTE: The config file is optional.
// NOTE: For multilevel nested structs, the keys in the ENV variables should be separated by underscores.
// e.g. for the nesting:
//
//	{
//		"a": {
//			"b_with_long_name": {
//				"c": "value"
//			}
//		}
//	}
//
// the ENV variable should be named as: <ENVPREFIX>_A_B_WITH_LONG_NAME_C
// the ENVPREFIX is the prefix that is passed to the NewLoader function.
func (l *Loader[T]) Load() (T, error) {
	if err := l.setViperDefaults(); err != nil {
		return l.cfg, err
	}

	l.prepareViper()

	if err := l.loadFromFile(); err != nil {
		return l.cfg, err
	}

	if err := l.viperToCfg(); err != nil {
		return l.cfg, err
	}

	return l.cfg, nil
}

func (l *Loader[T]) setViperDefaults() error {
	defaultsMap := make(map[string]any)
	if err := mapstructure.Decode(l.cfg, &defaultsMap); err != nil {
		err = fmt.Errorf("error occurred while setting defaults: %w", err)
		return err
	}

	for k, v := range defaultsMap {
		l.viper.SetDefault(k, v)
	}

	return nil
}

func (l *Loader[T]) prepareViper() {
	l.viper.SetEnvPrefix(l.envPrefix)
	l.viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	l.viper.AutomaticEnv()
}

func (l *Loader[T]) loadFromFile() error {
	if l.configFilePath == DefaultConfigFilePath {
		_, err := os.Stat(l.configFilePath)
		if os.IsNotExist(err) {
			// Config file not specified. Using defaults
			return nil
		}
	}

	l.viper.SetConfigFile(l.configFilePath)
	if err := l.viper.ReadInConfig(); err != nil {
		return fmt.Errorf("error while reading config file: %w", err)
	}

	if l.configFileExt == "dotenv" || l.configFileExt == "env" {
		// Register aliases for nested keys. Necessary for .env files to avoid "." in the key names (underscores are used instead)
		prefix := l.envPrefix
		if prefix != "" {
			prefix += "_"
		}
		for _, key := range l.viper.AllKeys() {
			l.viper.RegisterAlias(prefix+strings.ReplaceAll(key, ".", "_"), key)
		}
	}

	return nil
}

func (l *Loader[T]) viperToCfg() error {
	if err := l.viper.Unmarshal(&l.cfg); err != nil {
		return fmt.Errorf("error while unmarshalling config from viper: %w", err)
	}
	return nil
}
