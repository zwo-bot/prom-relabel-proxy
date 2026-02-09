package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/zwo-bot/prom-relabel-proxy/internal/config"
	"github.com/zwo-bot/prom-relabel-proxy/internal/rewriter"
)

var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "prom_relabel_proxy_requests_total",
		Help: "Total number of proxied requests.",
	}, []string{"method", "path", "status"})

	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "prom_relabel_proxy_request_duration_seconds",
		Help:    "Duration of proxied requests in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	rewriteErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "prom_relabel_proxy_rewrite_errors_total",
		Help: "Total number of rewrite errors.",
	})
)

// PrometheusProxy is a reverse proxy for Prometheus that rewrites labels
type PrometheusProxy struct {
	targetURL *url.URL
	proxy     *httputil.ReverseProxy
	rewriter  *rewriter.Rewriter
	debug     bool
}

// New creates a new PrometheusProxy
func New(cfg *config.Config, debug bool) (*PrometheusProxy, error) {
	targetURL, err := url.Parse(cfg.GetTargetPrometheus())
	if err != nil {
		return nil, err
	}

	rw := rewriter.New(cfg)

	proxy := &PrometheusProxy{
		targetURL: targetURL,
		rewriter:  rw,
		debug:     debug,
	}

	// Create the reverse proxy
	reverseProxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Customize the director function to modify the request
	originalDirector := reverseProxy.Director
	reverseProxy.Director = func(req *http.Request) {
		originalDirector(req)
		proxy.rewriteRequest(req)
	}

	// Add a response modifier
	reverseProxy.ModifyResponse = proxy.rewriteResponse

	proxy.proxy = reverseProxy

	return proxy, nil
}

// UpdateConfig updates the proxy with new configuration
func (p *PrometheusProxy) UpdateConfig(cfg *config.Config) error {
	targetURL, err := url.Parse(cfg.GetTargetPrometheus())
	if err != nil {
		return err
	}

	p.targetURL = targetURL
	p.rewriter.UpdateConfig(cfg)

	return nil
}

// ServeHTTP implements the http.Handler interface
func (p *PrometheusProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
	p.proxy.ServeHTTP(rec, r)
	duration := time.Since(start).Seconds()

	path := normalizePath(r.URL.Path)
	requestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(rec.statusCode)).Inc()
	requestDuration.WithLabelValues(r.Method, path).Observe(duration)
}

// statusRecorder wraps http.ResponseWriter to capture the status code
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// normalizePath normalizes the request path for metric labels
func normalizePath(p string) string {
	// Collapse Prometheus API paths to reduce cardinality
	if strings.HasPrefix(p, "/api/v1/") {
		parts := strings.SplitN(p, "/", 5)
		if len(parts) >= 4 {
			return "/api/v1/" + parts[3]
		}
	}
	return p
}

// debugLog logs a message if debug mode is enabled
func (p *PrometheusProxy) debugLog(format string, v ...interface{}) {
	if p.debug {
		log.Printf(format, v...)
	}
}

// rewriteRequest modifies the request before it's sent to Prometheus
func (p *PrometheusProxy) rewriteRequest(req *http.Request) {
	p.debugLog("Rewriting request: %s %s", req.Method, req.URL.String())

	// Rewrite the URL query parameters
	originalURL := req.URL.String()
	req.URL = p.rewriter.RewriteQueryURL(req.URL)
	p.debugLog("Rewrote URL from %s to %s", originalURL, req.URL.String())

	// If it's a POST request with form data, we need to handle that too
	if req.Method == http.MethodPost && req.Body != nil {
		contentType := req.Header.Get("Content-Type")
		p.debugLog("POST request with Content-Type: %s", contentType)

		if contentType == "application/x-www-form-urlencoded" {
			// Read the body
			body, err := io.ReadAll(req.Body)
			if err != nil {
				p.debugLog("Error reading request body: %v", err)
				return
			}
			p.debugLog("Original request body: %s", string(body))

			// Parse the form data
			form, err := url.ParseQuery(string(body))
			if err != nil {
				p.debugLog("Error parsing form data: %v", err)
				return
			}

			// Rewrite the query parameters
			for param, values := range form {
				if param == "query" || param == "match[]" {
					for i, value := range values {
						originalValue := value
						form[param][i] = p.rewriter.RewriteQuery(value)
						p.debugLog("Rewrote query param %s from %s to %s", param, originalValue, form[param][i])
					}
				}
			}

			// Encode the form data back to the body
			newBody := form.Encode()
			p.debugLog("New request body: %s", newBody)
			req.Body = io.NopCloser(bytes.NewBufferString(newBody))
			req.ContentLength = int64(len(newBody))

			// Use strconv.Itoa instead of string(rune()) for proper string conversion
			contentLengthStr := strconv.Itoa(len(newBody))
			p.debugLog("Setting Content-Length header to: %s", contentLengthStr)
			req.Header.Set("Content-Length", contentLengthStr)
		}
	}
}

// rewriteResponse modifies the response before it's sent back to the client
func (p *PrometheusProxy) rewriteResponse(resp *http.Response) error {
	p.debugLog("Rewriting response from %s", resp.Request.URL.String())

	// Only process JSON responses
	contentType := resp.Header.Get("Content-Type")
	p.debugLog("Response Content-Type: %s", contentType)

	if !strings.Contains(contentType, "application/json") {
		p.debugLog("Skipping non-JSON response")
		return nil
	}

	// Check for compression
	contentEncoding := resp.Header.Get("Content-Encoding")
	p.debugLog("Response Content-Encoding: %s", contentEncoding)

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.debugLog("Error reading response body: %v", err)
		rewriteErrors.Inc()
		return err
	}

	// Close the original body
	resp.Body.Close()

	// Decompress if needed
	var decompressedBody []byte
	if contentEncoding == "gzip" {
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			p.debugLog("Error creating gzip reader: %v", err)
			rewriteErrors.Inc()
			// If we can't decompress, return the original body
			resp.Body = io.NopCloser(bytes.NewBuffer(body))
			return nil
		}
		decompressedBody, err = io.ReadAll(reader)
		reader.Close()
		if err != nil {
			p.debugLog("Error decompressing response: %v", err)
			rewriteErrors.Inc()
			// If we can't decompress, return the original body
			resp.Body = io.NopCloser(bytes.NewBuffer(body))
			return nil
		}

		// Log a sample of the decompressed body
		if p.debug {
			bodyPreview := string(decompressedBody)
			if len(bodyPreview) > 200 {
				bodyPreview = bodyPreview[:200] + "..."
			}
			p.debugLog("Decompressed response body (sample): %s", bodyPreview)
		}
	} else {
		decompressedBody = body

		// Log a sample of the response body
		if p.debug {
			bodyPreview := string(body)
			if len(bodyPreview) > 200 {
				bodyPreview = bodyPreview[:200] + "..."
			}
			p.debugLog("Original response body (sample): %s", bodyPreview)
		}
	}

	// Rewrite the JSON
	newBody := p.rewriter.RewriteResultJSON(decompressedBody)

	// Log a sample of the new response body
	if p.debug {
		newBodyPreview := string(newBody)
		if len(newBodyPreview) > 200 {
			newBodyPreview = newBodyPreview[:200] + "..."
		}
		p.debugLog("Rewritten response body (sample): %s", newBodyPreview)
	}

	// Re-compress if the original was compressed
	var finalBody []byte
	if contentEncoding == "gzip" {
		var buf bytes.Buffer
		gzipWriter := gzip.NewWriter(&buf)
		_, err := gzipWriter.Write(newBody)
		gzipWriter.Close()
		if err != nil {
			p.debugLog("Error compressing response: %v", err)
			rewriteErrors.Inc()
			// If we can't compress, use uncompressed and remove the encoding header
			finalBody = newBody
			resp.Header.Del("Content-Encoding")
		} else {
			finalBody = buf.Bytes()
		}
	} else {
		finalBody = newBody
	}

	// Set the new body
	resp.Body = io.NopCloser(bytes.NewBuffer(finalBody))
	resp.ContentLength = int64(len(finalBody))

	// Use strconv.Itoa for proper string conversion
	contentLengthStr := strconv.Itoa(len(finalBody))
	p.debugLog("Setting response Content-Length header to: %s", contentLengthStr)
	resp.Header.Set("Content-Length", contentLengthStr)

	return nil
}
