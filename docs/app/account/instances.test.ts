import { describe, expect, it } from 'vitest';
import {
  computeIdentityFreshness,
  normalizeAclRules,
  normalizeEmailDomainRule,
  normalizeServiceName,
  normalizeWalletRule,
} from './instances';

const WALLET = '9xQeWvG816bUx9EPfEZ2vA8SxVYbdxM2f6u9QhQv5Kxa';

describe('normalizeEmailDomainRule', () => {
  it('lowercases ASCII domains', () => {
    expect(normalizeEmailDomainRule('Example.ORG')).toBe('example.org');
  });

  it('rejects full email addresses', () => {
    expect(() => normalizeEmailDomainRule('alice@example.org')).toThrow(/domain/i);
  });

  it('rejects unicode domains', () => {
    expect(() => normalizeEmailDomainRule('exämple.org')).toThrow(/ASCII/i);
  });
});

describe('normalizeWalletRule', () => {
  it('accepts canonical Solana addresses', () => {
    expect(normalizeWalletRule(WALLET)).toBe(WALLET);
  });

  it('rejects malformed addresses', () => {
    expect(() => normalizeWalletRule('not-a-wallet')).toThrow(/valid Solana wallet/i);
  });
});

describe('normalizeServiceName', () => {
  it('accepts exact ASCII service names', () => {
    expect(normalizeServiceName('llm-private')).toBe('llm-private');
  });

  it('rejects service names with spaces', () => {
    expect(() => normalizeServiceName('llm private')).toThrow(/ASCII letters/i);
  });
});

describe('normalizeAclRules', () => {
  it('deduplicates normalized rules while keeping order', () => {
    expect(
      normalizeAclRules([
        { kind: 'email_domain', value: 'Example.org' },
        { kind: 'email_domain', value: 'example.org' },
        { kind: 'wallet', value: WALLET },
      ]),
    ).toEqual([
      { kind: 'email_domain', value: 'example.org' },
      { kind: 'wallet', value: WALLET },
    ]);
  });
});

describe('computeIdentityFreshness', () => {
  it('marks a verified identity stale after expiry', () => {
    expect(
      computeIdentityFreshness(
        {
          email: 'alice@example.org',
          email_domain: 'example.org',
          email_verified: true,
          last_verified_at: '2026-07-01T00:00:00Z',
          expires_at: '2026-07-10T00:00:00Z',
          max_age_seconds: null,
        },
        Date.parse('2026-07-29T00:00:00Z'),
      ),
    ).toEqual({
      state: 'stale',
      expires_at: '2026-07-10T00:00:00Z',
    });
  });

  it('marks unverified identities as unusable for email rules', () => {
    expect(
      computeIdentityFreshness({
        email: 'alice@example.org',
        email_domain: 'example.org',
        email_verified: false,
        last_verified_at: null,
        expires_at: null,
        max_age_seconds: null,
      }),
    ).toEqual({
      state: 'unverified',
      expires_at: null,
    });
  });
});
