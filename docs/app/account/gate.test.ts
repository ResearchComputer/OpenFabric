import { describe, expect, it } from 'vitest';
import { resolveGate, type GateInput } from './gate';

const signedIn: GateInput = {
  mounted: true,
  sessionLoading: false,
  sessionError: null,
  hasUser: true,
};

describe('resolveGate', () => {
  it('waits before deciding anything', () => {
    expect(resolveGate({ ...signedIn, mounted: false })).toBe('loading');
    expect(resolveGate({ ...signedIn, sessionLoading: true })).toBe('loading');
  });

  it('shows the console to a signed-in user', () => {
    expect(resolveGate(signedIn)).toBe('signed-in');
  });

  it('shows sign-in when the check succeeded and found no user', () => {
    expect(resolveGate({ ...signedIn, hasUser: false })).toBe('signed-out');
  });

  // The reported bug: a failed refetch reports no data, just like a signed-out
  // session, and the console was replaced by the sign-in screen mid-session.
  it('keeps a signed-in user through a failed session check', () => {
    expect(resolveGate({ ...signedIn, sessionError: 'Failed to fetch' })).toBe(
      'signed-in',
    );
  });

  it('does not ask for a password it never established was needed', () => {
    expect(
      resolveGate({ ...signedIn, hasUser: false, sessionError: 'Failed to fetch' }),
    ).toBe('unreachable');
  });

  // Neon Auth requires email verification at sign-up: the session exists right
  // away, but the console stays closed until the emailed code is entered.
  it('sends a signed-in but unverified user to email verification', () => {
    expect(resolveGate({ ...signedIn, emailVerified: false })).toBe('verify-email');
  });

  it('shows the console once the address is verified', () => {
    expect(resolveGate({ ...signedIn, emailVerified: true })).toBe('signed-in');
  });

  it('verification state survives a failed refetch like the session does', () => {
    expect(
      resolveGate({ ...signedIn, emailVerified: false, sessionError: 'Failed to fetch' }),
    ).toBe('verify-email');
    expect(
      resolveGate({ ...signedIn, emailVerified: true, sessionError: 'Failed to fetch' }),
    ).toBe('signed-in');
  });

  it('never gates someone whose verification state is unknown', () => {
    expect(resolveGate({ ...signedIn, emailVerified: null })).toBe('signed-in');
    expect(resolveGate({ ...signedIn, emailVerified: undefined })).toBe('signed-in');
  });

  it('asks for verification data only when there is a user', () => {
    expect(resolveGate({ ...signedIn, hasUser: false, emailVerified: false })).toBe(
      'signed-out',
    );
  });
});
