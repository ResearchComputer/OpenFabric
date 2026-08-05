import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  ObservatoryUnavailableError,
  fetchObservatory,
  formatMs,
  formatPercent,
  formatTps,
  observatoryURL,
  successRateTone,
} from './observatory';

afterEach(() => vi.restoreAllMocks());

// Shaped like the live /v1/leaderboard response.
const BODY = {
  generated_at: '2026-08-04T20:55:31Z',
  window_hours: 168,
  entries: [
    {
      gpu_model: 'NVIDIA GeForce RTX 4090',
      model: 'gpt-4o',
      requests: 101,
      providers: 8,
      success_rate: 0.99,
      avg_output_tokens_per_sec: 55.94,
      p50_output_tokens_per_sec: 56,
      ttft_p50_ms: 224,
      ttft_p90_ms: 244,
      ttft_p99_ms: 248,
    },
    {
      gpu_model: 'Tesla T4',
      model: 'gpt-4o',
      requests: 101,
      providers: 8,
      success_rate: 1,
      avg_output_tokens_per_sec: 17.95,
      p50_output_tokens_per_sec: 18,
      ttft_p50_ms: 225,
      ttft_p90_ms: 245,
      ttft_p99_ms: 249,
    },
  ],
};

describe('fetchObservatory', () => {
  it('reads the report without credentials', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(JSON.stringify(BODY), { status: 200 }));

    const report = await fetchObservatory('https://api.example');

    expect(fetchSpy).toHaveBeenCalledWith('https://api.example/v1/leaderboard');
    // no second argument at all -> no Authorization header
    expect(fetchSpy.mock.calls[0]).toHaveLength(1);
    expect(report.window_hours).toBe(168);
    expect(report.entries.map((e) => e.gpu_model)).toEqual([
      'NVIDIA GeForce RTX 4090',
      'Tesla T4',
    ]);
    expect(report.entries[0].avg_output_tokens_per_sec).toBe(55.94);
    expect(report.generated_at).toBe('2026-08-04T20:55:31Z');
  });

  it('encodes window and filters in the query string', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(JSON.stringify(BODY), { status: 200 }));

    await fetchObservatory('https://api.example', {
      hours: 24,
      service: 'llm',
      model: 'Qwen/Qwen3-8B',
    });

    expect(fetchSpy).toHaveBeenCalledWith(
      'https://api.example/v1/leaderboard?hours=24&service=llm&model=Qwen%2FQwen3-8B',
    );
  });

  it('omits the hours parameter at the default window', () => {
    expect(observatoryURL('https://api.example', { hours: 168 })).toBe(
      'https://api.example/v1/leaderboard',
    );
  });

  it('tolerates a report with no entries', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ entries: null }), { status: 200 }),
    );
    const report = await fetchObservatory('https://api.example', { hours: 24 });
    expect(report.entries).toEqual([]);
    expect(report.window_hours).toBe(24);
  });

  it('defaults missing fields rather than rendering undefined', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ entries: [{ gpu_model: 'bare' }] }), {
        status: 200,
      }),
    );
    const [entry] = (await fetchObservatory('https://api.example')).entries;
    expect(entry).toEqual({
      gpu_model: 'bare',
      model: 'unknown',
      requests: 0,
      providers: 0,
      success_rate: 0,
      avg_output_tokens_per_sec: 0,
      p50_output_tokens_per_sec: 0,
      ttft_p50_ms: 0,
      ttft_p90_ms: 0,
      ttft_p99_ms: 0,
    });
  });

  it('treats 404 as the feature being disabled, not an error', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(null, { status: 404 }),
    );
    await expect(fetchObservatory('https://api.example')).rejects.toBeInstanceOf(
      ObservatoryUnavailableError,
    );
  });

  it('throws on other non-2xx responses', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(null, { status: 503 }),
    );
    await expect(fetchObservatory('https://api.example')).rejects.toThrow(/503/);
  });
});

describe('successRateTone', () => {
  it('is green at 99% and above', () => {
    expect(successRateTone(1)).toBe('active');
    expect(successRateTone(0.99)).toBe('active');
  });

  it('is neutral between 95% and 99%', () => {
    expect(successRateTone(0.97)).toBe('');
    expect(successRateTone(0.95)).toBe('');
  });

  it('is red below 95%', () => {
    expect(successRateTone(0.949)).toBe('revoked');
    expect(successRateTone(0)).toBe('revoked');
  });
});

describe('formatting', () => {
  it('renders success rates as percentages', () => {
    expect(formatPercent(0.995)).toBe('99.5%');
    expect(formatPercent(1)).toBe('100.0%');
  });

  it('renders latencies in milliseconds', () => {
    expect(formatMs(224.4)).toBe('224 ms');
  });

  it('renders throughput with one decimal and a unit', () => {
    // The grouping/decimal separators are locale-dependent ('1,234.6' in
    // en-US, '1\u2019234,6' in de-CH), so only the digits are pinned down.
    expect(formatTps(55.94)).toMatch(/55\D9\D?t\/s/);
    expect(formatTps(1234.56)).toMatch(/1\D?234\D6 t\/s/);
  });
});
