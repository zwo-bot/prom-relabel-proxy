package config

import (
	"fmt"
	"io/ioutil"
	"sync"

	"gopkg.in/yaml.v3"
)

// Direction represents the direction of label mapping (query, result, or both)
type Direction string

const (
	DirectionQuery  Direction = "query"
	DirectionResult Direction = "result"
	DirectionBoth   Direction = "both"
)

// Rule represents a single label mapping rule
type Rule struct {
	SourceLabel string `yaml:"source_label"`
	TargetLabel string `yaml:"target_label"`
}

// Mapping represents a set of rules with a specific direction
type Mapping struct {
	Direction Direction `yaml:"direction"`
	Rules     []Rule    `yaml:"rules"`
}

// Config represents the main configuration structure
type Config struct {
	TargetPrometheus string    `yaml:"target_prometheus"`
	Mappings         []Mapping `yaml:"mappings"`

	mu sync.RWMutex
}

// LoadFromFile loads configuration from a YAML file
func LoadFromFile(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.TargetPrometheus == "" {
		return fmt.Errorf("target_prometheus is required")
	}

	for i, mapping := range c.Mappings {
		if mapping.Direction != DirectionQuery && 
		   mapping.Direction != DirectionResult && 
		   mapping.Direction != DirectionBoth {
			return fmt.Errorf("invalid direction in mapping %d: %s", i, mapping.Direction)
		}

		if len(mapping.Rules) == 0 {
			return fmt.Errorf("no rules defined in mapping %d", i)
		}

		for j, rule := range mapping.Rules {
			if rule.SourceLabel == "" {
				return fmt.Errorf("source_label is required in mapping %d, rule %d", i, j)
			}
			if rule.TargetLabel == "" {
				return fmt.Errorf("target_label is required in mapping %d, rule %d", i, j)
			}
		}
	}

	return nil
}

// GetRules returns rules for a specific direction
func (c *Config) GetRules(direction Direction) []Rule {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var rules []Rule
	for _, mapping := range c.Mappings {
		if mapping.Direction == direction || mapping.Direction == DirectionBoth {
			rules = append(rules, mapping.Rules...)
		}
	}
	return rules
}

// GetQueryRules returns rules for query direction
func (c *Config) GetQueryRules() []Rule {
	return c.GetRules(DirectionQuery)
}

// GetResultRules returns rules for result direction
func (c *Config) GetResultRules() []Rule {
	return c.GetRules(DirectionResult)
}

// GetTargetPrometheus returns the target Prometheus URL
func (c *Config) GetTargetPrometheus() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TargetPrometheus
}
