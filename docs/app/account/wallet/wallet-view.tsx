'use client';

import { useCallback, useEffect, useState } from 'react';
import {
  CheckCircle2,
  Copy,
  Gift,
  KeyRound,
  Loader2,
  LogIn,
  LogOut,
  RefreshCw,
  Send,
  Trash2,
  Wallet as WalletIcon,
} from 'lucide-react';
import { useAccount } from '../account-context';
import { tokenManagerConfig } from '../config';
import {
  buildAuthChallenge,
  createApiKey,
  listApiKeys,
  revokeApiKey,
  type ApiKeyInfo,
  type CreatedApiKey,
} from '../auth-api';
import { getAuthJwt } from '../neon-auth';
import {
  claimFaucet,
  getFaucetStatus,
  type FaucetClaim,
  type FaucetStatus,
} from '../manage-api';
import { formatDate, middleEllipsis, uiAmountToRaw } from '../format';
import {
  buildOtelaTransfer,
  explorerTransactionUrl,
  parsePublicKey,
  sendWalletTransaction,
} from '../solana';
import { signWalletMessage } from '../wallet';

interface SignedSession {
  wallet: string;
  challenge: string;
  signature: string;
  signedAt: string;
}

export default function WalletView() {
  const {
    provider,
    wallet,
    balances,
    connection,
    rpcUrl,
    neonUser,
    linkedWallets,
    connectWallet,
    disconnectWallet,
    refreshBalances,
    applyRpcUrl,
    resetRpcUrl,
    showNotice,
    pending,
    setPending,
    releasePending,
  } = useAccount();

  const [session, setSession] = useState<SignedSession | null>(null);
  const [apiKeys, setApiKeys] = useState<ApiKeyInfo[]>([]);
  const [createdKey, setCreatedKey] = useState<CreatedApiKey | null>(null);
  const [label, setLabel] = useState('');
  const [recipient, setRecipient] = useState('');
  const [amount, setAmount] = useState('');
  const [rpcDraftUrl, setRpcDraftUrl] = useState(rpcUrl);
  const [txSignature, setTxSignature] = useState<string | null>(null);
  const [faucet, setFaucet] = useState<FaucetStatus | null>(null);
  const [faucetClaim, setFaucetClaim] = useState<FaucetClaim | null>(null);

  useEffect(() => {
    setRpcDraftUrl(rpcUrl);
  }, [rpcUrl]);

  const connected = Boolean(wallet);
  const signedIn = Boolean(session);

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
    if (!wallet || !session) return;
    refreshKeys().catch((error: unknown) => {
      showNotice('error', error instanceof Error ? error.message : String(error));
    });
  }, [refreshKeys, session, showNotice, wallet]);

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
      releasePending('signin');
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
      releasePending('create-key');
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
      releasePending(key.key_id);
    }
  }

  async function copyToken() {
    if (!createdKey) return;
    try {
      await navigator.clipboard.writeText(createdKey.token);
      showNotice('success', 'Bearer token copied');
    } catch {
      showNotice(
        'error',
        'Copy failed — select the token and copy it manually',
      );
    }
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
      releasePending('transfer');
    }
  }

  const refreshFaucet = useCallback(async () => {
    if (!neonUser) return;
    const jwt = await getAuthJwt();
    setFaucet(await getFaucetStatus(tokenManagerConfig.apiBaseUrl, jwt));
  }, [neonUser]);

  useEffect(() => {
    if (!neonUser) {
      setFaucet(null);
      setFaucetClaim(null);
      return;
    }
    refreshFaucet().catch((error: unknown) =>
      showNotice('error', error instanceof Error ? error.message : String(error)),
    );
  }, [neonUser, refreshFaucet, showNotice]);

  async function handleFaucetClaim() {
    if (!neonUser) return;
    setPending('faucet');
    setFaucetClaim(null);
    try {
      const jwt = await getAuthJwt();
      const claim = await claimFaucet(tokenManagerConfig.apiBaseUrl, jwt);
      setFaucetClaim(claim);
      setFaucet((prev) => (prev ? { ...prev, claimed: true, claimed_at: new Date().toISOString(), tx_signature: claim.tx_signature, wallet: claim.wallet } : prev));
      await refreshBalances(claim.wallet);
      showNotice('success', `${claim.amount_ui} OTELA claimed`);
    } catch (error) {
      showNotice('error', error instanceof Error ? error.message : String(error));
    } finally {
      releasePending('faucet');
    }
  }

  return (
    <div className="acct-page">
      <header className="acct-page-head">
        <div>
          <h1>Wallet</h1>
          <p className="acct-page-sub">
            Connect a Solana wallet to see balances, send OTELA, and manage keys
            signed by that wallet.
          </p>
        </div>
        <div className="acct-page-actions otm-wallet-actions">
        {wallet ? (
          <>
            <span className="otm-wallet-chip" title={wallet}>
              <WalletIcon size={16} />
              {middleEllipsis(wallet)}
            </span>
            <button
              type="button"
              className="otm-icon-button"
              onClick={() => {
                disconnectWallet().catch(() => undefined);
                setSession(null);
                setApiKeys([]);
                setCreatedKey(null);
              }}
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
            onClick={() => {
              setSession(null);
              setApiKeys([]);
              connectWallet().catch(() => undefined);
            }}
            disabled={!provider || pending === 'connect'}
          >
            {pending === 'connect' ? (
              <Loader2 className="otm-spin" size={16} />
            ) : (
              <WalletIcon size={16} />
            )}
            {provider ? 'Connect wallet' : 'Install a wallet'}
          </button>
        )}
        </div>
      </header>

      <section className="otm-status-grid" aria-label="Wallet status">
        <div className="otm-metric-panel">
          <span>Network</span>
          <strong>{tokenManagerConfig.solanaCluster}</strong>
          <form
            className="otm-rpc-form"
            onSubmit={(event) => {
              event.preventDefault();
              applyRpcUrl(rpcDraftUrl).catch(() => undefined);
            }}
          >
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
              <button type="button" className="otm-text-button" onClick={() => resetRpcUrl()}>
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

      <section className="otm-panel otm-api-panel">
        <div className="otm-panel-heading">
          <div>
            <p className="otm-eyebrow">Access</p>
            <h2>Wallet keys</h2>
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

      <section className="otm-panel otm-faucet-panel">
        <div className="otm-panel-heading">
          <div>
            <p className="otm-eyebrow">Faucet</p>
            <h2>Claim test OTELA</h2>
          </div>
        </div>

        {!neonUser ? (
          <div className="otm-empty-state">
            Sign in with OpenTela to claim from the faucet.
          </div>
        ) : faucet === null ? (
          <div className="otm-empty-state">Loading faucet status…</div>
        ) : !faucet.enabled ? (
          <div className="otm-empty-state">
            The faucet is not enabled on this deployment.
          </div>
        ) : !faucet.email_verified ? (
          <div className="otm-empty-state">
            Verify your email to claim {faucet.amount_ui} OTELA from the
            faucet. Check your inbox for a verification email from Neon Auth.
          </div>
        ) : faucet.claimed || faucetClaim ? (
          <div className="otm-faucet-claimed">
            <CheckCircle2 className="otm-success-icon" size={22} />
            <div>
              <strong>
                {faucetClaim?.amount_ui ?? faucet.amount_ui} OTELA claimed
              </strong>
              <p>
                Sent to{' '}
                <code>{faucetClaim?.wallet ?? faucet.wallet ?? 'your wallet'}</code>
              </p>
              {(faucetClaim?.tx_signature ?? faucet.tx_signature) ? (
                <a
                  className="otm-tx-link"
                  href={explorerTransactionUrl(
                    faucetClaim?.tx_signature ?? faucet.tx_signature ?? '',
                    tokenManagerConfig.solanaCluster,
                  )}
                  target="_blank"
                  rel="noreferrer"
                >
                  View transaction
                </a>
              ) : null}
            </div>
          </div>
        ) : (
          <div className="otm-faucet-claim">
            <p>
              Claim {faucet.amount_ui} OTELA once per verified account. Tokens
              are sent to your{' '}
              {linkedWallets.length > 0 ? 'primary linked wallet' : 'linked wallet'}.
            </p>
            <button
              type="button"
              className="otm-primary-button"
              onClick={handleFaucetClaim}
              disabled={
                pending === 'faucet' ||
                linkedWallets.length === 0 ||
                Boolean(faucet.claimed)
              }
            >
              {pending === 'faucet' ? (
                <Loader2 className="otm-spin" size={16} />
              ) : (
                <Gift size={16} />
              )}
              {linkedWallets.length === 0
                ? 'Link a wallet first'
                : `Claim ${faucet.amount_ui} OTELA`}
            </button>
          </div>
        )}
      </section>
    </div>
  );
}
