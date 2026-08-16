'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { Loader2, RefreshCw } from 'lucide-react';
import { tokenManagerConfig } from '../account/config';
import { formatDate } from '../account/format';
import { listMeshServices, type ServiceSummary } from '../account/services';
import {
  DEFAULT_HOURS,
  HOUR_OPTIONS,
  ObservatoryUnavailableError,
  fetchObservatory,
  formatMs,
  formatPercent,
  formatTps,
  successRateTone,
  type ObservatoryReport,
} from './observatory';

// Scoped to LLM inference for now: the mesh can serve other workloads, but
// they do not have enough traffic to make the numbers meaningful. The API
// accepts any service filter — this is a product choice, not a limitation.
const SERVICE = 'llm';

export default function ObservatoryView() {
  const [report, setReport] = useState<ObservatoryReport | null>(null);
  const [hours, setHours] = useState(DEFAULT_HOURS);
  const [model, setModel] = useState('');
  const [services, setServices] = useState<ServiceSummary[]>([]);
  const [pending, setPending] = useState(true);
  const [loaded, setLoaded] = useState(false);
  const [notice, setNotice] = useState<{
    kind: 'error' | 'info' | 'success';
    message: string;
  } | null>(null);
  // Flipping the window or filters fires overlapping loads; the sequence
  // number makes sure only the latest response ever touches the state.
  const requestSeq = useRef(0);

  const load = useCallback(
    async (announce: boolean) => {
      const seq = ++requestSeq.current;
      setPending(true);
      try {
        const next = await fetchObservatory(tokenManagerConfig.apiBaseUrl, {
          hours,
          service: SERVICE,
          model: model || undefined,
        });
        if (seq !== requestSeq.current) return;
        setReport(next);
        setNotice(null);
        if (announce) {
          setNotice({ kind: 'success', message: 'Observatory refreshed' });
        }
      } catch (error) {
        if (seq !== requestSeq.current) return;
        setReport(null);
        if (error instanceof ObservatoryUnavailableError) {
          setNotice({ kind: 'info', message: error.message });
        } else {
          setNotice({
            kind: 'error',
            message: error instanceof Error ? error.message : String(error),
          });
        }
      } finally {
        if (seq !== requestSeq.current) return;
        setPending(false);
        setLoaded(true);
      }
    },
    [hours, model],
  );

  useEffect(() => {
    load(false).catch(() => undefined);
  }, [load]);

  // The gateway caches the report for about a minute server-side, so quietly
  // re-reading it every 60 s picks up fresh numbers whenever they exist.
  useEffect(() => {
    const id = setInterval(() => {
      load(false).catch(() => undefined);
    }, 60_000);
    return () => clearInterval(id);
  }, [load]);

  // The model filter is a dropdown of the models the llm service advertises
  // in the catalogue; a catalogue failure only means the dropdown stays at
  // "all models", so it stays silent.
  useEffect(() => {
    listMeshServices(tokenManagerConfig.apiBaseUrl)
      .then((items) => setServices(items))
      .catch(() => setServices([]));
  }, []);

  const entries = report?.entries ?? [];
  // The throughput bar in the Avg t/s column scales to the fastest row.
  const maxAvgTps = Math.max(
    0,
    ...entries.map((entry) => entry.avg_output_tokens_per_sec),
  );
  const modelOptions = services.find((s) => s.name === SERVICE)?.models ?? [];

  return (
    <main id="nd-main" className="obs-shell">
      <div className="obs-wrap">
        <header className="acct-page-head">
          <div>
            <p className="otm-eyebrow">OpenTela</p>
            <h1>Mesh observatory</h1>
            <p className="acct-page-sub">
              Real throughput measured at the gateway while serving live LLM
              inference — time to first token and output tokens per second per
              GPU model and served model, aggregated over the trailing window.
              No benchmarks, no self-reported numbers: just the traffic the mesh
              actually answered.
            </p>
          </div>
          <div className="acct-page-actions">
            <button
              type="button"
              className="otm-secondary-button"
              onClick={() => {
                load(true).catch(() => undefined);
              }}
              disabled={pending}
            >
              {pending ? (
                <Loader2 className="otm-spin" size={16} />
              ) : (
                <RefreshCw size={16} />
              )}
              Refresh
            </button>
          </div>
        </header>

        {notice ? (
          <div
            className={`otm-notice ${notice.kind}`}
            role="status"
            style={{ marginBottom: 16 }}
          >
            {notice.message}
          </div>
        ) : null}

        <section className="otm-panel">
          <div className="obs-controls">
            <div className="obs-control-group">
              <div className="obs-field">
                <span className="obs-field-label" id="obs-window-label">
                  Time window
                </span>
                <div
                  className="obs-window-group"
                  role="group"
                  aria-labelledby="obs-window-label"
                >
                  {HOUR_OPTIONS.map(({ hours: h, label }) => (
                    <button
                      key={h}
                      type="button"
                      className={h === hours ? 'active' : ''}
                      aria-pressed={h === hours}
                      onClick={() => setHours(h)}
                    >
                      {label}
                    </button>
                  ))}
                </div>
              </div>
              <label>
                Model
                <select
                  value={model}
                  onChange={(e) => setModel(e.target.value)}
                  disabled={modelOptions.length === 0}
                  aria-label="Filter by model"
                >
                  <option value="">all models</option>
                  {modelOptions.map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            {report?.generated_at ? (
              <span className="otm-eyebrow">
                generated {formatDate(report.generated_at)}
              </span>
            ) : null}
          </div>

          {!loaded ? (
            <div className="otm-empty-state">Measuring the mesh…</div>
          ) : entries.length === 0 && !notice ? (
            <div className="otm-empty-state">
              No
              {model ? (
                <>
                  {' '}
                  <code>{model}</code>
                </>
              ) : null}{' '}
              LLM inference traffic was measured in this window yet. Rows
              appear as soon as requests go through the gateway.
            </div>
          ) : entries.length === 0 ? null : (
            <div className="otm-table-wrap">
              <table>
                <thead>
                  <tr>
                    <th aria-label="Rank" />
                    <th>GPU</th>
                    <th>Model</th>
                    <th className="obs-num" title="Requests measured in the window">
                      Requests
                    </th>
                    <th className="obs-num" title="Distinct providers behind the rows">
                      Providers
                    </th>
                    <th className="obs-num" title="Requests without a server-side error">
                      Success
                    </th>
                    <th
                      className="obs-num"
                      title="Average output tokens per second over generation time"
                    >
                      Avg t/s
                    </th>
                    <th
                      className="obs-num"
                      title="Median per-request output tokens per second"
                    >
                      p50 t/s
                    </th>
                    <th className="obs-num" title="Median time to first output token">
                      TTFT p50
                    </th>
                    <th
                      className="obs-num"
                      title="90th-percentile time to first output token"
                    >
                      TTFT p90
                    </th>
                    <th
                      className="obs-num"
                      title="99th-percentile time to first output token"
                    >
                      TTFT p99
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {entries.map((row, index) => (
                    <tr key={`${row.gpu_model}|${row.model}`}>
                      <td>
                        <span className={`obs-rank${index === 0 ? ' first' : ''}`}>
                          {index + 1}
                        </span>
                      </td>
                      <td>
                        <code>{row.gpu_model}</code>
                      </td>
                      <td>
                        <code>{row.model}</code>
                      </td>
                      <td className="obs-num">
                        {row.requests.toLocaleString()}
                      </td>
                      <td className="obs-num">{row.providers}</td>
                      <td className="obs-num">
                        <span
                          className={`otm-status-pill ${successRateTone(row.success_rate)}`.trim()}
                        >
                          {formatPercent(row.success_rate)}
                        </span>
                      </td>
                      <td className="obs-num obs-tps-cell">
                        <span
                          className="obs-tps-fill"
                          aria-hidden="true"
                          style={{
                            width:
                              maxAvgTps > 0
                                ? `${(row.avg_output_tokens_per_sec / maxAvgTps) * 100}%`
                                : 0,
                          }}
                        />
                        <span className="obs-tps-value">
                          {formatTps(row.avg_output_tokens_per_sec)}
                        </span>
                      </td>
                      <td className="obs-num">
                        {formatTps(row.p50_output_tokens_per_sec)}
                      </td>
                      <td className="obs-num">{formatMs(row.ttft_p50_ms)}</td>
                      <td className="obs-num">{formatMs(row.ttft_p90_ms)}</td>
                      <td className="obs-num">{formatMs(row.ttft_p99_ms)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>

        <p className="acct-page-sub" style={{ marginTop: 16 }}>
          Providers are counted by anonymised peer fingerprint — no operator
          identity is reported. A window of{' '}
          {report ? `${report.window_hours} h` : `${hours} h`} is shown; the
          report is cached server-side for about a minute.
        </p>
      </div>
    </main>
  );
}
