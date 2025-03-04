package rewriter

import (
	"encoding/json"
	"log"
	"net/url"
	"regexp"
	"strings"

	"github.com/zwo-bot/prom-relabel-proxy/internal/config"
)

// Rewriter handles the rewriting of labels in Prometheus queries and results
type Rewriter struct {
	queryRules  []config.Rule
	resultRules []config.Rule
}

// New creates a new Rewriter with the given configuration
func New(cfg *config.Config) *Rewriter {
	return &Rewriter{
		queryRules:  cfg.GetQueryRules(),
		resultRules: cfg.GetResultRules(),
	}
}

// UpdateConfig updates the rewriter with new configuration
func (r *Rewriter) UpdateConfig(cfg *config.Config) {
	r.queryRules = cfg.GetQueryRules()
	r.resultRules = cfg.GetResultRules()
}

// RewriteQuery rewrites labels in a Prometheus query
func (r *Rewriter) RewriteQuery(query string) string {
	if len(r.queryRules) == 0 {
		return query
	}

	// Simple label matcher pattern: {label="value"} or {label=~"value"}
	// This is a simplified approach and might need to be enhanced for complex PromQL
	labelPattern := regexp.MustCompile(`\{([^{}]*)\}`)
	
	return labelPattern.ReplaceAllStringFunc(query, func(match string) string {
		// Remove the braces
		inner := match[1 : len(match)-1]
		
		// Split by comma for multiple label matchers
		parts := strings.Split(inner, ",")
		
		for i, part := range parts {
			for _, rule := range r.queryRules {
				// Look for the source label in this part
				if strings.HasPrefix(strings.TrimSpace(part), rule.SourceLabel+"=") ||
				   strings.HasPrefix(strings.TrimSpace(part), rule.SourceLabel+"=~") ||
				   strings.HasPrefix(strings.TrimSpace(part), rule.SourceLabel+"!=") ||
				   strings.HasPrefix(strings.TrimSpace(part), rule.SourceLabel+"!~") {
					// Replace the label name but keep the operator and value
					operator := "="
					if strings.Contains(part, "=~") {
						operator = "=~"
					} else if strings.Contains(part, "!=") {
						operator = "!="
					} else if strings.Contains(part, "!~") {
						operator = "!~"
					}
					
					valueStart := strings.Index(part, operator) + len(operator)
					value := part[valueStart:]
					
					parts[i] = rule.TargetLabel + operator + value
					break
				}
			}
		}
		
		return "{" + strings.Join(parts, ",") + "}"
	})
}

// RewriteQueryURL rewrites labels in a Prometheus query URL
func (r *Rewriter) RewriteQueryURL(queryURL *url.URL) *url.URL {
	query := queryURL.Query()
	
	// Handle different Prometheus API endpoints
	for _, param := range []string{"query", "match[]"} {
		if values, exists := query[param]; exists {
			for i, value := range values {
				query[param][i] = r.RewriteQuery(value)
			}
		}
	}
	
	queryURL.RawQuery = query.Encode()
	return queryURL
}

// RewriteResultJSON rewrites labels in Prometheus JSON result
func (r *Rewriter) RewriteResultJSON(jsonData []byte) []byte {
	if len(r.resultRules) == 0 {
		return jsonData
	}

	// Parse the JSON
	var data map[string]interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		log.Printf("Error parsing JSON response: %v", err)
		return jsonData
	}

	// Process the data structure
	r.processJSONData(data)

	// Re-encode the JSON
	result, err := json.Marshal(data)
	if err != nil {
		log.Printf("Error encoding JSON response: %v", err)
		return jsonData
	}

	return result
}

// processJSONData recursively processes the JSON data structure
func (r *Rewriter) processJSONData(data interface{}) {
	switch v := data.(type) {
	case map[string]interface{}:
		// Check if this is a metric object with labels
		if metric, ok := v["metric"].(map[string]interface{}); ok {
			// This is a metric object, rewrite the labels
			for _, rule := range r.resultRules {
				if val, exists := metric[rule.SourceLabel]; exists {
					metric[rule.TargetLabel] = val
					delete(metric, rule.SourceLabel)
				}
			}
		}
		
		// Process all fields recursively
		for _, value := range v {
			r.processJSONData(value)
		}
	case []interface{}:
		// Process array elements
		for _, item := range v {
			r.processJSONData(item)
		}
	}
}
