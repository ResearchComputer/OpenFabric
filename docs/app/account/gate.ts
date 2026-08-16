/**
 * Which of the four screens /account should show.
 *
 * The distinction that matters here is between "not signed in" and "could not
 * find out". The session client reports both as no data, so treating them alike
 * replaces the console with the sign-in screen whenever a background session
 * refetch fails — while the session is still perfectly valid.
 */
export type GateState =
  | 'loading'
  | 'unreachable'
  | 'signed-out'
  | 'verify-email'
  | 'signed-in';

export interface GateInput {
  /** False until the client has mounted, so SSR and hydration agree. */
  mounted: boolean;
  /** The first session check is still in flight. */
  sessionLoading: boolean;
  /** The last session check failed, e.g. the auth host was unreachable. */
  sessionError: string | null;
  /** The user the last successful session check reported, if any. */
  hasUser: boolean;
  /**
   * What the same session check knows about the address: false means it
   * still needs the verification code Neon Auth emails at sign-up (the
   * Console's "Verify at Sign-up" requirement). Unknown (null/undefined)
   * must not block the console — only a definitive report gates a user.
   */
  emailVerified?: boolean | null;
}

export function resolveGate(input: GateInput): GateState {
  if (!input.mounted || input.sessionLoading) return 'loading';
  // A user we already have outranks a failed refetch: a lost connection is not
  // a sign-out, and ejecting mid-session would lose whatever they were doing.
  // Their verification state comes from the same last-known data, so the same
  // precedence applies — a failed refetch neither verifies nor unverifies them.
  if (input.hasUser && input.emailVerified === false) return 'verify-email';
  if (input.hasUser) return 'signed-in';
  if (input.sessionError) return 'unreachable';
  return 'signed-out';
}
