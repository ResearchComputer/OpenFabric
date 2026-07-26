'use client';

import { useEffect, useState } from 'react';
import { NeonAuthUIProvider, AuthView } from '@neondatabase/auth-ui';
import { authClient, isNeonConfigured } from '../../account/neon-auth';

export default function AuthPathView({ pathname }: { pathname: string }) {
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
  }, []);

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

  // authClient is null during SSR, so hold the same placeholder on both the
  // server render and the first client render (no hydration mismatch).
  if (!mounted || !authClient) {
    return (
      <main className="acct-centered">
        <div className="acct-card">
          <p className="acct-login-sub">Loading…</p>
        </div>
      </main>
    );
  }

  return (
    <main className="acct-centered">
      <div className="acct-login">
        <div className="acct-login-brand">
          {/* Vendor card titles are not heading elements; this is the page's h1. */}
          <h1 className="otm-eyebrow">OpenTela</h1>
        </div>
        <NeonAuthUIProvider authClient={authClient}>
          <AuthView pathname={pathname} redirectTo="/account" />
        </NeonAuthUIProvider>
      </div>
    </main>
  );
}
