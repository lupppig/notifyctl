package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigFileName = ".notifyctl.yaml"
	DefaultServerAddr     = "localhost:50051"
)

type Config struct {
	ServerAddr string `yaml:"server_addr"`
	ServiceID  string `yaml:"service_id"`
	APIKey     string `yaml:"api_key"`
}

func DefaultConfig() *Config {
	return &Config{
		ServerAddr: DefaultServerAddr,
	}
}

func (c *Config) Validate() error {
	if c.ServerAddr == "" {
		return fmt.Errorf("server_addr is required")
	}
	return nil
}

func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get home directory: %w", err)
		}
		path = filepath.Join(home, DefaultConfigFileName)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", path, err)
	}

	if addr := os.Getenv("NOTIFYCTL_SERVER_ADDR"); addr != "" {
		cfg.ServerAddr = addr
	}
	if id := os.Getenv("NOTIFYCTL_SERVICE_ID"); id != "" {
		cfg.ServiceID = id
	}
	if key := os.Getenv("NOTIFYCTL_API_KEY"); key != "" {
		cfg.APIKey = key
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}
