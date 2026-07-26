'use client';

import { useState } from 'react';
import { Copy, KeyRound, Loader2, RefreshCw, Trash2 } from 'lucide-react';
import { useAccount } from '../account-context';
import { tokenManagerConfig } from '../config';
import { getAuthJwt } from '../neon-auth';
import { createManageKey, revokeManageKey } from '../manage-api';
import { formatDate } from '../format';

export default function KeysView() {
  const {
    neonUser,
    skKeys,
    refreshSkKeys,
    lastCreatedSk,
    setLastCreatedSk,
    showNotice,
    pending,
    setPending,
    releasePending,
    handleApiError,
  } = useAccount();
  const [skLabel, setSkLabel] = useState('');

  async function handleCreateSkKey(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!neonUser) return;
    setPending('create-sk');
    setLastCreatedSk(null);
    try {
      const jwt = await getAuthJwt();
      const created = await createManageKey(
        tokenManagerConfig.apiBaseUrl,
        jwt,
        skLabel.trim() || undefined,
      );
      setLastCreatedSk(created);
      setSkLabel('');
      await refreshSkKeys();
      showNotice('success', 'API key created — copy it now, it is shown once');
    } catch (error) {
      handleApiError(error);
    } finally {
      releasePending('create-sk');
    }
  }

  async function handleRevokeSkKey(id: string) {
    if (!window.confirm(`Revoke API key ${id}?`)) return;
    setPending(`sk-${id}`);
    try {
      const jwt = await getAuthJwt();
      await revokeManageKey(tokenManagerConfig.apiBaseUrl, jwt, id);
      await refreshSkKeys();
      showNotice('success', 'API key revoked');
    } catch (error) {
      handleApiError(error);
    } finally {
      releasePending(`sk-${id}`);
    }
  }

  return (
    <div className="acct-page">
      <header className="acct-page-head">
        <div>
          <h1>API keys</h1>
          <p className="acct-page-sub">
            Keys authenticate your requests to the OpenTela API. A key is shown
            once, when you create it.
          </p>
        </div>
      </header>

      <section className="otm-panel">
        <div className="otm-panel-heading">
          <h2>Your keys</h2>
          <button
            type="button"
            className="otm-icon-button"
            title="Refresh keys"
            onClick={() => {
              refreshSkKeys().catch((error: unknown) => handleApiError(error));
            }}
          >
            <RefreshCw size={18} />
          </button>
        </div>

        <form className="otm-create-key-form" onSubmit={handleCreateSkKey}>
          <label>
            Label
            <input
              value={skLabel}
              onChange={(event) => setSkLabel(event.target.value)}
              placeholder="laptop"
              maxLength={100}
            />
          </label>
          <button
            type="submit"
            className="otm-primary-button"
            disabled={pending === 'create-sk'}
          >
            {pending === 'create-sk' ? (
              <Loader2 className="otm-spin" size={16} />
            ) : (
              <KeyRound size={16} />
            )}
            Create key
          </button>
        </form>

        {lastCreatedSk ? (
          <div className="otm-token-reveal">
            <div>
              <span>New API key (shown once)</span>
              <code>{lastCreatedSk.key}</code>
            </div>
            <button
              type="button"
              className="otm-icon-button"
              title="Copy API key"
              onClick={() => {
                navigator.clipboard
                  .writeText(lastCreatedSk.key)
                  .then(() => showNotice('success', 'API key copied'))
                  .catch(() =>
                    showNotice(
                      'error',
                      'Copy failed — select the key and copy it manually',
                    ),
                  );
              }}
            >
              <Copy size={18} />
            </button>
          </div>
        ) : null}

        <div className="otm-table-wrap">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Prefix</th>
                <th>Created</th>
                <th>Status</th>
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {skKeys.length === 0 ? (
                <tr>
                  <td colSpan={5} className="otm-table-empty">
                    No API keys for this account.
                  </td>
                </tr>
              ) : (
                skKeys.map((key) => (
                  <tr key={key.id}>
                    <td>{key.name || '-'}</td>
                    <td>
                      <code>{key.prefix}</code>
                    </td>
                    <td>{formatDate(key.created_at)}</td>
                    <td>
                      <span
                        className={`otm-status-pill ${
                          key.revoked_at ? 'revoked' : 'active'
                        }`}
                      >
                        {key.revoked_at ? 'Revoked' : 'Active'}
                      </span>
                    </td>
                    <td>
                      <button
                        type="button"
                        className="otm-icon-button danger"
                        onClick={() => handleRevokeSkKey(key.id)}
                        disabled={Boolean(key.revoked_at) || pending === `sk-${key.id}`}
                        title="Revoke API key"
                      >
                        {pending === `sk-${key.id}` ? (
                          <Loader2 className="otm-spin" size={18} />
                        ) : (
                          <Trash2 size={18} />
                        )}
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
