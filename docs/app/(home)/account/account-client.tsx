'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Buffer } from 'buffer';
import {
  CheckCircle2,
  Copy,
  KeyRound,
  Loader2,
  LogIn,
  LogOut,
  RefreshCw,
  Send,
  Trash2,
  Wallet,
} from 'lucide-react';
import { PublicKey } from '@solana/web3.js';
import '@neondatabase/auth-ui/css';
import { NeonAuthUIProvider, AuthView } from '@neondatabase/auth-ui';
import { tokenManagerConfig } from './config';
import {
  buildAuthChallenge,
  createApiKey,
  listApiKeys,
  revokeApiKey,
  type ApiKeyInfo,
  type CreatedApiKey,
} from './auth-api';
import { formatDate, middleEllipsis, uiAmountToRaw } from './format';
import {
  createManageKey,
  listManageKeys,
  revokeManageKey,
  type ManageKey,
  type CreatedManageKey,
} from './manage-api';
import { authClient, isNeonConfigured, getAuthJwt } from './neon-auth';
import { readLink, writeLink, clearLink, type AccountLink } from './link';
import { listModels, type ServiceModel } from './services';
import {
  buildOtelaTransfer,
  createConnection,
  explorerTransactionUrl,
  getWalletBalances,
  parsePublicKey,
  sendWalletTransaction,
  type WalletBalances,
} from './solana';
import {
  getInjectedWallet,
  publicKeyString,
  signWalletMessage,
  type SolanaWalletProvider,
} from './wallet';

if (typeof globalThis !== 'undefined' && !('Buffer' in globalThis)) {
  (globalThis as typeof globalThis & { Buffer: typeof Buffer }).Buffer = Buffer;
}

type NoticeKind = 'info' | 'success' | 'error';

interface Notice {
  kind: NoticeKind;
  message: string;
}

interface SignedSession {
  wallet: string;
  challenge: string;
  signature: string;
  signedAt: string;
}

export default function AccountClient() {
  const [provider, setProvider] = useState<SolanaWalletProvider | null>(null);
  const [wallet, setWallet] = useState<string | null>(null);
  const [session, setSession] = useState<SignedSession | null>(null);
  const [balances, setBalances] = useState<WalletBalances | null>(null);
  const [apiKeys, setApiKeys] = useState<ApiKeyInfo[]>([]);
  const [createdKey, setCreatedKey] = useState<CreatedApiKey | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [label, setLabel] = useState('');
  const [recipient, setRecipient] = useState('');
  const [amount, setAmount] = useState('');
  const [rpcUrl, setRpcUrl] = useState(tokenManagerConfig.solanaRpcUrl);
  const [rpcDraftUrl, setRpcDraftUrl] = useState(tokenManagerConfig.solanaRpcUrl);
  const [pending, setPending] = useState<string | null>(null);
  const [txSignature, setTxSignature] = useState<string | null>(null);
  const [neonUser, setNeonUser] = useState<{ id: string; email?: string } | null>(null);
  const [mounted, setMounted] = useState(false);
  const [skKeys, setSkKeys] = useState<ManageKey[]>([]);
  const [lastCreatedSk, setLastCreatedSk] = useState<CreatedManageKey | null>(null);
  const [skLabel, setSkLabel] = useState('');
  const [models, setModels] = useState<ServiceModel[]>([]);
  const [servicesKey, setServicesKey] = useState('');
  const [accountLink, setAccountLink] = useState<AccountLink | null>(null);

  useEffect(() => {
    setAccountLink(readLink());
  }, []);

  const canLink = Boolean(wallet && neonUser);
  const isLinked = Boolean(
    accountLink && wallet === accountLink.wallet && neonUser?.id === accountLink.neonUserId,
  );

  const connection = useMemo(
    () => createConnection(rpcUrl),
    [rpcUrl],
  );

  useEffect(() => {
    const injected = getInjectedWallet();
    setProvider(injected);
    if (injected) setWallet(publicKeyString(injected));

    const savedRpcUrl = window.localStorage.getItem('opentela-token-manager-rpc');
    if (savedRpcUrl) {
      setRpcUrl(savedRpcUrl);
      setRpcDraftUrl(savedRpcUrl);
    }
  }, []);

  useEffect(() => {
    setMounted(true);
  }, []);

  useEffect(() => {
    if (!mounted || !authClient) return;
    authClient
      .getSession()
      .then((res: { data?: { user?: { id: string; email?: string } } | null }) => {
        if (res?.data?.user) {
          setNeonUser({ id: res.data.user.id, email: res.data.user.email });
        }
      })
      .catch(() => undefined);
  }, [mounted]);

  const showNotice = useCallback((kind: NoticeKind, message: string) => {
    setNotice({ kind, message });
  }, []);

  const refreshBalances = useCallback(
    async (walletAddress = wallet) => {
      if (!walletAddress) return;
      const owner = new PublicKey(walletAddress);
      const nextBalances = await getWalletBalances({
        connection,
        owner,
        mint: tokenManagerConfig.otelaMint,
        tokenProgramId: tokenManagerConfig.tokenProgramId,
        rpcUrl,
      });
      setBalances(nextBalances);
    },
    [connection, rpcUrl, wallet],
  );

  const refreshKeys = useCallback(
    async (walletAddress = wallet) => {
      if (!walletAddress || !session) return;
      const keys = await listApiKeys(
        tokenManagerConfig.authApiBaseUrl,
        walletAddress,
      );
      setApiKeys(keys);
    },
    [session, wallet],
  );

  useEffect(() => {
    if (!wallet) return;
    refreshBalances().catch((error: unknown) => {
      showNotice('error', error instanceof Error ? error.message : String(error));
    });
  }, [refreshBalances, showNotice, wallet]);

  useEffect(() => {
    if (!wallet || !session) return;
    refreshKeys().catch((error: unknown) => {
      showNotice('error', error instanceof Error ? error.message : String(error));
    });
  }, [refreshKeys, session, showNotice, wallet]);

  const refreshSkKeys = useCallback(async () => {
    if (!neonUser) return;
    const jwt = await getAuthJwt();
    setSkKeys(await listManageKeys(tokenManagerConfig.apiBaseUrl, jwt));
  }, [neonUser]);

  useEffect(() => {
    if (!neonUser) {
      setSkKeys([]);
      return;
    }
    refreshSkKeys().catch((e: unknown) =>
      showNotice('error', e instanceof Error ? e.message : String(e)),
    );
  }, [neonUser, refreshSkKeys, showNotice]);

  useEffect(() => {
    if (lastCreatedSk?.key) setServicesKey(lastCreatedSk.key);
  }, [lastCreatedSk]);

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
    } catch (e) {
      showNotice('error', e instanceof Error ? e.message : String(e));
    } finally {
      setPending(null);
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
    } catch (e) {
      showNotice('error', e instanceof Error ? e.message : String(e));
    } finally {
      setPending(null);
    }
  }

  async function handleLoadServices(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!servicesKey.trim()) return;
    setPending('services');
    try {
      setModels(await listModels(tokenManagerConfig.apiBaseUrl, servicesKey.trim()));
      showNotice('success', 'Services loaded');
    } catch (e) {
      setModels([]);
      showNotice('error', e instanceof Error ? e.message : String(e));
    } finally {
      setPending(null);
    }
  }

  async function connectWallet() {
    if (!provider) {
      showNotice('error', 'No Solana browser wallet detected');
      return;
    }
    setPending('connect');
    try {
      const result = await provider.connect();
      const nextWallet = result.publicKey.toBase58();
      setWallet(nextWallet);
      setSession(null);
      setApiKeys([]);
      await refreshBalances(nextWallet);
      showNotice('success', 'Wallet connected');
    } catch (error) {
      showNotice('error', error instanceof Error ? error.message : String(error));
    } finally {
      setPending(null);
    }
  }

  async function disconnectWallet() {
    if (!provider) return;
    setPending('disconnect');
    try {
      await provider.disconnect();
      setWallet(null);
      setSession(null);
      setBalances(null);
      setApiKeys([]);
      setCreatedKey(null);
      showNotice('info', 'Wallet disconnected');
    } catch (error) {
      showNotice('error', error instanceof Error ? error.message : String(error));
    } finally {
      setPending(null);
    }
  }

  function handleNeonSignOut() {
    authClient?.signOut().finally(() => setNeonUser(null));
  }

  function handleLink() {
    if (!wallet || !neonUser) return;
    setAccountLink(writeLink({ wallet, neonUserId: neonUser.id }));
    showNotice('success', 'Wallet and account linked in this browser');
  }

  function handleUnlink() {
    clearLink();
    setAccountLink(null);
    showNotice('info', 'Link removed');
  }

  async function signIn() {
    if (!provider || !wallet) return;
    setPending('signin');
    try {
      const challenge = buildAuthChallenge(wallet);
      const signature = await signWalletMessage(provider, challenge);
      setSession({
        wallet,
        challenge,
        signature,
        signedAt: new Date().toISOString(),
      });
      showNotice('success', 'Wallet proof signed');
    } catch (error) {
      showNotice('error', error instanceof Error ? error.message : String(error));
    } finally {
      setPending(null);
    }
  }

  async function handleCreateKey(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!provider || !wallet || !session) return;
    setPending('create-key');
    setCreatedKey(null);
    try {
      const challenge = buildAuthChallenge(wallet);
      const signature = await signWalletMessage(provider, challenge);
      const key = await createApiKey(tokenManagerConfig.authApiBaseUrl, {
        wallet,
        challenge,
        signature,
        label: label.trim(),
      });
      setCreatedKey(key);
      setLabel('');
      await refreshKeys(wallet);
      showNotice('success', 'API key created');
    } catch (error) {
      showNotice('error', error instanceof Error ? error.message : String(error));
    } finally {
      setPending(null);
    }
  }

  async function handleRevokeKey(key: ApiKeyInfo) {
    if (!wallet) return;
    const ok = window.confirm(`Revoke API key ${key.key_id}?`);
    if (!ok) return;

    setPending(key.key_id);
    try {
      await revokeApiKey(tokenManagerConfig.authApiBaseUrl, key.key_id, wallet);
      await refreshKeys(wallet);
      showNotice('success', 'API key revoked');
    } catch (error) {
      showNotice('error', error instanceof Error ? error.message : String(error));
    } finally {
      setPending(null);
    }
  }

  async function copyToken() {
    if (!createdKey) return;
    await navigator.clipboard.writeText(createdKey.token);
    showNotice('success', 'Bearer token copied');
  }

  async function handleTransfer(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!provider || !wallet) return;
    setPending('transfer');
    setTxSignature(null);
    try {
      const owner = parsePublicKey(wallet, 'wallet');
      const to = parsePublicKey(recipient, 'recipient');
      const rawAmount = uiAmountToRaw(amount, tokenManagerConfig.otelaDecimals);
      const built = await buildOtelaTransfer({
        connection,
        owner,
        recipient: to,
        mint: tokenManagerConfig.otelaMint,
        amountRaw: rawAmount,
        decimals: tokenManagerConfig.otelaDecimals,
        tokenProgramId: tokenManagerConfig.tokenProgramId,
      });
      const signature = await sendWalletTransaction({ connection, provider, built });
      setTxSignature(signature);
      setRecipient('');
      setAmount('');
      await refreshBalances(wallet);
      showNotice('success', 'Transfer confirmed');
    } catch (error) {
      showNotice('error', error instanceof Error ? error.message : String(error));
    } finally {
      setPending(null);
    }
  }

  async function applyRpcUrl(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const nextRpcUrl = rpcDraftUrl.trim();

    try {
      const parsed = new URL(nextRpcUrl);
      if (!['http:', 'https:'].includes(parsed.protocol)) {
        throw new Error('RPC URL must start with http:// or https://');
      }
    } catch (error) {
      showNotice('error', error instanceof Error ? error.message : 'Invalid RPC URL');
      return;
    }

    window.localStorage.setItem('opentela-token-manager-rpc', nextRpcUrl);
    setRpcUrl(nextRpcUrl);
    setBalances(null);
    showNotice('info', 'RPC endpoint updated');

    if (wallet) {
      setPending('refresh-balances');
      try {
        const owner = new PublicKey(wallet);
        const nextBalances = await getWalletBalances({
          connection: createConnection(nextRpcUrl),
          owner,
          mint: tokenManagerConfig.otelaMint,
          tokenProgramId: tokenManagerConfig.tokenProgramId,
          rpcUrl: nextRpcUrl,
        });
        setBalances(nextBalances);
      } catch (error) {
        showNotice('error', error instanceof Error ? error.message : String(error));
      } finally {
        setPending(null);
      }
    }
  }

  async function resetRpcUrl() {
    window.localStorage.removeItem('opentela-token-manager-rpc');
    setRpcDraftUrl(tokenManagerConfig.solanaRpcUrl);
    setRpcUrl(tokenManagerConfig.solanaRpcUrl);
    setBalances(null);
    showNotice('info', 'RPC endpoint reset to the site default');
  }

  const connected = Boolean(wallet);
  const signedIn = Boolean(session);

  return (
    <main className="otm-shell">
      <header className="otm-topbar">
        <div>
          <p className="otm-eyebrow">OpenTela</p>
          <h1>Account</h1>
        </div>
        <div className="otm-wallet-actions">
          {wallet ? (
            <>
              <span className="otm-wallet-chip" title={wallet}>
                <Wallet size={16} />
                {middleEllipsis(wallet)}
              </span>
              <button
                type="button"
                className="otm-icon-button"
                onClick={disconnectWallet}
                title="Disconnect wallet"
                disabled={pending === 'disconnect'}
              >
                {pending === 'disconnect' ? (
                  <Loader2 className="otm-spin" size={18} />
                ) : (
                  <LogOut size={18} />
                )}
              </button>
            </>
          ) : (
            <button
              type="button"
              className="otm-primary-button"
              onClick={connectWallet}
              disabled={!provider || pending === 'connect'}
            >
              {pending === 'connect' ? (
                <Loader2 className="otm-spin" size={16} />
              ) : (
                <Wallet size={16} />
              )}
              {provider ? 'Connect' : 'Install Wallet'}
            </button>
          )}
          {isLinked ? (
            <span
              className="otm-wallet-chip"
              title="Local link only — not a verified binding"
            >
              Linked
              <button
                type="button"
                className="otm-text-button"
                onClick={handleUnlink}
              >
                Unlink
              </button>
            </span>
          ) : canLink ? (
            <button
              type="button"
              className="otm-secondary-button"
              onClick={handleLink}
            >
              Link
            </button>
          ) : null}
        </div>
      </header>

      {notice ? (
        <div className={`otm-notice ${notice.kind}`} role="status">
          {notice.message}
        </div>
      ) : null}

      {canLink || isLinked ? (
        <p className="otm-eyebrow">
          Linking pairs your wallet and account in this browser only — it is a
          local convenience, not a server-verified binding.
        </p>
      ) : null}

      <section className="otm-status-grid" aria-label="Wallet status">
        <div className="otm-metric-panel">
          <span>Network</span>
          <strong>{tokenManagerConfig.solanaCluster}</strong>
          <form className="otm-rpc-form" onSubmit={applyRpcUrl}>
            <label>
              RPC URL
              <input
                value={rpcDraftUrl}
                onChange={(event) => setRpcDraftUrl(event.target.value)}
                spellCheck={false}
              />
            </label>
            <div className="otm-rpc-actions">
              <button
                type="submit"
                className="otm-secondary-button"
                disabled={pending === 'refresh-balances'}
              >
                Apply
              </button>
              <button type="button" className="otm-text-button" onClick={resetRpcUrl}>
                Reset
              </button>
            </div>
          </form>
        </div>
        <div className="otm-metric-panel">
          <span>SOL</span>
          <strong>{balances ? balances.sol.toFixed(4) : connected ? '...' : '-'}</strong>
          <small>Fee balance</small>
        </div>
        <div className="otm-metric-panel accent">
          <span>OTELA</span>
          <strong>{balances ? balances.otela : connected ? '...' : '-'}</strong>
          <small title={tokenManagerConfig.otelaMint.toBase58()}>
            {middleEllipsis(tokenManagerConfig.otelaMint.toBase58())}
          </small>
        </div>
        <div className="otm-metric-panel">
          <span>Rewards</span>
          <strong>{tokenManagerConfig.rewardCampaign ? 'Campaign' : 'Program'}</strong>
          <dl className="otm-compact-detail-list">
            <div>
              <dt>Program</dt>
              <dd title={tokenManagerConfig.rewardProgramId.toBase58()}>
                {middleEllipsis(tokenManagerConfig.rewardProgramId.toBase58(), 8, 8)}
              </dd>
            </div>
            <div>
              <dt>Campaign</dt>
              <dd title={tokenManagerConfig.rewardCampaign?.toBase58() ?? ''}>
                {tokenManagerConfig.rewardCampaign
                  ? middleEllipsis(tokenManagerConfig.rewardCampaign.toBase58(), 8, 8)
                  : '-'}
              </dd>
            </div>
          </dl>
        </div>
      </section>

      <div className="otm-workspace">
        <section className="otm-panel">
          <div className="otm-panel-heading">
            <div>
              <p className="otm-eyebrow">Account</p>
              <h2>Sign in</h2>
            </div>
            {neonUser ? (
              <button type="button" className="otm-text-button" onClick={handleNeonSignOut}>
                Sign out
              </button>
            ) : null}
          </div>

          {!isNeonConfigured() ? (
            <div className="otm-empty-state">
              Account sign-in is not configured on this deployment.
            </div>
          ) : !mounted ? (
            <div className="otm-empty-state">Loading…</div>
          ) : neonUser ? (
            <>
              <p className="otm-eyebrow">
                Signed in as {neonUser.email ?? neonUser.id}
              </p>
              <form className="otm-create-key-form" onSubmit={handleCreateSkKey}>
                <label>
                  Label
                  <input
                    value={skLabel}
                    onChange={(e) => setSkLabel(e.target.value)}
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
                  Create sk- key
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
                      navigator.clipboard.writeText(lastCreatedSk.key);
                      showNotice('success', 'API key copied');
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
                      skKeys.map((k) => (
                        <tr key={k.id}>
                          <td>{k.name || '-'}</td>
                          <td>
                            <code>{k.prefix}</code>
                          </td>
                          <td>{formatDate(k.created_at)}</td>
                          <td>
                            <span
                              className={`otm-status-pill ${
                                k.revoked_at ? 'revoked' : 'active'
                              }`}
                            >
                              {k.revoked_at ? 'Revoked' : 'Active'}
                            </span>
                          </td>
                          <td>
                            <button
                              type="button"
                              className="otm-icon-button danger"
                              onClick={() => handleRevokeSkKey(k.id)}
                              disabled={Boolean(k.revoked_at) || pending === `sk-${k.id}`}
                              title="Revoke API key"
                            >
                              {pending === `sk-${k.id}` ? (
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
            </>
          ) : authClient ? (
            <NeonAuthUIProvider authClient={authClient}>
              <AuthView pathname="sign-in" />
            </NeonAuthUIProvider>
          ) : (
            <div className="otm-empty-state">Account sign-in unavailable.</div>
          )}
        </section>

        <section className="otm-panel otm-api-panel">
          <div className="otm-panel-heading">
            <div>
              <p className="otm-eyebrow">Access</p>
              <h2>Wallet Keys</h2>
            </div>
            <button
              type="button"
              className="otm-icon-button"
              onClick={() => {
                refreshKeys().catch((error: unknown) =>
                  showNotice(
                    'error',
                    error instanceof Error ? error.message : String(error),
                  ),
                );
              }}
              title="Refresh API keys"
              disabled={!signedIn}
            >
              <RefreshCw size={18} />
            </button>
          </div>

          {!connected ? (
            <div className="otm-empty-state">Connect a Solana wallet to manage API keys.</div>
          ) : !signedIn ? (
            <div className="otm-inline-auth">
              <div>
                <strong>Wallet proof required</strong>
                <p>Sign an OpenTela challenge before managing bearer tokens.</p>
              </div>
              <button
                type="button"
                className="otm-primary-button"
                onClick={signIn}
                disabled={pending === 'signin'}
              >
                {pending === 'signin' ? (
                  <Loader2 className="otm-spin" size={16} />
                ) : (
                  <LogIn size={16} />
                )}
                Sign In
              </button>
            </div>
          ) : (
            <>
              <form className="otm-create-key-form" onSubmit={handleCreateKey}>
                <label>
                  Label
                  <input
                    value={label}
                    onChange={(event) => setLabel(event.target.value)}
                    placeholder="notebook"
                    maxLength={80}
                  />
                </label>
                <button
                  type="submit"
                  className="otm-primary-button"
                  disabled={pending === 'create-key'}
                >
                  {pending === 'create-key' ? (
                    <Loader2 className="otm-spin" size={16} />
                  ) : (
                    <KeyRound size={16} />
                  )}
                  Create
                </button>
              </form>

              {createdKey ? (
                <div className="otm-token-reveal">
                  <div>
                    <span>New bearer token</span>
                    <code>{createdKey.token}</code>
                  </div>
                  <button
                    type="button"
                    className="otm-icon-button"
                    onClick={copyToken}
                    title="Copy bearer token"
                  >
                    <Copy size={18} />
                  </button>
                </div>
              ) : null}

              <div className="otm-table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Label</th>
                      <th>Key ID</th>
                      <th>Created</th>
                      <th>Status</th>
                      <th aria-label="Actions" />
                    </tr>
                  </thead>
                  <tbody>
                    {apiKeys.length === 0 ? (
                      <tr>
                        <td colSpan={5} className="otm-table-empty">
                          No API keys for this wallet.
                        </td>
                      </tr>
                    ) : (
                      apiKeys.map((key) => (
                        <tr key={key.key_id}>
                          <td>{key.label || '-'}</td>
                          <td>
                            <code>{key.key_id}</code>
                          </td>
                          <td>{formatDate(key.created_at)}</td>
                          <td>
                            <span
                              className={`otm-status-pill ${
                                key.revoked ? 'revoked' : 'active'
                              }`}
                            >
                              {key.revoked ? 'Revoked' : 'Active'}
                            </span>
                          </td>
                          <td>
                            <button
                              type="button"
                              className="otm-icon-button danger"
                              onClick={() => handleRevokeKey(key)}
                              disabled={key.revoked || pending === key.key_id}
                              title="Revoke API key"
                            >
                              {pending === key.key_id ? (
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
            </>
          )}
        </section>

        <section className="otm-panel otm-transfer-panel">
          <div className="otm-panel-heading">
            <div>
              <p className="otm-eyebrow">OTELA</p>
              <h2>Transfer</h2>
            </div>
            {txSignature ? <CheckCircle2 className="otm-success-icon" size={22} /> : null}
          </div>

          <form className="otm-transfer-form" onSubmit={handleTransfer}>
            <label>
              Recipient
              <input
                value={recipient}
                onChange={(event) => setRecipient(event.target.value)}
                placeholder="Solana address"
                disabled={!connected}
              />
            </label>
            <label>
              Amount
              <input
                value={amount}
                onChange={(event) => setAmount(event.target.value)}
                placeholder="0.00"
                inputMode="decimal"
                disabled={!connected}
              />
            </label>
            <button
              type="submit"
              className="otm-primary-button otm-send-button"
              disabled={!connected || pending === 'transfer'}
            >
              {pending === 'transfer' ? (
                <Loader2 className="otm-spin" size={16} />
              ) : (
                <Send size={16} />
              )}
              Send OTELA
            </button>
          </form>

          {balances?.ata ? (
            <dl className="otm-detail-list">
              <div>
                <dt>Associated token account</dt>
                <dd title={balances.ata}>{middleEllipsis(balances.ata, 8, 8)}</dd>
              </div>
              <div>
                <dt>Token program</dt>
                <dd title={tokenManagerConfig.tokenProgramId.toBase58()}>
                  {middleEllipsis(tokenManagerConfig.tokenProgramId.toBase58(), 8, 8)}
                </dd>
              </div>
            </dl>
          ) : null}

          {txSignature ? (
            <a
              className="otm-tx-link"
              href={explorerTransactionUrl(
                txSignature,
                tokenManagerConfig.solanaCluster,
              )}
              target="_blank"
              rel="noreferrer"
            >
              View transaction
            </a>
          ) : null}
        </section>

        <section className="otm-panel">
          <div className="otm-panel-heading">
            <div>
              <p className="otm-eyebrow">Permissionless</p>
              <h2>Running Services</h2>
            </div>
          </div>
          <form className="otm-create-key-form" onSubmit={handleLoadServices}>
            <label>
              API key
              <input
                value={servicesKey}
                onChange={(e) => setServicesKey(e.target.value)}
                placeholder="sk-…"
                spellCheck={false}
              />
            </label>
            <button
              type="submit"
              className="otm-primary-button"
              disabled={pending === 'services'}
            >
              {pending === 'services' ? (
                <Loader2 className="otm-spin" size={16} />
              ) : (
                <RefreshCw size={16} />
              )}
              Load
            </button>
          </form>
          {models.length === 0 ? (
            <div className="otm-empty-state">
              Enter an API key and load to list running models (needs a valid key; may be
              blocked by CORS from some origins).
            </div>
          ) : (
            <div className="otm-table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Model</th>
                    <th>Endpoint</th>
                    <th aria-label="curl" />
                  </tr>
                </thead>
                <tbody>
                  {models.map((m) => {
                    const curl = `curl ${tokenManagerConfig.apiBaseUrl}/v1/chat/completions \\\n  -H "Authorization: Bearer ${servicesKey.trim()}" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"${m.id}","messages":[{"role":"user","content":"hello"}]}'`;
                    return (
                      <tr key={m.id}>
                        <td>
                          <code>{m.id}</code>
                        </td>
                        <td>
                          <code>/v1/chat/completions</code>
                        </td>
                        <td>
                          <button
                            type="button"
                            className="otm-icon-button"
                            title="Copy curl"
                            onClick={() => {
                              navigator.clipboard.writeText(curl);
                              showNotice('success', 'curl copied');
                            }}
                          >
                            <Copy size={18} />
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>
    </main>
  );
}
