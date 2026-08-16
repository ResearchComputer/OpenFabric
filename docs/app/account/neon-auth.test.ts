import { beforeEach, describe, expect, it, vi } from 'vitest';

const JWT = 'eyJhbGciOiJFZERTQSJ9.eyJzdWIiOiJ1c2VyLTEifQ.c2lnbmF0dXJl';
// What better-auth stores as session.token before the client overwrites it with
// the JWT from the set-auth-jwt response header.
const OPAQUE_SESSION_TOKEN = 'remZjVn0TKB6GC0hIfAse1uJRASJGYOM';

// Stands in for the Neon auth client. token() reproduces the vendor behaviour
// that broke sign-in: the client routes any URL containing "/token" through its
// session-cache interceptor, which answers from cache with the { session, user }
// payload — so token() resolves with no `token` field and never reaches the
// network. Any implementation that consults it therefore sees "no token".
const client = vi.hoisted(() => ({
  getSession: vi.fn(),
  token: vi.fn(),
  emailOtp: { verifyEmail: vi.fn() },
  sendVerificationEmail: vi.fn(),
}));

vi.mock('@neondatabase/neon-js/auth', () => ({
  createAuthClient: () => client,
}));
vi.mock('@neondatabase/neon-js/auth/react', () => ({
  BetterAuthReactAdapter: () => () => ({}),
}));
vi.mock('./config', () => ({
  tokenManagerConfig: { neonAuthUrl: 'https://auth.example/neondb/auth' },
}));

const { AuthTokenError, getAuthJwt, verifyEmailCode, resendVerificationEmail } =
  await import('./neon-auth');

function signedInSession(token: string) {
  return { data: { session: { token }, user: { id: 'user-1' } }, error: null };
}

beforeEach(() => {
  vi.clearAllMocks();
  client.token.mockResolvedValue({
    data: { session: { token: JWT }, user: { id: 'user-1' } },
    error: null,
  });
});

describe('getAuthJwt', () => {
  it('returns the JWT the session carries', async () => {
    client.getSession.mockResolvedValue(signedInSession(JWT));
    await expect(getAuthJwt()).resolves.toBe(JWT);
  });

  it('never asks the token endpoint, whose response the session cache intercepts', async () => {
    client.getSession.mockResolvedValue(signedInSession(JWT));
    await getAuthJwt();
    expect(client.token).not.toHaveBeenCalled();
  });

  it('reports an ended session as AuthTokenError so the gate returns to sign-in', async () => {
    client.getSession.mockResolvedValue({ data: null, error: null });
    await expect(getAuthJwt()).rejects.toBeInstanceOf(AuthTokenError);
  });

  // Signing the user out cannot fix either of these, and doing so would bounce
  // them to the login screen on every attempt.
  it('does not treat an unreachable auth host as a signed-out session', async () => {
    client.getSession.mockResolvedValue({
      data: null,
      error: { status: 503, message: 'Service Unavailable' },
    });
    const error = await getAuthJwt().catch((e: unknown) => e);
    expect(error).toBeInstanceOf(Error);
    expect(error).not.toBeInstanceOf(AuthTokenError);
    expect(String(error)).toMatch(/503|unavailable/i);
  });

  it('does not treat an opaque session token as a signed-out session', async () => {
    client.getSession.mockResolvedValue(signedInSession(OPAQUE_SESSION_TOKEN));
    const error = await getAuthJwt().catch((e: unknown) => e);
    expect(error).toBeInstanceOf(Error);
    expect(error).not.toBeInstanceOf(AuthTokenError);
    expect(String(error)).toMatch(/JWT/i);
  });
});

describe('verifyEmailCode', () => {
  it('submits the emailed code and reports the auto sign-in the server did', async () => {
    client.emailOtp.verifyEmail.mockResolvedValue({
      data: { session: { token: JWT }, user: { id: 'user-1' } },
      error: null,
    });
    await expect(verifyEmailCode('a@b.c', '123456')).resolves.toEqual({
      autoSignedIn: true,
    });
    expect(client.emailOtp.verifyEmail).toHaveBeenCalledWith({
      email: 'a@b.c',
      otp: '123456',
    });
  });

  it('reports no auto sign-in when verification returns no session or token', async () => {
    client.emailOtp.verifyEmail.mockResolvedValue({
      data: { status: true },
      error: null,
    });
    await expect(verifyEmailCode('a@b.c', '123456')).resolves.toEqual({
      autoSignedIn: false,
    });
  });

  // The wrong code is the common case; the raw "INVALID_OTP" copy from the
  // server must not leak into the UI.
  it('translates a wrong code into guidance', async () => {
    client.emailOtp.verifyEmail.mockResolvedValue({
      data: null,
      error: { status: 400, code: 'INVALID_OTP', message: 'Invalid OTP' },
    });
    await expect(verifyEmailCode('a@b.c', '000000')).rejects.toThrow(/does not match/);
  });

  it('translates an expired code into the documented 15-minute lifetime', async () => {
    client.emailOtp.verifyEmail.mockResolvedValue({
      data: null,
      error: { status: 400, code: 'OTP_EXPIRED', message: 'OTP expired' },
    });
    await expect(verifyEmailCode('a@b.c', '000000')).rejects.toThrow(/15 minutes/);
  });

  it('falls through to the server message for unmapped failures', async () => {
    client.emailOtp.verifyEmail.mockResolvedValue({
      data: null,
      error: { status: 503, message: 'Service Unavailable' },
    });
    await expect(verifyEmailCode('a@b.c', '123456')).rejects.toThrow(
      /503.*Service Unavailable/,
    );
  });
});

describe('resendVerificationEmail', () => {
  it('points the verification email back at the account console', async () => {
    client.sendVerificationEmail.mockResolvedValue({ data: { status: true }, error: null });
    await expect(resendVerificationEmail('a@b.c')).resolves.toBeUndefined();
    expect(client.sendVerificationEmail).toHaveBeenCalledWith({
      email: 'a@b.c',
      callbackURL: `${window.location.origin}/account`,
    });
  });

  it('translates an unknown address instead of pretending the email went out', async () => {
    client.sendVerificationEmail.mockResolvedValue({
      data: null,
      error: { status: 404, code: 'USER_NOT_FOUND', message: 'User not found' },
    });
    await expect(resendVerificationEmail('a@b.c')).rejects.toThrow(/No account exists/);
  });
});
