package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFile(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		content := `
target_prometheus: "http://localhost:9090"
mappings:
  - direction: "query"
    rules:
      - source_label: "host"
        target_label: "instance"
  - direction: "result"
    rules:
      - source_label: "instance"
        target_label: "host"
`
		path := writeTemp(t, content)
		cfg, err := LoadFromFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.TargetPrometheus != "http://localhost:9090" {
			t.Errorf("expected target_prometheus=http://localhost:9090, got %s", cfg.TargetPrometheus)
		}
		if len(cfg.Mappings) != 2 {
			t.Errorf("expected 2 mappings, got %d", len(cfg.Mappings))
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := LoadFromFile("/nonexistent/path.yaml")
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		path := writeTemp(t, "{{invalid yaml")
		_, err := LoadFromFile(path)
		if err == nil {
			t.Fatal("expected error for invalid yaml")
		}
	})

	t.Run("validation failure", func(t *testing.T) {
		content := `
target_prometheus: ""
mappings: []
`
		path := writeTemp(t, content)
		_, err := LoadFromFile(path)
		if err == nil {
			t.Fatal("expected validation error for empty target_prometheus")
		}
	})
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				TargetPrometheus: "http://localhost:9090",
				Mappings: []Mapping{
					{
						Direction: DirectionQuery,
						Rules: []Rule{
							{SourceLabel: "host", TargetLabel: "instance"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty target_prometheus",
			config: &Config{
				TargetPrometheus: "",
				Mappings: []Mapping{
					{
						Direction: DirectionQuery,
						Rules: []Rule{
							{SourceLabel: "host", TargetLabel: "instance"},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid direction",
			config: &Config{
				TargetPrometheus: "http://localhost:9090",
				Mappings: []Mapping{
					{
						Direction: "invalid",
						Rules: []Rule{
							{SourceLabel: "host", TargetLabel: "instance"},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "empty rules",
			config: &Config{
				TargetPrometheus: "http://localhost:9090",
				Mappings: []Mapping{
					{
						Direction: DirectionQuery,
						Rules:     []Rule{},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "empty source_label",
			config: &Config{
				TargetPrometheus: "http://localhost:9090",
				Mappings: []Mapping{
					{
						Direction: DirectionQuery,
						Rules: []Rule{
							{SourceLabel: "", TargetLabel: "instance"},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "empty target_label",
			config: &Config{
				TargetPrometheus: "http://localhost:9090",
				Mappings: []Mapping{
					{
						Direction: DirectionQuery,
						Rules: []Rule{
							{SourceLabel: "host", TargetLabel: ""},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "direction both",
			config: &Config{
				TargetPrometheus: "http://localhost:9090",
				Mappings: []Mapping{
					{
						Direction: DirectionBoth,
						Rules: []Rule{
							{SourceLabel: "host", TargetLabel: "instance"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "direction result",
			config: &Config{
				TargetPrometheus: "http://localhost:9090",
				Mappings: []Mapping{
					{
						Direction: DirectionResult,
						Rules: []Rule{
							{SourceLabel: "instance", TargetLabel: "host"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "no mappings is valid",
			config: &Config{
				TargetPrometheus: "http://localhost:9090",
				Mappings:         nil,
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if tc.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestGetRules(t *testing.T) {
	cfg := &Config{
		TargetPrometheus: "http://localhost:9090",
		Mappings: []Mapping{
			{
				Direction: DirectionQuery,
				Rules: []Rule{
					{SourceLabel: "host", TargetLabel: "instance"},
				},
			},
			{
				Direction: DirectionResult,
				Rules: []Rule{
					{SourceLabel: "instance", TargetLabel: "host"},
				},
			},
			{
				Direction: DirectionBoth,
				Rules: []Rule{
					{SourceLabel: "env", TargetLabel: "environment"},
				},
			},
		},
	}

	t.Run("query rules", func(t *testing.T) {
		rules := cfg.GetRules(DirectionQuery)
		// Should include query + both = 2 rules
		if len(rules) != 2 {
			t.Errorf("expected 2 query rules, got %d", len(rules))
		}
	})

	t.Run("result rules", func(t *testing.T) {
		rules := cfg.GetRules(DirectionResult)
		// Should include result + both = 2 rules
		if len(rules) != 2 {
			t.Errorf("expected 2 result rules, got %d", len(rules))
		}
	})

	t.Run("both rules", func(t *testing.T) {
		rules := cfg.GetRules(DirectionBoth)
		// Only "both" direction matches exactly
		if len(rules) != 1 {
			t.Errorf("expected 1 both rule, got %d", len(rules))
		}
	})

	t.Run("no matching rules", func(t *testing.T) {
		emptyCfg := &Config{
			TargetPrometheus: "http://localhost:9090",
			Mappings:         nil,
		}
		rules := emptyCfg.GetRules(DirectionQuery)
		if len(rules) != 0 {
			t.Errorf("expected 0 rules, got %d", len(rules))
		}
	})
}

func TestGetQueryRules(t *testing.T) {
	cfg := &Config{
		TargetPrometheus: "http://localhost:9090",
		Mappings: []Mapping{
			{
				Direction: DirectionQuery,
				Rules: []Rule{
					{SourceLabel: "host", TargetLabel: "instance"},
				},
			},
			{
				Direction: DirectionResult,
				Rules: []Rule{
					{SourceLabel: "instance", TargetLabel: "host"},
				},
			},
		},
	}

	rules := cfg.GetQueryRules()
	if len(rules) != 1 {
		t.Errorf("expected 1 query rule, got %d", len(rules))
	}
	if rules[0].SourceLabel != "host" {
		t.Errorf("expected source_label=host, got %s", rules[0].SourceLabel)
	}
}

func TestGetResultRules(t *testing.T) {
	cfg := &Config{
		TargetPrometheus: "http://localhost:9090",
		Mappings: []Mapping{
			{
				Direction: DirectionQuery,
				Rules: []Rule{
					{SourceLabel: "host", TargetLabel: "instance"},
				},
			},
			{
				Direction: DirectionResult,
				Rules: []Rule{
					{SourceLabel: "instance", TargetLabel: "host"},
				},
			},
		},
	}

	rules := cfg.GetResultRules()
	if len(rules) != 1 {
		t.Errorf("expected 1 result rule, got %d", len(rules))
	}
	if rules[0].SourceLabel != "instance" {
		t.Errorf("expected source_label=instance, got %s", rules[0].SourceLabel)
	}
}

func TestGetTargetPrometheus(t *testing.T) {
	cfg := &Config{
		TargetPrometheus: "http://my-prom:9090",
	}
	if got := cfg.GetTargetPrometheus(); got != "http://my-prom:9090" {
		t.Errorf("expected http://my-prom:9090, got %s", got)
	}
}

// writeTemp creates a temporary file with the given content and returns the path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
