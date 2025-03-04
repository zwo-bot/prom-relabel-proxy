package rewriter

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"

	"github.com/zwo-bot/prom-relabel-proxy/internal/config"
)

func TestRewriteQuery(t *testing.T) {
	// Create a test configuration
	cfg := &config.Config{
		TargetPrometheus: "http://localhost:9090",
		Mappings: []config.Mapping{
			{
				Direction: config.DirectionQuery,
				Rules: []config.Rule{
					{
						SourceLabel: "instance",
						TargetLabel: "host",
					},
					{
						SourceLabel: "job",
						TargetLabel: "service",
					},
				},
			},
		},
	}

	// Create a rewriter
	rw := New(cfg)

	// Test cases
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple query",
			input:    `up{instance="localhost:9090"}`,
			expected: `up{host="localhost:9090"}`,
		},
		{
			name:     "Multiple labels",
			input:    `up{instance="localhost:9090",job="prometheus"}`,
			expected: `up{host="localhost:9090",service="prometheus"}`,
		},
		{
			name:     "Regex matcher",
			input:    `up{instance=~"localhost.*"}`,
			expected: `up{host=~"localhost.*"}`,
		},
		{
			name:     "Not equal matcher",
			input:    `up{instance!="localhost:9090"}`,
			expected: `up{host!="localhost:9090"}`,
		},
		{
			name:     "Not regex matcher",
			input:    `up{instance!~"localhost.*"}`,
			expected: `up{host!~"localhost.*"}`,
		},
		{
			name:     "No labels",
			input:    `up`,
			expected: `up`,
		},
		{
			name:     "Unmapped labels",
			input:    `up{foo="bar"}`,
			expected: `up{foo="bar"}`,
		},
	}

	// Run tests
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := rw.RewriteQuery(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestRewriteQueryURL(t *testing.T) {
	// Create a test configuration
	cfg := &config.Config{
		TargetPrometheus: "http://localhost:9090",
		Mappings: []config.Mapping{
			{
				Direction: config.DirectionQuery,
				Rules: []config.Rule{
					{
						SourceLabel: "instance",
						TargetLabel: "host",
					},
				},
			},
		},
	}

	// Create a rewriter
	rw := New(cfg)

	// Test cases
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Query parameter",
			input:    `/api/v1/query?query=up{instance="localhost:9090"}`,
			expected: `/api/v1/query?query=up%7Bhost%3D%22localhost%3A9090%22%7D`,
		},
		{
			name:     "Match parameter",
			input:    `/api/v1/series?match[]=up{instance="localhost:9090"}`,
			expected: `/api/v1/series?match%5B%5D=up%7Bhost%3D%22localhost%3A9090%22%7D`,
		},
	}

	// Run tests
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			u, _ := url.Parse("http://localhost:8080" + tc.input)
			result := rw.RewriteQueryURL(u)
			if result.RequestURI() != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result.RequestURI())
			}
		})
	}
}

func TestRewriteResultJSON(t *testing.T) {
	// Create a test configuration
	cfg := &config.Config{
		TargetPrometheus: "http://localhost:9090",
		Mappings: []config.Mapping{
			{
				Direction: config.DirectionResult,
				Rules: []config.Rule{
					{
						SourceLabel: "instance",
						TargetLabel: "host",
					},
					{
						SourceLabel: "job",
						TargetLabel: "service",
					},
				},
			},
		},
	}

	// Create a rewriter
	rw := New(cfg)

	// Test cases
	testCases := []struct {
		name     string
		input    string
		expected map[string]interface{}
	}{
		{
			name:  "Simple JSON",
			input: `{"metric":{"instance":"localhost:9090"}}`,
			expected: map[string]interface{}{
				"metric": map[string]interface{}{
					"host": "localhost:9090",
				},
			},
		},
		{
			name:  "Multiple metrics",
			input: `{"result":[{"metric":{"instance":"localhost:9090"}},{"metric":{"instance":"localhost:9091"}}]}`,
			expected: map[string]interface{}{
				"result": []interface{}{
					map[string]interface{}{
						"metric": map[string]interface{}{
							"host": "localhost:9090",
						},
					},
					map[string]interface{}{
						"metric": map[string]interface{}{
							"host": "localhost:9091",
						},
					},
				},
			},
		},
		{
			name:  "Prometheus API response",
			input: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up","instance":"localhost:9090","job":"prometheus"},"value":[1677758935,"1"]}]}}`,
			expected: map[string]interface{}{
				"status": "success",
				"data": map[string]interface{}{
					"resultType": "vector",
					"result": []interface{}{
						map[string]interface{}{
							"metric": map[string]interface{}{
								"__name__": "up",
								"host":     "localhost:9090",
								"service":  "prometheus",
							},
							"value": []interface{}{
								float64(1677758935),
								"1",
							},
						},
					},
				},
			},
		},
	}

	// Run tests
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := rw.RewriteResultJSON([]byte(tc.input))
			
			// Parse the result
			var resultMap map[string]interface{}
			if err := json.Unmarshal(result, &resultMap); err != nil {
				t.Fatalf("Failed to parse result JSON: %v", err)
			}
			
			// Compare with expected
			if !reflect.DeepEqual(resultMap, tc.expected) {
				t.Errorf("Expected %v, got %v", tc.expected, resultMap)
			}
		})
	}
}
