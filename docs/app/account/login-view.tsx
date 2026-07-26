'use client';

import { usePathname } from 'next/navigation';
import { NeonAuthUIProvider, AuthView } from '@neondatabase/auth-ui';
import { authClient } from './neon-auth';

export default function LoginView() {
  const pathname = usePathname();

  if (!authClient) {
    return (
      <main className="acct-centered">
        <div className="acct-card">
          <p className="acct-login-sub">Account sign-in unavailable.</p>
        </div>
      </main>
    );
  }

  // AuthView renders its own titled card ("Sign In" + description), so this
  // wrapper contributes brand only — a second heading/card would duplicate it.
  return (
    <main className="acct-centered">
      <div className="acct-login">
        <div className="acct-login-brand">
          {/* The vendor card's "Sign In" title is not a heading element, so this
              is the page's only h1 — brand text, not a duplicate title. */}
          <h1 className="otm-eyebrow">OpenTela</h1>
          <p className="acct-login-sub">
            Manage your API keys, browse running services, and connect a wallet.
          </p>
        </div>
        <NeonAuthUIProvider authClient={authClient}>
          <AuthView pathname="sign-in" redirectTo={pathname} />
        </NeonAuthUIProvider>
        <a className="acct-back-link" href="/docs">
          ← Back to documentation
        </a>
      </div>
    </main>
  );
}
