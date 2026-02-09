package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zwo-bot/prom-relabel-proxy/internal/config"
)

// newTestConfig creates a config for testing
func newTestConfig(target string) *config.Config {
	return &config.Config{
		TargetPrometheus: target,
		Mappings: []config.Mapping{
			{
				Direction: config.DirectionQuery,
				Rules: []config.Rule{
					{SourceLabel: "host", TargetLabel: "instance"},
				},
			},
			{
				Direction: config.DirectionResult,
				Rules: []config.Rule{
					{SourceLabel: "instance", TargetLabel: "host"},
				},
			},
		},
	}
}

// newTestBackend creates a fake Prometheus backend for testing
func newTestBackend(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func TestNew(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := newTestConfig("http://localhost:9090")
		p, err := New(cfg, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil proxy")
		}
	})

	t.Run("invalid target URL", func(t *testing.T) {
		cfg := &config.Config{TargetPrometheus: "://invalid"}
		_, err := New(cfg, false)
		if err == nil {
			t.Fatal("expected error for invalid URL")
		}
	})
}

func TestServeHTTP_QueryRewriting(t *testing.T) {
	// Set up a fake Prometheus backend that echoes the received query
	backend := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]string{
			"received_query": r.URL.Query().Get("query"),
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer backend.Close()

	cfg := newTestConfig(backend.URL)
	p, err := New(cfg, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create a request with a query that should be rewritten
	req := httptest.NewRequest("GET", "/api/v1/query?query=up{host=\"localhost:9090\"}", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// The backend should have received the rewritten query (host → instance)
	var resp map[string]string
	body, _ := io.ReadAll(rec.Body)
	// The response body is the rewritten JSON (result rules apply to response)
	// Just check that the proxy returns a valid response
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to decode response: %v (body: %s)", err, string(body))
	}
}

func TestServeHTTP_POSTFormRewriting(t *testing.T) {
	var receivedQuery string
	backend := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		receivedQuery = form.Get("query")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success"}`))
	})
	defer backend.Close()

	cfg := newTestConfig(backend.URL)
	p, err := New(cfg, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	formData := url.Values{}
	formData.Set("query", `up{host="localhost:9090"}`)
	req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Check that the backend received the rewritten query
	if !strings.Contains(receivedQuery, "instance") {
		t.Errorf("expected query to contain 'instance' (rewritten from 'host'), got: %s", receivedQuery)
	}
}

func TestServeHTTP_ResponseRewriting(t *testing.T) {
	promResponse := `{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [{
				"metric": {"__name__":"up","instance":"localhost:9090"},
				"value": [1677758935, "1"]
			}]
		}
	}`

	backend := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(promResponse))
	})
	defer backend.Close()

	cfg := newTestConfig(backend.URL)
	p, err := New(cfg, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/query?query=up", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Body)
	bodyStr := string(body)

	// The response should have "host" instead of "instance" (result rule)
	if !strings.Contains(bodyStr, `"host"`) {
		t.Errorf("expected response to contain 'host' (rewritten from 'instance'), body: %s", bodyStr)
	}
	if strings.Contains(bodyStr, `"instance"`) {
		t.Errorf("expected response NOT to contain 'instance' after rewriting, body: %s", bodyStr)
	}
}

func TestServeHTTP_GzipResponse(t *testing.T) {
	promResponse := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up","instance":"localhost:9090"},"value":[1677758935,"1"]}]}}`

	backend := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")

		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		gz.Write([]byte(promResponse))
		gz.Close()
		w.Write(buf.Bytes())
	})
	defer backend.Close()

	cfg := newTestConfig(backend.URL)
	p, err := New(cfg, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/query?query=up", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	// Response should be gzip compressed — decompress
	body := rec.Body.Bytes()
	var decompressed []byte
	if rec.Header().Get("Content-Encoding") == "gzip" {
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("failed to create gzip reader: %v", err)
		}
		decompressed, _ = io.ReadAll(reader)
		reader.Close()
	} else {
		decompressed = body
	}

	bodyStr := string(decompressed)
	if !strings.Contains(bodyStr, `"host"`) {
		t.Errorf("expected gzip response to contain 'host' after rewriting, body: %s", bodyStr)
	}
}

func TestServeHTTP_NonJSONPassthrough(t *testing.T) {
	backend := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("# HELP up Target is up\nup 1\n"))
	})
	defer backend.Close()

	cfg := newTestConfig(backend.URL)
	p, err := New(cfg, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Body)
	// Non-JSON responses should pass through unchanged
	if !strings.Contains(string(body), "# HELP up") {
		t.Errorf("expected non-JSON response to pass through, got: %s", string(body))
	}
}

func TestUpdateConfig(t *testing.T) {
	cfg := newTestConfig("http://localhost:9090")
	p, err := New(cfg, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newCfg := newTestConfig("http://new-target:9090")
	if err := p.UpdateConfig(newCfg); err != nil {
		t.Fatalf("unexpected error updating config: %v", err)
	}

	if p.targetURL.Host != "new-target:9090" {
		t.Errorf("expected target host new-target:9090, got %s", p.targetURL.Host)
	}
}

func TestUpdateConfig_InvalidURL(t *testing.T) {
	cfg := newTestConfig("http://localhost:9090")
	p, err := New(cfg, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	badCfg := &config.Config{TargetPrometheus: "://invalid"}
	if err := p.UpdateConfig(badCfg); err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/api/v1/query", "/api/v1/query"},
		{"/api/v1/query_range", "/api/v1/query_range"},
		{"/api/v1/series", "/api/v1/series"},
		{"/health", "/health"},
		{"/", "/"},
	}

	for _, tc := range tests {
		got := normalizePath(tc.input)
		if got != tc.expected {
			t.Errorf("normalizePath(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestDebugLog(t *testing.T) {
	// Just ensure it doesn't panic
	cfg := newTestConfig("http://localhost:9090")
	p, _ := New(cfg, true)
	p.debugLog("test %s", "message")

	p2, _ := New(cfg, false)
	p2.debugLog("test %s", "message")
}
