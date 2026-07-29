'use client';

import { useEffect, useState, type CSSProperties, type FormEvent } from 'react';
import {
  CheckCircle2,
  Link2,
  Loader2,
  Plus,
  RefreshCw,
  ShieldCheck,
  Trash2,
  Wallet as WalletIcon,
  WifiOff,
} from 'lucide-react';
import { useAccount } from '../account-context';
import { tokenManagerConfig } from '../config';
import { formatDate, middleEllipsis } from '../format';
import { getAuthJwt } from '../neon-auth';
import {
  createManageInstance,
  createWalletChallenge,
  deleteManageInstance,
  deleteManageWallet,
  linkManageWallet,
  replaceManageInstanceAcl,
  updateManageInstance,
  type ManageAclRule,
  type ManageInstance,
  type ManageInstanceAccessMode,
  type ManageIdentitySnapshot,
} from '../manage-api';
import {
  computeIdentityFreshness,
  describeRulePlaceholder,
  emptyRule,
  normalizeAclRules,
} from '../instances';
import { signWalletMessage } from '../wallet';

const selectStyle: CSSProperties = {
  minHeight: 36,
  width: '100%',
  border: '1px solid var(--ac-line)',
  borderRadius: 8,
  padding: '0 11px',
  background: 'var(--ac-panel)',
  color: 'var(--ac-ink)',
  fontSize: '0.875rem',
};

const inlineFieldsetStyle: CSSProperties = {
  border: '1px solid var(--ac-line)',
  borderRadius: 12,
  padding: 14,
  margin: 0,
};

const radioGroupStyle: CSSProperties = {
  display: 'flex',
  gap: 12,
  flexWrap: 'wrap',
};

const preStyle: CSSProperties = {
  margin: 0,
  padding: 12,
  borderRadius: 10,
  background: 'var(--ac-band)',
  border: '1px solid var(--ac-line)',
  fontFamily: "ui-monospace, 'SFMono-Regular', Menlo, Consolas, monospace",
  fontSize: '0.8rem',
  overflowX: 'auto',
  whiteSpace: 'pre-wrap',
  wordBreak: 'break-word',
};

const ruleRowStyle: CSSProperties = {
  display: 'grid',
  gridTemplateColumns: 'minmax(150px, 190px) minmax(0, 1fr) auto',
  gap: 10,
  alignItems: 'end',
};

interface LastSignedChallenge {
  wallet: string;
  message: string;
  expires_at: string;
}

function ruleSummary(rules: ManageAclRule[]): string {
  if (rules.length === 0) return 'Owner only';
  return `${rules.length} rule${rules.length === 1 ? '' : 's'}`;
}

function ownershipCopy(instance: ManageInstance): string {
  switch (instance.ownership_status) {
    case 'ownership_mismatch':
      return 'Ownership mismatch';
    case 'ownership_unavailable':
      return 'Verification unavailable';
    default:
      return 'Ownership verified';
  }
}

function ownershipTone(instance: ManageInstance): 'active' | 'revoked' {
  return instance.ownership_status &&
    instance.ownership_status !== 'active' &&
    instance.ownership_status !== 'verified'
    ? 'revoked'
    : 'active';
}

function modeLabel(mode: ManageInstanceAccessMode): string {
  return mode === 'public' ? 'Public' : 'Restricted';
}

function identitySummary(identity: ManageIdentitySnapshot | null): string {
  if (!identity?.email) return 'No verified email on this session';
  return identity.email;
}

function identityDetail(identity: ManageIdentitySnapshot | null): string {
  const freshness = computeIdentityFreshness(identity);
  switch (freshness.state) {
    case 'fresh':
      return freshness.expires_at
        ? `Email rules keep matching until ${formatDate(freshness.expires_at)}`
        : 'Email rules can match while this verified session stays fresh';
    case 'stale':
      return 'Email-domain rules are currently stale and fail closed until you sign in again';
    case 'unverified':
      return 'This session has no verified email, so email-domain rules will not match';
    default:
      return 'Sign out and sign back in before relying on email-domain access rules';
  }
}

function WalletIdentityPanel({
  identity,
  onReauthenticate,
}: {
  identity: ManageIdentitySnapshot | null;
  onReauthenticate: () => void;
}) {
  const freshness = computeIdentityFreshness(identity);

  return (
    <section className="otm-panel">
      <div className="otm-panel-heading">
        <h2>Email identity</h2>
      </div>
      <div style={{ padding: 16, display: 'grid', gap: 12 }}>
        <div className="otm-status-grid" style={{ marginBottom: 0 }}>
          <div className="otm-metric-panel">
            <span>Verified email</span>
            <strong>{identitySummary(identity)}</strong>
            <small>
              {identity?.last_verified_at
                ? `Last refreshed ${formatDate(identity.last_verified_at)}`
                : 'No freshness timestamp yet'}
            </small>
          </div>
          <div className="otm-metric-panel">
            <span>Freshness</span>
            <strong>
              {freshness.state === 'fresh'
                ? 'Fresh'
                : freshness.state === 'stale'
                  ? 'Expired'
                  : freshness.state === 'unverified'
                    ? 'Unverified'
                    : 'Unknown'}
            </strong>
            <small>{identityDetail(identity)}</small>
          </div>
        </div>

        <div
          className="otm-notice"
          role="note"
          style={{ margin: 0, display: 'grid', gap: 10 }}
        >
          <span>
            Email-domain access depends on a recent verified management session.
            When it expires, wallet rules and owner access can still work, but
            email rules fail closed.
          </span>
          <div>
            <button
              type="button"
              className="otm-secondary-button"
              onClick={onReauthenticate}
            >
              Reauthenticate
            </button>
          </div>
        </div>
      </div>
    </section>
  );
}

function ManagedInstanceCard({
  instance,
  onRefreshInstances,
  onRefreshWallets,
}: {
  instance: ManageInstance;
  onRefreshInstances: () => Promise<void>;
  onRefreshWallets: () => Promise<void>;
}) {
  const { showNotice, handleApiError, pending, setPending, releasePending } =
    useAccount();
  const [label, setLabel] = useState(instance.label ?? '');
  const [mode, setMode] = useState<ManageInstanceAccessMode>(instance.mode);
  const [rules, setRules] = useState<ManageAclRule[]>(
    instance.acl_rules.length > 0 ? instance.acl_rules : [emptyRule()],
  );
  const [editorError, setEditorError] = useState<string | null>(null);

  useEffect(() => {
    setLabel(instance.label ?? '');
    setMode(instance.mode);
    setRules(instance.acl_rules.length > 0 ? instance.acl_rules : [emptyRule()]);
    setEditorError(null);
  }, [instance]);

  async function saveInstance(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setEditorError(null);
    setPending(`instance-save-${instance.id}`);

    try {
      const filteredRules = rules.filter((rule) => rule.value.trim() !== '');
      if (filteredRules.length > 100) {
        throw new Error('Restricted ACLs can have at most 100 rules');
      }
      const normalizedRules =
        mode === 'restricted' ? normalizeAclRules(filteredRules) : [];
      const jwt = await getAuthJwt();
      await replaceManageInstanceAcl(
        tokenManagerConfig.apiBaseUrl,
        jwt,
        instance.id,
        { mode, rules: normalizedRules },
      );
      await updateManageInstance(tokenManagerConfig.apiBaseUrl, jwt, instance.id, {
        label: label.trim(),
      });
      await onRefreshInstances();
      showNotice('success', 'Instance policy saved');
    } catch (error) {
      setEditorError(error instanceof Error ? error.message : String(error));
      handleApiError(error);
    } finally {
      releasePending(`instance-save-${instance.id}`);
    }
  }

  async function handleDelete() {
    if (!window.confirm(`Delete managed instance ${instance.peer_id}?`)) return;
    setPending(`instance-delete-${instance.id}`);
    try {
      const jwt = await getAuthJwt();
      await deleteManageInstance(tokenManagerConfig.apiBaseUrl, jwt, instance.id);
      await onRefreshInstances();
      await onRefreshWallets();
      showNotice('success', 'Instance deleted');
    } catch (error) {
      handleApiError(error);
    } finally {
      releasePending(`instance-delete-${instance.id}`);
    }
  }

  return (
    <section className="otm-panel">
      <div className="otm-panel-heading">
        <div>
          <p className="otm-eyebrow">Managed instance</p>
          <h2>{instance.label?.trim() || instance.peer_id}</h2>
        </div>
        <div className="acct-page-actions">
          <span className={`otm-status-pill ${instance.online ? 'active' : 'revoked'}`}>
            {instance.online ? 'Online' : 'Offline'}
          </span>
          <span className={`otm-status-pill ${ownershipTone(instance)}`}>
            {ownershipCopy(instance)}
          </span>
          <span className={`otm-status-pill ${instance.mode === 'public' ? 'active' : 'revoked'}`}>
            {modeLabel(instance.mode)}
          </span>
          <button
            type="button"
            className="otm-icon-button danger"
            onClick={handleDelete}
            disabled={pending === `instance-delete-${instance.id}`}
            title="Delete instance"
          >
            {pending === `instance-delete-${instance.id}` ? (
              <Loader2 className="otm-spin" size={18} />
            ) : (
              <Trash2 size={18} />
            )}
          </button>
        </div>
      </div>

      <div style={{ padding: 16, display: 'grid', gap: 16 }}>
        <div className="otm-status-grid" style={{ marginBottom: 0 }}>
          <div className="otm-metric-panel">
            <span>Peer ID</span>
            <strong className="acct-chip">{middleEllipsis(instance.peer_id, 10, 10)}</strong>
            <small>{instance.peer_id}</small>
          </div>
          <div className="otm-metric-panel">
            <span>Owner wallet</span>
            <strong className="acct-chip">
              {middleEllipsis(instance.owner_wallet, 10, 10)}
            </strong>
            <small>{instance.owner_wallet}</small>
          </div>
          <div className="otm-metric-panel">
            <span>ACL rules</span>
            <strong>{ruleSummary(instance.acl_rules)}</strong>
            <small>
              {instance.policy_revision !== null
                ? `Policy revision ${instance.policy_revision}`
                : 'Revision not reported'}
            </small>
          </div>
          <div className="otm-metric-panel">
            <span>Last ownership check</span>
            <strong>
              {instance.ownership_checked_at
                ? formatDate(instance.ownership_checked_at)
                : 'Unknown'}
            </strong>
            <small>
              {instance.online === false
                ? 'The peer is currently absent from the live mesh'
                : 'Policy changes require a fresh ownership match'}
            </small>
          </div>
        </div>

        <form onSubmit={saveInstance} style={{ display: 'grid', gap: 16 }}>
          <label>
            Label
            <input
              value={label}
              onChange={(event) => setLabel(event.target.value)}
              placeholder="Research cluster A"
              maxLength={100}
            />
          </label>

          <fieldset style={inlineFieldsetStyle}>
            <legend
              style={{
                padding: '0 6px',
                color: 'var(--ac-muted)',
                fontSize: '0.72rem',
                fontWeight: 700,
                letterSpacing: '0.05em',
                textTransform: 'uppercase',
              }}
            >
              Access mode
            </legend>
            <div style={radioGroupStyle}>
              <label
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  fontSize: '0.9rem',
                  textTransform: 'none',
                  letterSpacing: 'normal',
                }}
              >
                <input
                  type="radio"
                  name={`mode-${instance.id}`}
                  checked={mode === 'public'}
                  onChange={() => setMode('public')}
                />
                Public
              </label>
              <label
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  fontSize: '0.9rem',
                  textTransform: 'none',
                  letterSpacing: 'normal',
                }}
              >
                <input
                  type="radio"
                  name={`mode-${instance.id}`}
                  checked={mode === 'restricted'}
                  onChange={() => setMode('restricted')}
                />
                Restricted
              </label>
            </div>
          </fieldset>

          <fieldset style={inlineFieldsetStyle} disabled={mode !== 'restricted'}>
            <legend
              style={{
                padding: '0 6px',
                color: 'var(--ac-muted)',
                fontSize: '0.72rem',
                fontWeight: 700,
                letterSpacing: '0.05em',
                textTransform: 'uppercase',
              }}
            >
              Allow rules
            </legend>
            <div style={{ display: 'grid', gap: 12 }}>
              <p className="acct-page-sub" style={{ marginTop: 0 }}>
                Restricted instances always allow the owner. Add exact email
                domains or exact linked wallets for everyone else.
              </p>

              {rules.map((rule, index) => (
                <div key={`${instance.id}-${index}`} style={ruleRowStyle}>
                  <label>
                    Rule kind
                    <select
                      value={rule.kind}
                      onChange={(event) => {
                        const next = [...rules];
                        next[index] = {
                          kind: event.target.value as ManageAclRule['kind'],
                          value: '',
                        };
                        setRules(next);
                      }}
                      style={selectStyle}
                      aria-label={`Rule ${index + 1} kind`}
                    >
                      <option value="email_domain">Email domain</option>
                      <option value="wallet">Wallet</option>
                    </select>
                  </label>
                  <label>
                    Value
                    <input
                      value={rule.value}
                      onChange={(event) => {
                        const next = [...rules];
                        next[index] = { ...next[index], value: event.target.value };
                        setRules(next);
                      }}
                      placeholder={describeRulePlaceholder(rule.kind)}
                      aria-label={`Rule ${index + 1} value`}
                    />
                  </label>
                  <button
                    type="button"
                    className="otm-icon-button danger"
                    onClick={() => {
                      if (rules.length === 1) {
                        setRules([emptyRule()]);
                        return;
                      }
                      setRules(rules.filter((_, currentIndex) => currentIndex !== index));
                    }}
                    title="Remove rule"
                  >
                    <Trash2 size={18} />
                  </button>
                </div>
              ))}

              <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
                <button
                  type="button"
                  className="otm-secondary-button"
                  onClick={() => {
                    if (rules.length >= 100) {
                      setEditorError('Restricted ACLs can have at most 100 rules');
                      return;
                    }
                    setRules([...rules, emptyRule()]);
                  }}
                >
                  <Plus size={16} />
                  Add rule
                </button>
                <span className="acct-page-sub" style={{ margin: 0 }}>
                  Blank rows are ignored. A restricted instance with zero saved
                  rules stays owner-only.
                </span>
              </div>
            </div>
          </fieldset>

          {editorError ? (
            <div className="otm-notice error" role="alert" style={{ margin: 0 }}>
              {editorError}
            </div>
          ) : null}

          <div className="acct-page-actions">
            <button
              type="submit"
              className="otm-primary-button"
              disabled={pending === `instance-save-${instance.id}`}
            >
              {pending === `instance-save-${instance.id}` ? (
                <Loader2 className="otm-spin" size={16} />
              ) : (
                <ShieldCheck size={16} />
              )}
              Save policy
            </button>
          </div>
        </form>
      </div>
    </section>
  );
}

export default function InstancesView() {
  const {
    neonUser,
    provider,
    wallet,
    connectWallet,
    linkedWallets,
    walletBindingsLoaded,
    walletBindingsError,
    walletIdentity,
    refreshLinkedWallets,
    managedInstances,
    instancesLoaded,
    instancesError,
    refreshManagedInstances,
    showNotice,
    handleApiError,
    pending,
    setPending,
    releasePending,
    signOut,
  } = useAccount();
  const [claimPeerId, setClaimPeerId] = useState('');
  const [claimLabel, setClaimLabel] = useState('');
  const [lastChallenge, setLastChallenge] = useState<LastSignedChallenge | null>(null);
  const [offline, setOffline] = useState(false);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const sync = () => setOffline(!window.navigator.onLine);
    sync();
    window.addEventListener('online', sync);
    window.addEventListener('offline', sync);
    return () => {
      window.removeEventListener('online', sync);
      window.removeEventListener('offline', sync);
    };
  }, []);

  async function refreshAll(announce: boolean) {
    setPending('instances-refresh');
    try {
      await Promise.all([refreshLinkedWallets(), refreshManagedInstances()]);
      if (announce) showNotice('success', 'Instance state refreshed');
    } catch (error) {
      handleApiError(error);
    } finally {
      releasePending('instances-refresh');
    }
  }

  async function handleLinkConnectedWallet() {
    if (!neonUser || !provider || !wallet) return;
    setPending('wallet-link');
    try {
      const jwt = await getAuthJwt();
      const challenge = await createWalletChallenge(
        tokenManagerConfig.apiBaseUrl,
        jwt,
        wallet,
      );
      setLastChallenge({ wallet, message: challenge.message, expires_at: challenge.expires_at });
      const signature = await signWalletMessage(provider, challenge.message);
      await linkManageWallet(tokenManagerConfig.apiBaseUrl, jwt, {
        challenge_id: challenge.id,
        signature,
      });
      await Promise.all([refreshLinkedWallets(), refreshManagedInstances()]);
      showNotice('success', `Linked wallet ${middleEllipsis(wallet)}`);
    } catch (error) {
      handleApiError(error);
    } finally {
      releasePending('wallet-link');
    }
  }

  async function handleDeleteWallet(id: string, walletAddress: string) {
    if (!window.confirm(`Remove linked wallet ${walletAddress}?`)) return;
    setPending(`wallet-delete-${id}`);
    try {
      const jwt = await getAuthJwt();
      await deleteManageWallet(tokenManagerConfig.apiBaseUrl, jwt, id);
      await Promise.all([refreshLinkedWallets(), refreshManagedInstances()]);
      showNotice('success', 'Wallet removed');
    } catch (error) {
      handleApiError(error);
    } finally {
      releasePending(`wallet-delete-${id}`);
    }
  }

  async function handleClaimInstance(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!neonUser) return;
    setPending('instance-claim');
    try {
      const jwt = await getAuthJwt();
      await createManageInstance(tokenManagerConfig.apiBaseUrl, jwt, {
        peer_id: claimPeerId.trim(),
        label: claimLabel.trim() || undefined,
      });
      await refreshManagedInstances();
      setClaimPeerId('');
      setClaimLabel('');
      showNotice('success', 'Instance claimed');
    } catch (error) {
      handleApiError(error);
    } finally {
      releasePending('instance-claim');
    }
  }

  const publicCount = managedInstances.filter((instance) => instance.mode === 'public').length;
  const onlineCount = managedInstances.filter((instance) => instance.online).length;
  const currentWalletLinked =
    wallet !== null && linkedWallets.some((walletBinding) => walletBinding.wallet === wallet);

  return (
    <div className="acct-page">
      <header className="acct-page-head">
        <div>
          <h1>Instances</h1>
          <p className="acct-page-sub">
            Link the wallets you control, claim live peers whose attestation
            names those wallets, and declare whether each instance is public or
            restricted.
          </p>
        </div>
        <div className="acct-page-actions">
          <button
            type="button"
            className="otm-secondary-button"
            onClick={() => {
              refreshAll(true).catch(() => undefined);
            }}
            disabled={pending === 'instances-refresh'}
          >
            {pending === 'instances-refresh' ? (
              <Loader2 className="otm-spin" size={16} />
            ) : (
              <RefreshCw size={16} />
            )}
            Refresh
          </button>
        </div>
      </header>

      {offline ? (
        <div className="otm-notice" role="status">
          <WifiOff size={16} style={{ marginRight: 8, verticalAlign: 'text-bottom' }} />
          Browser offline detected. Cached state may render, but wallet linking
          and ownership checks will fail until the connection returns.
        </div>
      ) : null}

      <section className="otm-status-grid" aria-label="Instance summary">
        <div className="otm-metric-panel">
          <span>Linked wallets</span>
          <strong>{linkedWallets.length}</strong>
          <small>
            {linkedWallets.length === 0
              ? 'No ownership proofs yet'
              : `${linkedWallets.filter((walletBinding) => walletBinding.is_primary).length || 0} primary`}
          </small>
        </div>
        <div className="otm-metric-panel">
          <span>Managed peers</span>
          <strong>{managedInstances.length}</strong>
          <small>
            {managedInstances.length === 0 ? 'No claimed peers yet' : `${onlineCount} online now`}
          </small>
        </div>
        <div className="otm-metric-panel">
          <span>Public peers</span>
          <strong>{publicCount}</strong>
          <small>
            {managedInstances.length - publicCount} restricted
          </small>
        </div>
      </section>

      <WalletIdentityPanel identity={walletIdentity} onReauthenticate={signOut} />

      <section className="otm-panel">
        <div className="otm-panel-heading">
          <h2>Linked wallets</h2>
          {wallet ? (
            <span className="acct-chip" title={wallet}>
              {middleEllipsis(wallet)}
            </span>
          ) : null}
        </div>

        <div style={{ padding: 16, display: 'grid', gap: 16 }}>
          <div className="acct-page-sub" style={{ marginTop: 0 }}>
            To link a wallet, the browser signs the exact UTF-8 message returned
            by `POST /manage/wallets/challenges`; the signature sent to the
            server is the canonical base58 encoding of the Ed25519 signature
            bytes.
          </div>

          <div className="acct-page-actions" style={{ flexWrap: 'wrap' }}>
            {wallet ? (
              <button
                type="button"
                className="otm-primary-button"
                onClick={() => {
                  handleLinkConnectedWallet().catch(() => undefined);
                }}
                disabled={pending === 'wallet-link' || currentWalletLinked}
              >
                {pending === 'wallet-link' ? (
                  <Loader2 className="otm-spin" size={16} />
                ) : currentWalletLinked ? (
                  <CheckCircle2 size={16} />
                ) : (
                  <Link2 size={16} />
                )}
                {currentWalletLinked ? 'Already linked' : 'Link connected wallet'}
              </button>
            ) : (
              <button
                type="button"
                className="otm-primary-button"
                onClick={() => {
                  connectWallet().catch(() => undefined);
                }}
                disabled={!provider || pending === 'connect'}
              >
                {pending === 'connect' ? (
                  <Loader2 className="otm-spin" size={16} />
                ) : (
                  <WalletIcon size={16} />
                )}
                {provider ? 'Connect wallet to link it' : 'Install a Solana wallet'}
              </button>
            )}
          </div>

          {lastChallenge ? (
            <div style={{ display: 'grid', gap: 10 }}>
              <p className="acct-section-label" style={{ marginBottom: 0 }}>
                Exact challenge last signed
              </p>
              <div className="acct-page-sub" style={{ marginTop: 0 }}>
                Wallet {middleEllipsis(lastChallenge.wallet)}. Expires{' '}
                {formatDate(lastChallenge.expires_at)}.
              </div>
              <pre style={preStyle}>{lastChallenge.message}</pre>
            </div>
          ) : null}

          {!walletBindingsLoaded && !walletBindingsError ? (
            <div className="otm-empty-state">Loading linked wallets…</div>
          ) : walletBindingsError ? (
            <div className="otm-notice" role="alert" style={{ margin: 0 }}>
              {walletBindingsError}
            </div>
          ) : linkedWallets.length === 0 ? (
            <div className="otm-empty-state">
              No linked wallets yet. Link at least one attested owner wallet
              before claiming a peer.
            </div>
          ) : (
            <div className="otm-table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Wallet</th>
                    <th>Linked</th>
                    <th>Status</th>
                    <th aria-label="Actions" />
                  </tr>
                </thead>
                <tbody>
                  {linkedWallets.map((walletBinding) => (
                    <tr key={walletBinding.id}>
                      <td>
                        <code>{walletBinding.wallet}</code>
                      </td>
                      <td>
                        {walletBinding.linked_at
                          ? formatDate(walletBinding.linked_at)
                          : 'Unknown'}
                      </td>
                      <td>
                        <span
                          className={`otm-status-pill ${
                            walletBinding.is_primary ? 'active' : 'revoked'
                          }`}
                        >
                          {walletBinding.is_primary ? 'Primary' : 'Linked'}
                        </span>
                      </td>
                      <td>
                        <button
                          type="button"
                          className="otm-icon-button danger"
                          onClick={() => {
                            handleDeleteWallet(walletBinding.id, walletBinding.wallet).catch(
                              () => undefined,
                            );
                          }}
                          disabled={pending === `wallet-delete-${walletBinding.id}`}
                          title="Remove linked wallet"
                        >
                          {pending === `wallet-delete-${walletBinding.id}` ? (
                            <Loader2 className="otm-spin" size={18} />
                          ) : (
                            <Trash2 size={18} />
                          )}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </section>

      <section className="otm-panel">
        <div className="otm-panel-heading">
          <h2>Claim a live peer</h2>
        </div>
        <form className="otm-create-key-form" onSubmit={handleClaimInstance}>
          <label>
            Peer ID
            <input
              value={claimPeerId}
              onChange={(event) => setClaimPeerId(event.target.value)}
              placeholder="12D3KooW..."
              required
            />
          </label>
          <label>
            Label
            <input
              value={claimLabel}
              onChange={(event) => setClaimLabel(event.target.value)}
              placeholder="GPU node in Zurich"
              maxLength={100}
            />
          </label>
          <button
            type="submit"
            className="otm-primary-button"
            disabled={pending === 'instance-claim' || linkedWallets.length === 0}
          >
            {pending === 'instance-claim' ? (
              <Loader2 className="otm-spin" size={16} />
            ) : (
              <Plus size={16} />
            )}
            Claim instance
          </button>
        </form>
      </section>

      <section
        style={{
          display: 'grid',
          gap: 16,
          marginTop: 16,
        }}
      >
        {!instancesLoaded && !instancesError ? (
          <div className="otm-empty-state">Loading managed instances…</div>
        ) : instancesError ? (
          <div className="otm-notice" role="alert">
            {instancesError}
          </div>
        ) : managedInstances.length === 0 ? (
          <div className="otm-empty-state">
            No managed peers yet. Once a linked wallet owns a live peer
            attestation, claim it here and define its access mode.
          </div>
        ) : (
          managedInstances.map((instance) => (
            <ManagedInstanceCard
              key={instance.id}
              instance={instance}
              onRefreshInstances={refreshManagedInstances}
              onRefreshWallets={refreshLinkedWallets}
            />
          ))
        )}
      </section>
    </div>
  );
}
