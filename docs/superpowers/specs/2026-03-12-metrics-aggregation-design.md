# Metrics Aggregation Design

**Date**: 2026-03-12
**Status**: Approved

## Summary

Add federated Prometheus metrics aggregation to OpenTela. The head node periodically scrapes `/metrics` from all connected workers' services via libp2p, relabels with peer metadata, and serves everything — plus OpenTela's own operational metrics — on the head node's `/metrics` endpoint.

## Goals

- Single `/metrics` endpoint on the head node exposes the entire network's metrics
- Per-peer labels (`peer_id`, `provider_id`, `service`, `model`) for filtering/grouping
- OpenTela operational metrics for network health, request routing, and scraper status
- Standard Prometheus integration — works with any Prometheus/Grafana setup

## Architecture

### New Package: `src/internal/metrics/`

Three components:

#### 1. MetricsScraper (`scraper.go`)

- Periodically scrapes `/metrics` from each connected worker's service endpoint via libp2p HTTP transport
- Uses `GetConnectedPeers()` from node table to discover targets dynamically
- Parses Prometheus exposition format using `github.com/prometheus/common/expfmt` (`expfmt.NewDecoder` API, not the deprecated `TextParser`)
- Caches results per peer in a `sync.Map`
- Concurrent scraping bounded by configurable semaphore (`max_concurrent_scrapes`)
- Per-scrape timeout (default 5s) so one slow worker doesn't block the cycle
- Only runs on head/dispatcher nodes

#### 2. AggregatedCollector (`collector.go`)

- Implements `prometheus.Collector` interface as an **unchecked collector**: `Describe()` sends no descriptors (signals dynamic metrics), `Collect()` yields all metrics
- Registered on the default Prometheus registry
- On `Collect()`: reads cached scraped metrics, injects peer labels + `otela_node_` prefix, yields as Prometheus metric families
- Also yields OpenTela operational metrics (see below)

#### 3. Label Injection (`relabeler.go`)

- Takes raw parsed metrics and appends peer metadata labels from the node table
- Prefixes worker metrics with `otela_node_` namespace to avoid collisions with OpenTela's own metrics

### Integration

- Initialized in `server.StartServer()` (`server.go`) where the protocol host, CRDT store, and Gin router are set up
- Scraper receives references to node table and libp2p host's HTTP transport
- Creates a dedicated `*http.Transport` with `p2phttp.NewTransport(node)` for scraping (separate from the proxy transport, with shorter timeouts)
- Only starts if `metrics.aggregation_enabled` is true in config
- Worker `/metrics` is already reachable via libp2p because the Gin router is served on the p2p listener (`http.Serve(p2plistener, r)` in `server.go`)

### Configuration

In `cfg.yaml`:

```yaml
metrics:
  aggregation_enabled: true
  scrape_interval_seconds: 30
  scrape_timeout_seconds: 5
  worker_metrics_path: "/metrics"
  max_concurrent_scrapes: 10
```

## OpenTela Operational Metrics

### Network (`otela_network_`)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `otela_network_peers_connected` | Gauge | — | Currently connected peers |
| `otela_network_peers_total` | Gauge | — | Total known peers |
| `otela_network_peer_latency_ms` | Gauge | `peer_id` | Last measured latency per peer |

### Request Routing (`otela_routing_`)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `otela_routing_requests_total` | Counter | `service`, `status` | Requests forwarded |
| `otela_routing_request_duration_seconds` | Histogram | `service` | End-to-end forwarding latency |
| `otela_routing_fallback_total` | Counter | `service`, `level` | Fallback tier usage |

### Scraper Health (`otela_scraper_`)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `otela_scraper_targets` | Gauge | — | Peers being scraped |
| `otela_scraper_errors_total` | Counter | `peer_id` | Scrape failures |
| `otela_scraper_duration_seconds` | Histogram | `peer_id` | Per-peer scrape duration |
| `otela_scraper_cycle_duration_seconds` | Histogram | — | Total wall time for one full scrape cycle |

## Data Flow

### Scrape Cycle

1. Every `scrape_interval` seconds, scraper calls `GetConnectedPeers()`
2. For each peer with services, spawns goroutine (bounded by semaphore)
3. Each goroutine: HTTP GET `libp2p://{peerID}/metrics` via p2p transport, with timeout
4. Parse response with `expfmt.NewDecoder`
5. Store parsed metric families in `sync.Map` keyed by peer ID
6. On next Prometheus scrape of head node, `Collect()` reads the map, injects `otela_node_` prefix + peer labels, yields metrics

### Error Handling

- **Scrape timeout/failure**: Log warning, increment `otela_scraper_errors_total`, serve stale cached data (or nothing if first scrape). Don't block other peers.
- **Peer disconnects**: Disappears from `GetConnectedPeers()` → scraper actively deletes its entry from the `sync.Map` at the start of each cycle (iterate current peers, delete map entries not in the set).
- **Parse errors**: Skip that peer's metrics for this cycle, log + increment error counter.
- **Aggregation-enabled only**: Scraper only starts when `metrics.aggregation_enabled` is true in config.

## Files to Create

- `src/internal/metrics/scraper.go` — MetricsScraper
- `src/internal/metrics/collector.go` — AggregatedCollector (prometheus.Collector)
- `src/internal/metrics/relabeler.go` — Label injection and namespacing
- `src/internal/metrics/scraper_test.go` — Scraper unit tests
- `src/internal/metrics/collector_test.go` — Collector unit tests

## Files to Modify

- `src/internal/server/server.go` — Initialize scraper + register AggregatedCollector on Prometheus registry
- `src/internal/server/proxy_handler.go` — Instrument routing metrics (counters, histograms)
- `src/internal/protocol/clock.go` or `node_table.go` — Instrument network metrics (peer gauges)
- `src/entry/cmd/root.go` — Add metrics config to Viper bindings
