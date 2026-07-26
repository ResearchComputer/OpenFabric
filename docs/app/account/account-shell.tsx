'use client';

import type { ReactNode } from 'react';
import { AccountProvider, useAccount } from './account-context';
import { resolveGate } from './gate';
import { isNeonConfigured } from './neon-auth';
import LoginView from './login-view';
import Sidebar from './sidebar';

export default function AccountShell({ children }: { children: ReactNode }) {
  return (
    <AccountProvider>
      <AccountGate>{children}</AccountGate>
    </AccountProvider>
  );
}

function AccountGate({ children }: { children: ReactNode }) {
  const { mounted, neonUser, sessionLoading, sessionError, retrySession, notice } =
    useAccount();

  if (!isNeonConfigured()) {
    return (
      <main className="acct-centered">
        <div className="acct-card">
          <p className="otm-eyebrow">OpenTela</p>
          <h1>Account</h1>
          <p className="acct-login-sub">
            Account sign-in is not configured on this deployment.
          </p>
        </div>
      </main>
    );
  }

  const gate = resolveGate({
    mounted,
    sessionLoading,
    sessionError,
    hasUser: Boolean(neonUser),
  });

  // Until mounted, server and client render the same placeholder (no hydration mismatch).
  if (gate === 'loading') {
    return (
      <main className="acct-centered">
        <div className="acct-card">
          <p className="acct-login-sub">Loading…</p>
        </div>
      </main>
    );
  }

  // Asking for the password again would be a lie: the session was never checked.
  if (gate === 'unreachable') {
    return (
      <main className="acct-centered">
        <div className="acct-card">
          <p className="otm-eyebrow">OpenTela</p>
          <h1>Can’t check your sign-in</h1>
          <p className="acct-login-sub">{sessionError}</p>
          <p className="acct-login-sub">
            This is a connection problem, not a sign-out — your session may still
            be valid.
          </p>
          <button type="button" className="otm-primary-button" onClick={retrySession}>
            Try again
          </button>
          <a className="acct-back-link" href="/docs">
            ← Back to documentation
          </a>
        </div>
      </main>
    );
  }

  if (gate === 'signed-out') return <LoginView />;

  return (
    <div className="acct-shell">
      <Sidebar />
      <main className="acct-main">
        {notice ? (
          <div className="acct-notice-wrap">
            <div className={`otm-notice ${notice.kind}`} role="status">
              {notice.message}
            </div>
          </div>
        ) : null}
        {children}
      </main>
    </div>
  );
}
