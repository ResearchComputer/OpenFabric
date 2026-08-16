'use client';

import { useCallback, useEffect, useState } from 'react';
import { Copy, Loader2, RefreshCw } from 'lucide-react';
import { useAccount } from '../account-context';
import { tokenManagerConfig } from '../config';
import {
  isCatchAll,
  listMeshCatalogue,
  type MarketEntry,
  type ServiceSummary,
} from '../services';

/** Placeholder used in the copied command when the user has no key to hand. */
const KEY_PLACEHOLDER = '$OPENTELA_API_KEY';

export default function ServicesView() {
  const { lastCreatedSk, showNotice, pending, setPending, releasePending } =
    useAccount();
  const [services, setServices] = useState<ServiceSummary[]>([]);
  const [market, setMarket] = useState<MarketEntry[]>([]);
  const [loaded, setLoaded] = useState(false);

  const load = useCallback(
    async (announce: boolean) => {
      setPending('services');
      try {
        const catalogue = await listMeshCatalogue(tokenManagerConfig.apiBaseUrl);
        setServices(catalogue.services);
        setMarket(catalogue.market);
        if (announce) showNotice('success', 'Catalogue refreshed');
      } catch (error) {
        setServices([]);
        setMarket([]);
        showNotice('error', error instanceof Error ? error.message : String(error));
      } finally {
        setLoaded(true);
        releasePending('services');
      }
    },
    [releasePending, setPending, showNotice],
  );

  // The catalogue is public, so there is nothing to ask for before showing it.
  useEffect(() => {
    load(false).catch(() => undefined);
  }, [load]);

  function curlFor(service: ServiceSummary): string {
    const key = lastCreatedSk?.key ?? KEY_PLACEHOLDER;
    const model = service.models[0];
    const body = model
      ? `{"model":"${model}","messages":[{"role":"user","content":"hello"}]}`
      : `{"messages":[{"role":"user","content":"hello"}]}`;
    return `curl ${tokenManagerConfig.apiBaseUrl}/v1/service/${service.name}/v1/chat/completions \\\n  -H "Authorization: Bearer ${key}" \\\n  -H "Content-Type: application/json" \\\n  -d '${body}'`;
  }

  return (
    <div className="acct-page">
      <header className="acct-page-head">
        <div>
          <h1>Services</h1>
          <p className="acct-page-sub">
            What the permissionless mesh is serving right now. Browsing is open to
            everyone and service names here are not authenticated or reserved —
            use a trusted-region route when provider identity matters.
          </p>
        </div>
        <div className="acct-page-actions">
          <button
            type="button"
            className="otm-secondary-button"
            onClick={() => {
              load(true).catch(() => undefined);
            }}
            disabled={pending === 'services'}
          >
            {pending === 'services' ? (
              <Loader2 className="otm-spin" size={16} />
            ) : (
              <RefreshCw size={16} />
            )}
            Refresh
          </button>
        </div>
      </header>

      <section className="otm-panel">
        <div className="otm-panel-heading">
          <h2>Available services</h2>
        </div>

        <div className="otm-notice" role="note" style={{ margin: '0 16px 16px' }}>
          This catalogue only reflects the public partition. A private service may
          share the same name on a hostile unmanaged peer, so the catalogue alone
          does not prove who will answer. Trusted-region routes stay direct-only,
          skip application relays, and are revalidated again at the worker.
        </div>

        {services.length === 0 ? (
          <div className="otm-empty-state">
            {!loaded
              ? 'Loading the catalogue…'
              : 'No services are registered on the mesh right now. Providers appear here as soon as they come online.'}
          </div>
        ) : (
          <div className="otm-table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Service</th>
                  <th>Models</th>
                  <th>Providers</th>
                  <th aria-label="Copy request" />
                </tr>
              </thead>
              <tbody>
                {services.map((service) => (
                  <tr key={service.name}>
                    <td>
                      <code>{service.name}</code>
                    </td>
                    <td>
                      {service.models.length === 0 ? (
                        <span className="otm-eyebrow">
                          {isCatchAll(service) ? 'any request' : 'none advertised'}
                        </span>
                      ) : (
                        service.models.map((model) => (
                          <code key={model} style={{ marginRight: 6 }}>
                            {model}
                          </code>
                        ))
                      )}
                    </td>
                    <td>
                      <span
                        className={`otm-status-pill ${
                          service.online > 0 ? 'active' : 'revoked'
                        }`}
                      >
                        {service.online} of {service.providers} online
                      </span>
                    </td>
                    <td>
                      <button
                        type="button"
                        className="otm-icon-button"
                        title="Copy a request for this service"
                        onClick={() => {
                          navigator.clipboard
                            .writeText(curlFor(service))
                            .then(() =>
                              showNotice(
                                'success',
                                lastCreatedSk?.key
                                  ? 'Request copied'
                                  : `Request copied — replace ${KEY_PLACEHOLDER} with your key`,
                              ),
                            )
                            .catch(() =>
                              showNotice(
                                'error',
                                'Copy failed — select the command and copy it manually',
                              ),
                            );
                        }}
                      >
                        <Copy size={18} />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {market.length > 0 ? (
        <section className="otm-panel otm-market-panel">
          <div className="otm-panel-heading">
            <div>
              <p className="otm-eyebrow">Price discovery</p>
              <h2>Market prices</h2>
            </div>
            <p className="otm-market-note">
              Live published rates per million tokens. Min/median/max across
              quoting peers; a dash means no provider is quoting yet.
            </p>
          </div>
          <div className="otm-table-wrap">
            <table className="otm-market-table">
              <thead>
                <tr>
                  <th>Model</th>
                  <th>Quoters</th>
                  <th>Input /M</th>
                  <th>Cached /M</th>
                  <th>Output /M</th>
                </tr>
              </thead>
              <tbody>
                {market.map((entry) => (
                  <MarketRow key={`${entry.service}:${entry.model}`} entry={entry} />
                ))}
              </tbody>
            </table>
          </div>
        </section>
      ) : null}
    </div>
  );
}

function MarketRow({ entry }: { entry: MarketEntry }) {
  return (
    <tr>
      <td>
        <div className="otm-market-model">
          <code>{entry.model}</code>
          <span className="otm-eyebrow">{entry.service}</span>
        </div>
      </td>
      <td>{entry.quoters === 0 ? <span className="otm-eyebrow">—</span> : entry.quoters}</td>
      <td><PriceTripleCell triple={entry.input_per_million} /></td>
      <td><PriceTripleCell triple={entry.cached_input_per_million} /></td>
      <td><PriceTripleCell triple={entry.output_per_million} /></td>
    </tr>
  );
}

function PriceTripleCell({ triple }: { triple: { min: number; median: number; max: number } }) {
  if (triple.min === 0 && triple.max === 0) {
    return <span className="otm-eyebrow">—</span>;
  }
  return (
    <span className="otm-price-triple">
      <strong>{triple.median}</strong>
      <small>
        {triple.min}
        {triple.min !== triple.max ? `–${triple.max}` : ''}
      </small>
    </span>
  );
}
