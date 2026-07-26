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
