import { describe, expect, it } from 'vitest';
import { tokenManagerConfig } from './config';
import { describeSolanaError } from './solana';

// Solana's own public endpoint answers the CORS preflight and then rejects the
// request itself with 403 the moment it carries a browser Origin, so it cannot
// serve this page however correct the URL looks.
const BLOCKS_BROWSER_TRAFFIC = 'api.mainnet-beta.solana.com';

describe('the default RPC endpoint', () => {
  it('is one that accepts browser traffic', () => {
    expect(tokenManagerConfig.solanaRpcUrl).not.toContain(BLOCKS_BROWSER_TRAFFIC);
  });
});

describe('describeSolanaError', () => {
  it('names the endpoint that refused, and what to do about it', () => {
    const message = describeSolanaError(
      new Error('failed to get balance: 403 Access forbidden'),
      'https://rpc.example',
    );
    expect(message).toContain('https://rpc.example');
    expect(message).toMatch(/RPC URL/i);
  });

  it('passes other failures through unchanged', () => {
    expect(describeSolanaError(new Error('Invalid param'), 'https://rpc.example')).toBe(
      'Invalid param',
    );
  });
});
