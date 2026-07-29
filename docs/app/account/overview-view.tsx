'use client';

import Link from 'next/link';
import { useAccount } from './account-context';
import { middleEllipsis } from './format';

export default function OverviewView() {
  const { neonUser, wallet, balances, skKeys, linkedWallets, managedInstances } =
    useAccount();
  const activeKeys = skKeys.filter((key) => !key.revoked_at).length;
  const onlineInstances = managedInstances.filter((instance) => instance.online).length;

  return (
    <div className="acct-page">
      <header className="acct-page-head">
        <div>
          <h1>Overview</h1>
          <p className="acct-page-sub">
            Your keys, the models they can reach, and the wallet they settle
            against.
          </p>
        </div>
        <div className="acct-page-actions">
          <Link href="/account/keys" className="otm-primary-button">
            Create key
          </Link>
        </div>
      </header>

      <p className="acct-section-label">Account</p>
      <section className="otm-status-grid" aria-label="Account summary">
        <div className="otm-metric-panel">
          <span>Signed in as</span>
          <strong>{neonUser?.email ?? neonUser?.id ?? '—'}</strong>
          <small>OpenTela account</small>
        </div>
        <div className="otm-metric-panel">
          <span>Active keys</span>
          <strong>{activeKeys}</strong>
          <small>{activeKeys === 1 ? 'key in use' : 'keys in use'}</small>
        </div>
        <div className="otm-metric-panel">
          <span>Managed instances</span>
          <strong>{managedInstances.length}</strong>
          <small>
            {managedInstances.length === 0
              ? 'No claimed peers yet'
              : `${onlineInstances} online now`}
          </small>
        </div>
        <div className="otm-metric-panel accent">
          <span>OTELA</span>
          <strong>{wallet ? (balances ? balances.otela : '…') : '—'}</strong>
          <small>{wallet ? middleEllipsis(wallet) : 'No wallet connected'}</small>
        </div>
        <div className="otm-metric-panel">
          <span>Linked wallets</span>
          <strong>{linkedWallets.length}</strong>
          <small>
            {linkedWallets.length === 0
              ? 'Link a wallet to claim peers'
              : linkedWallets.some((walletBinding) => walletBinding.is_primary)
                ? 'Primary wallet is set'
                : 'Wallet proofs ready'}
          </small>
        </div>
      </section>

      <p className="acct-section-label">Go to</p>
      <section className="acct-quicklinks" aria-label="Sections">
        <Link href="/account/keys" className="acct-quicklink">
          <strong>API keys</strong>
          <span>Create and revoke keys for the API</span>
        </Link>
        <Link href="/account/instances" className="acct-quicklink">
          <strong>Instances</strong>
          <span>
            {managedInstances.length === 0
              ? 'Link wallets and claim peers'
              : `${managedInstances.length} managed peer${managedInstances.length === 1 ? '' : 's'}`}
          </span>
        </Link>
        <Link href="/account/services" className="acct-quicklink">
          <strong>Services</strong>
          <span>Browse models and copy a request</span>
        </Link>
        <Link href="/account/wallet" className="acct-quicklink">
          <strong>Wallet</strong>
          <span>Balances, transfers and wallet keys</span>
        </Link>
      </section>
    </div>
  );
}
