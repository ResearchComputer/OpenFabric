export interface ApiKeyInfo {
  key_id: string;
  wallet: string;
  label: string;
  created_at: string;
  revoked: boolean;
}

export interface CreatedApiKey extends ApiKeyInfo {
  token: string;
}

async function parseError(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as { detail?: string; error?: string };
    return body.detail ?? body.error ?? response.statusText;
  } catch {
    return response.statusText;
  }
}

async function requestJson<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });

  if (!response.ok) {
    throw new Error(await parseError(response));
  }

  return (await response.json()) as T;
}

export function buildAuthChallenge(wallet: string, now = Date.now()): string {
  return `otela-auth:${wallet}:${Math.floor(now / 1000)}`;
}

export async function createApiKey(
  baseUrl: string,
  input: {
    wallet: string;
    signature: string;
    challenge: string;
    label: string;
  },
): Promise<CreatedApiKey> {
  return requestJson<CreatedApiKey>(`${baseUrl}/api/keys`, {
    method: 'POST',
    body: JSON.stringify(input),
  });
}

export async function listApiKeys(
  baseUrl: string,
  wallet: string,
): Promise<ApiKeyInfo[]> {
  const params = new URLSearchParams({ wallet });
  return requestJson<ApiKeyInfo[]>(`${baseUrl}/api/keys?${params.toString()}`);
}

export async function revokeApiKey(
  baseUrl: string,
  keyId: string,
  wallet: string,
): Promise<{ status: string; key_id: string }> {
  const params = new URLSearchParams({ wallet });
  return requestJson<{ status: string; key_id: string }>(
    `${baseUrl}/api/keys/${encodeURIComponent(keyId)}?${params.toString()}`,
    { method: 'DELETE' },
  );
}
