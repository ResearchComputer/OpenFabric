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

export async function getAuthJwt(): Promise<string> {
  if (!authClient) {
    throw new Error('Account sign-in unavailable on this deployment');
  }
  const { data, error } = await authClient.token();
  if (error || !data?.token) throw new Error('Not signed in');
  return data.token;
}
