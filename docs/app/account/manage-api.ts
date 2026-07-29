export interface ManageKey {
  id: string;
  name: string | null;
  prefix: string;
  created_at: string;
  revoked_at: string | null;
}

export type CreatedManageKey = ManageKey & { key: string };

export interface ManageIdentitySnapshot {
  email: string | null;
  email_domain: string | null;
  email_verified: boolean;
  last_verified_at: string | null;
  expires_at: string | null;
  max_age_seconds: number | null;
}

export interface ManageWallet {
  id: string;
  wallet: string;
  linked_at: string | null;
  is_primary: boolean;
}

export interface WalletChallenge {
  id: string;
  message: string;
  expires_at: string;
}

export interface WalletBindingsSnapshot {
  wallets: ManageWallet[];
  identity: ManageIdentitySnapshot | null;
}

export type ManageInstanceAccessMode = 'public' | 'restricted';
export type ManageAclRuleKind = 'email_domain' | 'wallet';

export interface ManageAclRule {
  kind: ManageAclRuleKind;
  value: string;
}

export interface ManageInstance {
  id: string;
  label: string | null;
  peer_id: string;
  owner_wallet: string;
  mode: ManageInstanceAccessMode;
  acl_rules: ManageAclRule[];
  online: boolean | null;
  ownership_status: string | null;
  ownership_checked_at: string | null;
  policy_revision: number | null;
}

export interface ManageInstancesSnapshot {
  instances: ManageInstance[];
  identity: ManageIdentitySnapshot | null;
}

export class ManageApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = 'ManageApiError';
    this.status = status;
  }
}

type RecordValue = Record<string, unknown>;
type NormalizedSnapshot<T> = { items: T[]; identity: ManageIdentitySnapshot | null };

function authHeaders(jwt: string): HeadersInit {
  return { Authorization: `Bearer ${jwt}`, 'Content-Type': 'application/json' };
}

function isRecord(value: unknown): value is RecordValue {
  return typeof value === 'object' && value !== null;
}

function readString(value: unknown): string | null {
  return typeof value === 'string' ? value : null;
}

function readId(value: unknown): string | null {
  if (typeof value === 'string' && value.length > 0) return value;
  if (
    typeof value === 'number' &&
    Number.isInteger(value) &&
    Number.isFinite(value) &&
    value >= 0
  ) {
    return String(value);
  }
  return null;
}

function readBoolean(value: unknown, fallback = false): boolean {
  return typeof value === 'boolean' ? value : fallback;
}

function readArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function readNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}

function readFirstString(value: RecordValue, keys: readonly string[]): string | null {
  for (const key of keys) {
    const result = readString(value[key]);
    if (result !== null) return result;
  }
  return null;
}

async function readErrorDetail(response: Response): Promise<string> {
  const contentType = response.headers.get('content-type') ?? '';
  try {
    const text = (await response.text()).trim();
    if (!text) return response.statusText || `HTTP ${response.status}`;
    if (/json/i.test(contentType)) {
      const body = JSON.parse(text) as
        | { error?: string; detail?: string; message?: string }
        | null;
      if (body && typeof body === 'object') {
        return body.detail ?? body.error ?? body.message ?? text;
      }
    }
    return text;
  } catch {
    return response.statusText || `HTTP ${response.status}`;
  }
}

function requestFailedMessage(error: unknown): string {
  if (typeof navigator !== 'undefined' && navigator.onLine === false) {
    return 'You appear offline — reconnect and try again';
  }
  return error instanceof Error ? error.message : 'The request could not be completed';
}

async function request(
  url: string,
  init: RequestInit,
  onError: (status: number, detail: string) => string,
): Promise<Response> {
  let response: Response;
  try {
    response = await fetch(url, init);
  } catch (error) {
    throw new ManageApiError(requestFailedMessage(error), 0);
  }

  if (!response.ok) {
    const detail = await readErrorDetail(response);
    throw new ManageApiError(onError(response.status, detail), response.status);
  }

  return response;
}

async function requestJson<T>(
  url: string,
  init: RequestInit,
  onError: (status: number, detail: string) => string,
): Promise<T> {
  const response = await request(url, init, onError);
  return (await response.json()) as T;
}

function normalizeIdentity(value: unknown): ManageIdentitySnapshot | null {
  if (!isRecord(value)) return null;
  return {
    email: readString(value.email),
    email_domain: readString(value.email_domain),
    email_verified: readBoolean(value.email_verified),
    last_verified_at: readString(value.last_verified_at),
    expires_at: readString(value.expires_at),
    max_age_seconds: readNumber(value.max_age_seconds),
  };
}

function normalizeWallet(value: unknown): ManageWallet {
  if (!isRecord(value)) {
    throw new Error('Wallet payload must be an object');
  }

  const id = readId(value.id);
  const wallet = readString(value.wallet);
  if (!id || !wallet) {
    throw new Error('Wallet payload is missing id or wallet');
  }

  return {
    id,
    wallet,
    linked_at:
      readString(value.linked_at) ??
      readString(value.created_at) ??
      readString(value.verified_at),
    is_primary: readBoolean(value.is_primary) || readBoolean(value.primary),
  };
}

function normalizeRule(value: unknown): ManageAclRule {
  if (!isRecord(value)) {
    throw new Error('ACL rule payload must be an object');
  }

  const kind = readString(value.kind);
  const rawValue = readString(value.value);
  if (
    (kind !== 'email_domain' && kind !== 'wallet') ||
    kind !== kind.toLowerCase() ||
    !rawValue
  ) {
    throw new Error('ACL rule payload is missing kind or value');
  }

  return { kind, value: rawValue };
}

function normalizeInstanceMode(value: unknown): ManageInstanceAccessMode {
  return readString(value) === 'public' ? 'public' : 'restricted';
}

function readInstanceRules(value: RecordValue): unknown[] {
  const aclRules = value.acl_rules;
  if (Array.isArray(aclRules)) return aclRules;

  const rules = value.rules;
  if (Array.isArray(rules)) return rules;

  return [];
}

function readInstanceOnline(value: RecordValue): boolean | null {
  if (typeof value.online === 'boolean') return value.online;
  if (typeof value.observed_online === 'boolean') return value.observed_online;

  const status = readString(value.status);
  if (status === null) return null;
  return status === 'online';
}

function readOwnershipCheckedAt(value: RecordValue): string | null {
  return readFirstString(value, [
    'ownership_observed_at',
    'ownership_checked_at',
    'last_verified_at',
    'last_observed_at',
  ]);
}

function normalizeInstance(value: unknown): ManageInstance {
  if (!isRecord(value)) {
    throw new Error('Instance payload must be an object');
  }

  const id = readId(value.id);
  const peerId = readFirstString(value, ['peer_id', 'peerId']);
  const ownerWallet = readFirstString(value, ['owner_wallet', 'ownerWallet']);
  if (!id || !peerId || !ownerWallet) {
    throw new Error('Instance payload is missing id, peer_id, or owner_wallet');
  }

  return {
    id,
    label: readString(value.label),
    peer_id: peerId,
    owner_wallet: ownerWallet,
    mode: normalizeInstanceMode(value.mode ?? value.access_mode),
    acl_rules: readInstanceRules(value).map(normalizeRule),
    online: readInstanceOnline(value),
    ownership_status: readFirstString(value, ['ownership_status', 'verification_status']),
    ownership_checked_at: readOwnershipCheckedAt(value),
    policy_revision: readNumber(value.policy_revision),
  };
}

function normalizeSnapshot<T>(
  body: unknown,
  listKey: string,
  normalizeItem: (value: unknown) => T,
  invalidMessage: string,
): NormalizedSnapshot<T> {
  if (Array.isArray(body)) {
    return { items: body.map(normalizeItem), identity: null };
  }

  if (!isRecord(body)) {
    throw new Error(invalidMessage);
  }

  return {
    items: readArray(body[listKey]).map(normalizeItem),
    identity:
      normalizeIdentity(body.identity) ??
      normalizeIdentity(body.email_identity) ??
      null,
  };
}

function normalizeWalletBindings(body: unknown): WalletBindingsSnapshot {
  const snapshot = normalizeSnapshot(
    body,
    'wallets',
    normalizeWallet,
    'Wallet list payload must be an array or object',
  );
  return { wallets: snapshot.items, identity: snapshot.identity };
}

function normalizeInstances(body: unknown): ManageInstancesSnapshot {
  const snapshot = normalizeSnapshot(
    body,
    'instances',
    normalizeInstance,
    'Instance list payload must be an array or object',
  );
  return { instances: snapshot.items, identity: snapshot.identity };
}

function keyError(status: number): string {
  switch (status) {
    case 400:
      return 'Bad request (name too long?)';
    case 401:
      return 'Not authenticated — sign in again';
    case 409:
      return 'Key limit reached';
    default:
      return `Key request failed: ${status}`;
  }
}

function walletListError(status: number): string {
  switch (status) {
    case 401:
      return 'Not authenticated — sign in again';
    case 503:
      return 'Wallet bindings are temporarily unavailable';
    default:
      return `Wallet list failed: ${status}`;
  }
}

function walletChallengeError(status: number, detail: string): string {
  switch (status) {
    case 400:
      return detail || 'Enter a valid Solana wallet address';
    case 401:
      return 'Not authenticated — sign in again';
    case 409:
      return detail || 'This wallet is already linked elsewhere';
    case 503:
      return 'Wallet challenge service is temporarily unavailable';
    default:
      return detail || `Wallet challenge failed: ${status}`;
  }
}

function walletLinkError(status: number, detail: string): string {
  switch (status) {
    case 400:
      return detail || 'The wallet proof is invalid, expired, or already used';
    case 401:
      return 'Not authenticated — sign in again';
    case 409:
      return detail || 'This wallet is already linked to another account';
    case 503:
      return 'Wallet verification is temporarily unavailable';
    default:
      return detail || `Wallet link failed: ${status}`;
  }
}

function walletDeleteError(status: number, detail: string): string {
  switch (status) {
    case 401:
      return 'Not authenticated — sign in again';
    case 404:
      return 'No such linked wallet for this account';
    case 409:
      return detail || 'This wallet still proves ownership of an active instance';
    default:
      return detail || `Wallet delete failed: ${status}`;
  }
}

function instanceListError(status: number): string {
  switch (status) {
    case 401:
      return 'Not authenticated — sign in again';
    case 503:
      return 'Instance management is temporarily unavailable';
    default:
      return `Instance list failed: ${status}`;
  }
}

function instanceCreateError(status: number, detail: string): string {
  switch (status) {
    case 400:
      return detail || 'Enter a valid peer ID and label';
    case 401:
      return 'Not authenticated — sign in again';
    case 409:
      return detail || 'That instance is already claimed';
    case 422:
      return detail || 'This peer is offline, unverifiable, or not owned by one of your linked wallets';
    case 503:
      return 'Ownership verification is temporarily unavailable';
    default:
      return detail || `Create failed: ${status}`;
  }
}

function instanceUpdateError(status: number, detail: string): string {
  switch (status) {
    case 400:
      return detail || 'The instance update is invalid';
    case 401:
      return 'Not authenticated — sign in again';
    case 404:
      return 'No such instance for this account';
    case 409:
      return detail || 'This instance cannot be changed until ownership is re-verified';
    case 422:
      return detail || 'Ownership verification failed for this instance';
    case 503:
      return 'Instance verification is temporarily unavailable';
    default:
      return detail || `Update failed: ${status}`;
  }
}

function instanceDeleteError(status: number, detail: string): string {
  switch (status) {
    case 401:
      return 'Not authenticated — sign in again';
    case 404:
      return 'No such instance for this account';
    case 503:
      return 'Instance deletion is temporarily unavailable';
    default:
      return detail || `Delete failed: ${status}`;
  }
}

export async function createManageKey(
  baseUrl: string,
  jwt: string,
  name?: string,
): Promise<CreatedManageKey> {
  return requestJson<CreatedManageKey>(
    `${baseUrl}/manage/keys`,
    {
      method: 'POST',
      headers: authHeaders(jwt),
      body: JSON.stringify(name ? { name } : {}),
    },
    (status) => keyError(status),
  );
}

export async function listManageKeys(
  baseUrl: string,
  jwt: string,
): Promise<ManageKey[]> {
  return requestJson<ManageKey[]>(
    `${baseUrl}/manage/keys`,
    { headers: authHeaders(jwt) },
    (status) => {
      switch (status) {
        case 401:
          return 'Not authenticated — sign in again';
        default:
          return `List failed: ${status}`;
      }
    },
  );
}

export async function revokeManageKey(
  baseUrl: string,
  jwt: string,
  id: string,
): Promise<void> {
  await request(
    `${baseUrl}/manage/keys/${encodeURIComponent(id)}`,
    {
      method: 'DELETE',
      headers: authHeaders(jwt),
    },
    (status) => {
      switch (status) {
        case 404:
          return 'No such key for this account';
        default:
          return `Revoke failed: ${status}`;
      }
    },
  );
}

export async function listManageWallets(
  baseUrl: string,
  jwt: string,
): Promise<WalletBindingsSnapshot> {
  const body = await requestJson<unknown>(
    `${baseUrl}/manage/wallets`,
    { headers: authHeaders(jwt) },
    (status) => walletListError(status),
  );
  return normalizeWalletBindings(body);
}

export async function createWalletChallenge(
  baseUrl: string,
  jwt: string,
  wallet: string,
): Promise<WalletChallenge> {
  return requestJson<WalletChallenge>(
    `${baseUrl}/manage/wallets/challenges`,
    {
      method: 'POST',
      headers: authHeaders(jwt),
      body: JSON.stringify({ wallet }),
    },
    walletChallengeError,
  );
}

export async function linkManageWallet(
  baseUrl: string,
  jwt: string,
  input: { challenge_id: string; signature: string },
): Promise<void> {
  await request(
    `${baseUrl}/manage/wallets`,
    {
      method: 'POST',
      headers: authHeaders(jwt),
      body: JSON.stringify(input),
    },
    walletLinkError,
  );
}

export async function deleteManageWallet(
  baseUrl: string,
  jwt: string,
  id: string,
): Promise<void> {
  await request(
    `${baseUrl}/manage/wallets/${encodeURIComponent(id)}`,
    {
      method: 'DELETE',
      headers: authHeaders(jwt),
    },
    walletDeleteError,
  );
}

export async function listManageInstances(
  baseUrl: string,
  jwt: string,
): Promise<ManageInstancesSnapshot> {
  const body = await requestJson<unknown>(
    `${baseUrl}/manage/instances`,
    { headers: authHeaders(jwt) },
    (status) => instanceListError(status),
  );
  return normalizeInstances(body);
}

export async function createManageInstance(
  baseUrl: string,
  jwt: string,
  input: { peer_id: string; label?: string },
): Promise<ManageInstance> {
  const body = await requestJson<unknown>(
    `${baseUrl}/manage/instances`,
    {
      method: 'POST',
      headers: authHeaders(jwt),
      body: JSON.stringify(input.label ? input : { peer_id: input.peer_id }),
    },
    instanceCreateError,
  );
  return normalizeInstance(body);
}

export async function updateManageInstance(
  baseUrl: string,
  jwt: string,
  id: string,
  input: { label?: string; mode?: ManageInstanceAccessMode },
): Promise<ManageInstance> {
  const payload: { label?: string; mode?: ManageInstanceAccessMode } = {};
  if (input.label !== undefined) payload.label = input.label;
  if (input.mode !== undefined) payload.mode = input.mode;
  const body = await requestJson<unknown>(
    `${baseUrl}/manage/instances/${encodeURIComponent(id)}`,
    {
      method: 'PATCH',
      headers: authHeaders(jwt),
      body: JSON.stringify(payload),
    },
    instanceUpdateError,
  );
  return normalizeInstance(body);
}

export async function replaceManageInstanceAcl(
  baseUrl: string,
  jwt: string,
  id: string,
  input: { mode: ManageInstanceAccessMode; rules: ManageAclRule[] },
): Promise<ManageInstance> {
  const body = await requestJson<unknown>(
    `${baseUrl}/manage/instances/${encodeURIComponent(id)}/acl`,
    {
      method: 'PUT',
      headers: authHeaders(jwt),
      body: JSON.stringify(input),
    },
    instanceUpdateError,
  );
  return normalizeInstance(body);
}

export async function deleteManageInstance(
  baseUrl: string,
  jwt: string,
  id: string,
): Promise<void> {
  await request(
    `${baseUrl}/manage/instances/${encodeURIComponent(id)}`,
    {
      method: 'DELETE',
      headers: authHeaders(jwt),
    },
    instanceDeleteError,
  );
}
