'use client';

import { useEffect, useState, type FormEvent } from 'react';
import { resendVerificationEmail, verifyEmailCode } from './neon-auth';

/** Rein the resend button in so a runaway clicker cannot spam inboxes. */
const RESEND_COOLDOWN_SECONDS = 60;

interface Feedback {
  kind: 'info' | 'error';
  message: string;
}

/**
 * The email-verification step of sign-up.
 *
 * Neon Auth is configured with "Verify at Sign-up" using one-time codes: it
 * emails a 6-digit code when a user registers and again whenever an
 * unverified user tries to sign in. Until the code is entered, sign-in is
 * refused server-side, so this card is the only way forward. It serves two
 * entries:
 *
 * - signed-in but unverified (straight after sign-up): the email is known
 *   from the session, and `onSignOut` offers a way back to the login screen;
 * - signed out (from the login card, after the server told them "email not
 *   verified"): they type the address they registered with, and `onCancel`
 *   returns to the sign-in form.
 */
export default function VerifyEmailView({
  email: sessionEmail,
  onVerified,
  onCancel,
  onSignOut,
}: {
  /** Email from the unverified session; omitted when reached signed out. */
  email?: string;
  /** Called when verification also created a session (auto sign-in). */
  onVerified?: () => void;
  onCancel?: () => void;
  onSignOut?: () => void;
}) {
  const [email, setEmail] = useState(sessionEmail ?? '');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState<'verify' | 'resend' | null>(null);
  const [feedback, setFeedback] = useState<Feedback | null>(null);
  const [cooldownUntil, setCooldownUntil] = useState(0);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (cooldownUntil <= Date.now()) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [cooldownUntil]);

  const cooldownLeft = Math.max(0, Math.ceil((cooldownUntil - now) / 1000));
  const targetEmail = (sessionEmail ?? email).trim();

  async function handleVerify(event: FormEvent) {
    event.preventDefault();
    if (!targetEmail || !code.trim() || busy) return;
    setFeedback(null);
    setBusy('verify');
    try {
      const { autoSignedIn } = await verifyEmailCode(targetEmail, code.trim());
      if (autoSignedIn) onVerified?.();
      // With auto sign-in the session listener opens the gate on its own, so
      // this message is only a brief acknowledgement; without it, the user
      // still has to sign in and the message tells them so.
      setFeedback({
        kind: 'info',
        message: autoSignedIn
          ? 'Email verified — signing you in…'
          : 'Email verified! You can now sign in.',
      });
    } catch (error) {
      setFeedback({
        kind: 'error',
        message: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setBusy(null);
    }
  }

  async function handleResend() {
    if (!targetEmail || busy || cooldownLeft > 0) return;
    setFeedback(null);
    setBusy('resend');
    try {
      await resendVerificationEmail(targetEmail);
      setCooldownUntil(Date.now() + RESEND_COOLDOWN_SECONDS * 1000);
      setNow(Date.now());
      setFeedback({
        kind: 'info',
        message: `Verification email sent to ${targetEmail}. It may take a minute to arrive.`,
      });
    } catch (error) {
      setFeedback({
        kind: 'error',
        message: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="acct-card">
      <p className="otm-eyebrow">OpenTela</p>
      <h1>Verify your email</h1>
      <p className="acct-login-sub">
        {sessionEmail ? (
          <>
            We emailed a 6-digit verification code to{' '}
            <strong>{sessionEmail}</strong>. Enter it below to finish signing
            in — the code expires after 15 minutes.
          </>
        ) : (
          <>
            Enter the email address you signed up with and the 6-digit code we
            sent it — the code expires after 15 minutes.
          </>
        )}
      </p>
      <form className="acct-verify-form" onSubmit={handleVerify}>
        {sessionEmail ? null : (
          <label>
            Email
            <input
              type="email"
              autoComplete="email"
              placeholder="you@example.com"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              required
            />
          </label>
        )}
        <label>
          Verification code
          <input
            inputMode="numeric"
            autoComplete="one-time-code"
            placeholder="123456"
            value={code}
            onChange={(event) => setCode(event.target.value)}
            required
          />
        </label>
        {feedback ? (
          <p className={`otm-notice ${feedback.kind}`} role="status">
            {feedback.message}
          </p>
        ) : null}
        <div className="acct-verify-actions">
          <button
            type="submit"
            className="otm-primary-button"
            disabled={busy !== null || !targetEmail || !code.trim()}
          >
            {busy === 'verify' ? 'Verifying…' : 'Verify email'}
          </button>
          <button
            type="button"
            className="otm-secondary-button"
            onClick={handleResend}
            disabled={busy !== null || !targetEmail || cooldownLeft > 0}
          >
            {busy === 'resend'
              ? 'Sending…'
              : cooldownLeft > 0
                ? `Resend code (${cooldownLeft}s)`
                : 'Resend code'}
          </button>
        </div>
      </form>
      {onSignOut ? (
        <button
          type="button"
          className="acct-back-link acct-back-button"
          onClick={onSignOut}
        >
          Sign in with a different account
        </button>
      ) : null}
      {onCancel ? (
        <button
          type="button"
          className="acct-back-link acct-back-button"
          onClick={onCancel}
        >
          ← Back to sign in
        </button>
      ) : null}
    </div>
  );
}
