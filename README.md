# Prometheus Label Rewriting Proxy (prom-relabel-proxy)

A simple proxy service for Prometheus that rewrites labels in queries and results.

## Features

- Intercepts Prometheus API requests and rewrites label names according to configured rules
- Supports label rewriting in both queries and results
- Handles compressed (gzip) responses from Prometheus
- Configurable via YAML file
- Transparent pass-through of authentication headers
- Designed for easy extension with more complex rewriting rules

## Configuration

The proxy is configured via a YAML file. Here's an example:

```yaml
target_prometheus: "http://localhost:9090"
mappings:
  - direction: "query"
    rules:
      - source_label: "instance"
        target_label: "host"
      - source_label: "job"
        target_label: "service"
  - direction: "result"
    rules:
      - source_label: "instance"
        target_label: "host"
      - source_label: "job"
        target_label: "service"
```

### Configuration Options

- `target_prometheus`: The URL of the upstream Prometheus server
- `mappings`: A list of mapping configurations
  - `direction`: The direction to apply the rules to (`query`, `result`, or `both`)
  - `rules`: A list of label mapping rules
    - `source_label`: The original label name
    - `target_label`: The new label name

## Usage

### Building

```bash
go build -o prom-relabel-proxy ./cmd/prom-relabel
```

### Running

```bash
./prom-relabel-proxy --config=configs/config.yaml --listen=:8080
```

### Command Line Options

- `--config`: Path to the configuration file (default: `configs/config.yaml`)
- `--listen`: Address to listen on (default: `:8080`)
- `--debug`: Enable detailed debug logging (default: `false`)

## Example

With the example configuration above, a query like:

```
http://localhost:8080/api/v1/query?query=up{instance="localhost:9090",job="prometheus"}
```

Will be rewritten to:

```
http://localhost:9090/api/v1/query?query=up{host="localhost:9090",service="prometheus"}
```

And the labels in the response will be rewritten back from `host` to `instance` and from `service` to `job`.

## Compression Handling

The proxy automatically detects and handles gzip-compressed responses from Prometheus:
- Decompresses responses before applying label transformations
- Re-compresses responses before sending them back to the client
- Preserves all original headers and compression settings

## Debugging

When run with the `--debug` flag, the proxy provides detailed logging about:
- Incoming requests and how they're rewritten
- Response handling, including compression detection and decompression
- Label transformations in both directions
- Content of requests and responses (truncated for readability)

This can be helpful when troubleshooting label rewriting issues or understanding how the proxy is transforming your queries and results.

## Kubernetes Deployment

For Kubernetes deployment, you can create a ConfigMap for the configuration and deploy the proxy as a Service.

## Future Enhancements

- Support for more complex transformation rules (regex, conditionals)
- Metrics about proxy operations
- Caching for performance optimization
- Multiple upstream Prometheus servers
