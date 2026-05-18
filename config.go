package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const configPath = "larakube.config.json"

type Config struct {
	LaravelNamespaces []string   `json:"laravelNamespaces"`
	Features          Features   `json:"features"`
	Logs              LogsConfig `json:"logs"`
}

type Features struct {
	Horizon bool `json:"horizon"`
}

type LogsConfig struct {
	Source    string `json:"source"`
	Path      string `json:"path"`
	Tail      int    `json:"tail"`
	Container string `json:"container"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}

	cfg.normalize()
	return cfg, nil
}

func WriteConfig(path string, cfg Config) error {
	cfg.normalize()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config %s: %w", path, err)
	}

	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}

	return nil
}

func DefaultConfig() Config {
	var cfg Config
	cfg.normalize()
	return cfg
}

func (c *Config) normalize() {
	seen := map[string]bool{}
	filtered := make([]string, 0, len(c.LaravelNamespaces))
	for _, namespace := range c.LaravelNamespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			continue
		}
		if seen[namespace] {
			continue
		}
		seen[namespace] = true
		filtered = append(filtered, namespace)
	}
	if len(filtered) == 0 {
		c.LaravelNamespaces = nil
	} else {
		c.LaravelNamespaces = filtered
	}

	if c.Logs.Source == "" {
		c.Logs.Source = "stdout"
	}
	if c.Logs.Tail <= 0 {
		c.Logs.Tail = 100
	}
	if c.Logs.Path == "" {
		c.Logs.Path = "storage/logs/laravel.log"
	}
}

func (c Config) IsLaravelNamespace(namespace string) bool {
	for _, candidate := range c.LaravelNamespaces {
		if candidate == namespace {
			return true
		}
	}
	return false
}

func (c Config) LaravelNamespaceNames() []string {
	if len(c.LaravelNamespaces) == 0 {
		return nil
	}

	namespaces := make([]string, len(c.LaravelNamespaces))
	copy(namespaces, c.LaravelNamespaces)
	return namespaces
}

func (c Config) FeatureEnabled(name string) bool {
	switch name {
	case "horizon":
		return c.Features.Horizon
	default:
		return true
	}
}
