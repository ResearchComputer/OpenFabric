'use client';

import { createAuthClient } from '@neondatabase/neon-js/auth';
import { BetterAuthReactAdapter } from '@neondatabase/neon-js/auth/react';
import { tokenManagerConfig } from './config';

export function isNeonConfigured(): boolean {
  return Boolean(tokenManagerConfig.neonAuthUrl);
}

export const authClient =
  isNeonConfigured() && typeof window !== 'undefined'
    ? createAuthClient(tokenManagerConfig.neonAuthUrl, {
        adapter: BetterAuthReactAdapter(),
      })
    : null;

/**
 * Thrown when the user has no usable session. The browser can hold a cached
 * session (so the UI looks signed in) after the server-side one has ended, so
 * callers treat this as "signed out" and return to the sign-in screen. Reserve
 * it for that case: every other failure survives a sign-out and would only
 * bounce the user to the login screen on each retry.
 */
export class AuthTokenError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'AuthTokenError';
  }
}

/** header.payload.signature — enough to tell a JWT from an opaque token. */
const JWT_SHAPE = /^[\w-]+\.[\w-]+\.[\w-]+$/;

export async function getAuthJwt(): Promise<string> {
  if (!authClient) {
    throw new Error('Account sign-in unavailable on this deployment');
  }

  // Read the JWT off the session rather than calling authClient.token(). The
  // Neon client routes every URL containing "/token" through its session-cache
  // interceptor, which answers from cache with the { session, user } payload —
  // so token() resolves with no `token` field and never reaches the network.
  // getSession() is where the JWT lands: the client copies it out of the
  // set-auth-jwt response header into session.token.
  const result = await authClient.getSession();

  if (result.error) {
    const { status, message, statusText } = result.error;
    const detail = message ?? statusText;
    throw new Error(
      `Could not reach the sign-in server (${status ?? 'no status'})${detail ? `: ${detail}` : ''}`,
    );
  }

  const token = result.data?.session?.token;
  if (!token) {
    throw new AuthTokenError('Your session has ended. Sign in again.');
  }
  if (!JWT_SHAPE.test(token)) {
    // A session with no JWT means the auth server never sent set-auth-jwt.
    // Signing out would not produce one, so report it instead.
    throw new Error('The sign-in server did not issue a JWT for this session');
  }
  return token;
}

/**
 * Outcome of a successful verification-code submission. Neon Auth is
 * configured to sign the user in as part of verification (the Console's
 * "auto sign-in after verification" default); when it does, the session
 * listener carries the gate open by itself.
 */
export interface EmailVerificationResult {
  autoSignedIn: boolean;
}

/**
 * Turn a better-auth error into copy a person can act on. The codes below are
 * the ones the email-OTP and verification endpoints produce; anything else
 * falls through to the server's own message.
 */
function verificationErrorMessage(
  error: { status?: number; message?: string; statusText?: string; code?: string },
): string {
  switch (error.code) {
    case 'INVALID_OTP':
      return 'That code does not match the one we emailed. Check it and try again.';
    case 'OTP_EXPIRED':
      // Matches the 15-minute lifetime documented for Neon Auth emails.
      return 'That code has expired (codes last 15 minutes). Request a new one.';
    case 'TOO_MANY_ATTEMPTS':
      return 'Too many wrong tries, so the code was invalidated. Request a new one.';
    case 'USER_NOT_FOUND':
      return 'No account exists for that email address.';
    case 'EMAIL_ALREADY_VERIFIED':
      return 'That email address is already verified. Sign in instead.';
    default: {
      const detail = error.message ?? error.statusText;
      const status = error.status;
      return `Email verification failed (${status ?? 'no status'})${detail ? `: ${detail}` : ''}`;
    }
  }
}

/**
 * Submit the 6-digit code Neon Auth emailed at sign-up (the Console's OTP
 * verification method). On success the auth client's session listener
 * refetches the session, so an already-signed-in user becomes verified in
 * place; when the server also auto-signs-in, a signed-out caller becomes
 * signed in the same way.
 */
export async function verifyEmailCode(
  email: string,
  code: string,
): Promise<EmailVerificationResult> {
  if (!authClient) {
    throw new Error('Account sign-in unavailable on this deployment');
  }
  const result = await authClient.emailOtp.verifyEmail({ email, otp: code });
  if (result.error) {
    throw new Error(verificationErrorMessage(result.error));
  }
  const data = result.data as
    | { session?: unknown; token?: unknown }
    | null
    | undefined;
  return { autoSignedIn: Boolean(data?.session ?? data?.token) };
}

/**
 * Ask Neon Auth to send the verification email again. Codes expire after 15
 * minutes, so this is also how a fresh code is requested. The server sends a
 * code or a link depending on the Console's verification method (OTP here).
 */
export async function resendVerificationEmail(email: string): Promise<void> {
  if (!authClient) {
    throw new Error('Account sign-in unavailable on this deployment');
  }
  const result = await authClient.sendVerificationEmail({
    email,
    callbackURL: `${window.location.origin}/account`,
  });
  if (result.error) {
    throw new Error(verificationErrorMessage(result.error));
  }
}
