'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import {
  KeyRound,
  LayoutDashboard,
  LogOut,
  Network,
  Server,
  ShieldCheck,
  Wallet,
} from 'lucide-react';
import { useAccount } from './account-context';

const NAV = [
  { href: '/account', label: 'Overview', Icon: LayoutDashboard },
  { href: '/account/keys', label: 'API keys', Icon: KeyRound },
  { href: '/account/instances', label: 'Instances', Icon: ShieldCheck },
  { href: '/account/regions', label: 'Trusted regions', Icon: Network },
  { href: '/account/services', label: 'Services', Icon: Server },
  { href: '/account/wallet', label: 'Wallet', Icon: Wallet },
];

export default function Sidebar() {
  const pathname = usePathname();
  const { neonUser, signOut, managedInstances } = useAccount();
  const who = neonUser?.email ?? neonUser?.id ?? '';

  return (
    <aside className="acct-sidebar">
      <Link href="/" className="acct-brand">
        <span className="acct-brand-mark" aria-hidden="true">
          OT
        </span>
        OpenTela
      </Link>

      <p className="acct-nav-label">Manage</p>
      <nav className="acct-nav" aria-label="Account sections">
        {NAV.map(({ href, label, Icon }) => {
          const active =
            href === '/account' ? pathname === '/account' : pathname.startsWith(href);
          return (
            <Link
              key={href}
              href={href}
              className={`acct-nav-link${active ? ' active' : ''}`}
              aria-current={active ? 'page' : undefined}
            >
              <Icon size={16} />
              {label === 'Instances' && managedInstances.length > 0
                ? `${label} (${managedInstances.length})`
                : label}
            </Link>
          );
        })}
      </nav>

      <div className="acct-sidebar-footer">
        <p className="acct-nav-label">Resources</p>
        <div className="acct-footer-links">
          <Link href="/docs">Documentation</Link>
          <Link href="/docs/blog">Blog</Link>
          <a
            href="https://github.com/eth-easl/OpenTela"
            target="_blank"
            rel="noopener noreferrer"
          >
            GitHub
          </a>
        </div>
        <div className="acct-user">
          <span title={who}>{who}</span>
          <button
            type="button"
            className="otm-text-button"
            onClick={signOut}
            title="Sign out"
          >
            <LogOut size={14} />
            Sign out
          </button>
        </div>
      </div>
    </aside>
  );
}
