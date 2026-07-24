import { afterEach, describe, expect, it } from 'vitest';
import { clearLink, readLink, writeLink } from './link';

afterEach(() => window.localStorage.clear());

describe('account link', () => {
  it('returns null when nothing is stored', () => {
    expect(readLink()).toBeNull();
  });

  it('writes and reads back a link with a timestamp', () => {
    const saved = writeLink({ wallet: 'W1', neonUserId: 'U1' });
    expect(saved.wallet).toBe('W1');
    expect(saved.neonUserId).toBe('U1');
    expect(typeof saved.linkedAt).toBe('string');
    expect(readLink()).toEqual(saved);
  });

  it('clears the stored link', () => {
    writeLink({ wallet: 'W1', neonUserId: 'U1' });
    clearLink();
    expect(readLink()).toBeNull();
  });

  it('returns null for corrupt json', () => {
    window.localStorage.setItem('opentela-account-link', '{not json');
    expect(readLink()).toBeNull();
  });

  it('returns null when a stored record is missing fields', () => {
    window.localStorage.setItem('opentela-account-link', JSON.stringify({ wallet: 'W1' }));
    expect(readLink()).toBeNull();
  });
});
