'use client';

import { useEffect, useState } from 'react';
import { NeonAuthUIProvider, AuthView } from '@neondatabase/auth-ui';
import { authClient, isNeonConfigured } from '../../account/neon-auth';
import VerifyEmailView from '../../account/verify-email-view';

export default function AuthPathView({ pathname }: { pathname: string }) {
  const [mounted, setMounted] = useState(false);
  // Same escape hatch as the account login card: when sign-in answers "email
  // not verified", the code the server sends needs somewhere to be entered.
  const [verifying, setVerifying] = useState(false);

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

  if (verifying) {
    return (
      <main className="acct-centered">
        {/* This page sits outside AccountProvider, so the session listener
            cannot walk the user over to the console — do it explicitly once
            verification has signed them in. */}
        <VerifyEmailView
          onVerified={() => window.location.assign('/account')}
          onCancel={() => setVerifying(false)}
        />
      </main>
    );
  }

  // Only the credential views can run into an unverified address; offering
  // the code form next to forgot-password or reset-password would be noise.
  const offersVerification = pathname === 'sign-in' || pathname === 'sign-up';

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
        {offersVerification ? (
          <button
            type="button"
            className="acct-back-link acct-back-button"
            onClick={() => setVerifying(true)}
          >
            Have a verification code? Verify your email
          </button>
        ) : null}
      </div>
    </main>
  );
}
