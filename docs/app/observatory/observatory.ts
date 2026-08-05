/**
 * GPU performance observatory: how fast each GPU model answers real inference
 * traffic on the mesh.
 *
 * Served by the API at `GET /v1/leaderboard` from per-request measurements the
 * streaming gateway itself records (time to first token, generation duration,
 * token counts) into ClickHouse. Reading it needs no credentials — it is as
 * public as the service catalogue. When the gateway has no ClickHouse
 * configured the endpoint is not mounted at all, so a 404 means the feature is
 * unavailable, not that something broke.
 */

export interface ObservatoryEntry {
  /** GPU model name as reported by the peer (e.g. "NVIDIA GeForce RTX 4090"). */
  gpu_model: string;
  /** Served model the traffic went to. */
  model: string;
  /** Requests measured in the window. */
  requests: number;
  /** Distinct providers (anonymised peer fingerprints) behind the rows. */
  providers: number;
  /** Fraction of requests without a server-side error, 0..1. */
  success_rate: number;
  /** Output tokens per second, averaged over generation time. */
  avg_output_tokens_per_sec: number;
  /** Median output tokens per second across requests. */
  p50_output_tokens_per_sec: number;
  /** Time to first output token, percentiles, milliseconds. */
  ttft_p50_ms: number;
  ttft_p90_ms: number;
  ttft_p99_ms: number;
}

export interface ObservatoryReport {
  /** Server timestamp the report was assembled at. */
  generated_at: string;
  /** Window the aggregates cover, in hours. */
  window_hours: number;
  /** Ranked rows — the API already orders by average throughput. */
  entries: ObservatoryEntry[];
}

export interface ObservatoryQuery {
  /** Window in hours; the API defaults to 168 and caps at 720. */
  hours?: number;
  /** Service filter, e.g. "llm". Empty/omitted means all services. */
  service?: string;
  /** Model filter. Empty/omitted means all models. */
  model?: string;
}

/** Window presets offered in the UI. */
export const HOUR_OPTIONS: { hours: number; label: string }[] = [
  { hours: 24, label: '24 h' },
  { hours: 168, label: '7 days' },
  { hours: 720, label: '30 days' },
];

export const DEFAULT_HOURS = 168;

/** The API does not mount the endpoint without ClickHouse; 404 is "off". */
export class ObservatoryUnavailableError extends Error {
  constructor() {
    super('Performance measurement is not enabled on this deployment');
    this.name = 'ObservatoryUnavailableError';
  }
}

export function observatoryURL(base: string, query: ObservatoryQuery): string {
  const params = new URLSearchParams();
  if (query.hours && query.hours !== DEFAULT_HOURS) {
    params.set('hours', String(query.hours));
  }
  if (query.service) params.set('service', query.service);
  if (query.model) params.set('model', query.model);
  const qs = params.toString();
  return `${base}/v1/leaderboard${qs ? `?${qs}` : ''}`;
}

/** Fetch the observatory report. No credentials — the endpoint is public. */
export async function fetchObservatory(
  baseUrl: string,
  query: ObservatoryQuery = {},
): Promise<ObservatoryReport> {
  const res = await fetch(observatoryURL(baseUrl, query));
  if (res.status === 404) throw new ObservatoryUnavailableError();
  if (!res.ok) throw new Error(`Could not load the observatory report: ${res.status}`);
  const body = (await res.json()) as {
    generated_at?: string;
    window_hours?: number;
    entries?: Partial<ObservatoryEntry>[] | null;
  };
  return {
    generated_at: body.generated_at ?? '',
    window_hours: body.window_hours ?? query.hours ?? DEFAULT_HOURS,
    entries: (body.entries ?? []).map((row) => ({
      gpu_model: row.gpu_model ?? 'unknown',
      model: row.model ?? 'unknown',
      requests: row.requests ?? 0,
      providers: row.providers ?? 0,
      success_rate: row.success_rate ?? 0,
      avg_output_tokens_per_sec: row.avg_output_tokens_per_sec ?? 0,
      p50_output_tokens_per_sec: row.p50_output_tokens_per_sec ?? 0,
      ttft_p50_ms: row.ttft_p50_ms ?? 0,
      ttft_p90_ms: row.ttft_p90_ms ?? 0,
      ttft_p99_ms: row.ttft_p99_ms ?? 0,
    })),
  };
}

/** Pill class for a success rate: green when healthy, red when degraded. */
export function successRateTone(rate: number): 'active' | 'revoked' | '' {
  if (rate >= 0.99) return 'active';
  if (rate < 0.95) return 'revoked';
  return '';
}

/** Format a success rate (0..1 fraction of a float) as a percentage string. */
export function formatPercent(rate: number): string {
  return `${(rate * 100).toFixed(1)}%`;
}

/** Format a throughput value compactly, e.g. "1 234 t/s" (locale grouping). */
export function formatTps(value: number): string {
  return `${value.toLocaleString(undefined, { maximumFractionDigits: 1 })} t/s`;
}

/** Format a latency in milliseconds. */
export function formatMs(value: number): string {
  return `${Math.round(value).toLocaleString()} ms`;
}
