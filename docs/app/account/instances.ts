import { PublicKey } from '@solana/web3.js';
import type { ManageAclRule, ManageAclRuleKind, ManageIdentitySnapshot } from './manage-api';

export interface IdentityFreshness {
  state: 'unknown' | 'unverified' | 'fresh' | 'stale';
  expires_at: string | null;
}

export function normalizeEmailDomainRule(value: string): string {
  const normalized = value.trim().toLowerCase();
  if (!normalized) throw new Error('Enter an email domain');
  if (normalized.length > 253) throw new Error('Email domains must be 253 characters or less');
  if (!/^[\x21-\x7e]+$/.test(normalized)) {
    throw new Error('Email domains must use ASCII only');
  }
  if (normalized.includes('@')) {
    throw new Error('Enter only the domain, not a full email address');
  }
  if (
    normalized.startsWith('.') ||
    normalized.endsWith('.') ||
    normalized.includes('..')
  ) {
    throw new Error('Email domains cannot start, end, or repeat dots');
  }

  const labels = normalized.split('.');
  if (labels.length < 2) throw new Error('Enter a full domain like example.org');
  for (const label of labels) {
    if (!/^[a-z0-9-]+$/.test(label)) {
      throw new Error('Email domains can use only letters, numbers, hyphens, and dots');
    }
    if (label.startsWith('-') || label.endsWith('-')) {
      throw new Error('Email-domain labels cannot start or end with a hyphen');
    }
    if (label.length === 0 || label.length > 63) {
      throw new Error('Each domain label must be between 1 and 63 characters');
    }
  }

  return normalized;
}

export function normalizeWalletRule(value: string): string {
  const normalized = value.trim();
  if (!normalized) throw new Error('Enter a Solana wallet address');

  let publicKey: PublicKey;
  try {
    publicKey = new PublicKey(normalized);
  } catch {
    throw new Error('Enter a valid Solana wallet address');
  }

  const canonical = publicKey.toBase58();
  if (canonical !== normalized) {
    throw new Error('Wallet addresses must use canonical base58 form');
  }
  return canonical;
}

export function normalizeAclRules(
  rules: ReadonlyArray<ManageAclRule>,
): ManageAclRule[] {
  const seen = new Set<string>();
  const normalized: ManageAclRule[] = [];

  for (const rule of rules) {
    const value =
      rule.kind === 'email_domain'
        ? normalizeEmailDomainRule(rule.value)
        : normalizeWalletRule(rule.value);
    const key = `${rule.kind}:${value}`;
    if (seen.has(key)) continue;
    seen.add(key);
    normalized.push({ kind: rule.kind, value });
  }

  return normalized;
}

export function normalizeServiceName(value: string): string {
  const normalized = value.trim();
  if (!normalized) throw new Error('Enter an exact service name');
  if (normalized.length > 80) {
    throw new Error('Service names must be 80 characters or less');
  }
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$/.test(normalized)) {
    throw new Error(
      'Service names can use ASCII letters, numbers, dots, underscores, and hyphens',
    );
  }
  return normalized;
}

export function emptyRule(kind: ManageAclRuleKind = 'email_domain'): ManageAclRule {
  return { kind, value: '' };
}

export function describeRulePlaceholder(kind: ManageAclRuleKind): string {
  return kind === 'email_domain' ? 'example.org' : 'FhtA...';
}

function resolveIdentityExpiry(identity: ManageIdentitySnapshot): string | null {
  if (identity.expires_at) {
    return identity.expires_at;
  }
  if (!identity.last_verified_at || !identity.max_age_seconds) {
    return null;
  }

  return new Date(
    new Date(identity.last_verified_at).getTime() + identity.max_age_seconds * 1000,
  ).toISOString();
}

export function computeIdentityFreshness(
  identity: ManageIdentitySnapshot | null,
  now = Date.now(),
): IdentityFreshness {
  if (!identity) return { state: 'unknown', expires_at: null };
  if (!identity.email_verified) {
    return {
      state: 'unverified',
      expires_at: identity.expires_at,
    };
  }

  const expiresAt = resolveIdentityExpiry(identity);
  if (!expiresAt) return { state: 'unknown', expires_at: null };
  const expiresTime = new Date(expiresAt).getTime();
  if (!Number.isFinite(expiresTime)) return { state: 'unknown', expires_at: null };
  return {
    state: expiresTime > now ? 'fresh' : 'stale',
    expires_at: expiresAt,
  };
}
