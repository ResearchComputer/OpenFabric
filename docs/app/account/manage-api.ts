export interface ManageKey {
  id: string;
  name: string | null;
  prefix: string;
  created_at: string;
  revoked_at: string | null;
}

export type CreatedManageKey = ManageKey & { key: string };

function authHeaders(jwt: string): HeadersInit {
  return { Authorization: `Bearer ${jwt}`, 'Content-Type': 'application/json' };
}

export async function createManageKey(
  baseUrl: string,
  jwt: string,
  name?: string,
): Promise<CreatedManageKey> {
  const res = await fetch(`${baseUrl}/manage/keys`, {
    method: 'POST',
    headers: authHeaders(jwt),
    body: JSON.stringify(name ? { name } : {}),
  });
  if (res.status === 401) throw new Error('Not authenticated — sign in again');
  if (res.status === 409) throw new Error('Key limit reached');
  if (res.status === 400) throw new Error('Bad request (name too long?)');
  if (!res.ok) throw new Error(`Create failed: ${res.status}`);
  return (await res.json()) as CreatedManageKey;
}

export async function listManageKeys(
  baseUrl: string,
  jwt: string,
): Promise<ManageKey[]> {
  const res = await fetch(`${baseUrl}/manage/keys`, { headers: authHeaders(jwt) });
  if (res.status === 401) throw new Error('Not authenticated — sign in again');
  if (!res.ok) throw new Error(`List failed: ${res.status}`);
  return (await res.json()) as ManageKey[];
}

export async function revokeManageKey(
  baseUrl: string,
  jwt: string,
  id: string,
): Promise<void> {
  const res = await fetch(`${baseUrl}/manage/keys/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: authHeaders(jwt),
  });
  if (res.status === 404) throw new Error('No such key for this account');
  if (!res.ok) throw new Error(`Revoke failed: ${res.status}`);
}
