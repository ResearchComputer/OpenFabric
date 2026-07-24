import { afterEach, describe, expect, it, vi } from 'vitest';
import { createManageKey, listManageKeys, revokeManageKey } from './manage-api';

const BASE = 'https://api.example';

afterEach(() => vi.restoreAllMocks());

function mockFetch(status: number, body?: unknown) {
  return vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), { status }),
  );
}

describe('manage-api', () => {
  it('creates a key and returns the plaintext once', async () => {
    mockFetch(201, {
      id: '1', key: 'sk-abc', prefix: 'sk-1a2b', name: 'laptop', created_at: 't',
    });
    const created = await createManageKey(BASE, 'jwt', 'laptop');
    expect(created.key).toBe('sk-abc');
    expect(created.prefix).toBe('sk-1a2b');
  });

  it('maps 409 to a key-cap error', async () => {
    mockFetch(409);
    await expect(createManageKey(BASE, 'jwt')).rejects.toThrow(/limit/i);
  });

  it('maps 401 to a not-authenticated error', async () => {
    mockFetch(401);
    await expect(listManageKeys(BASE, 'jwt')).rejects.toThrow(/authenticat/i);
  });

  it('lists keys', async () => {
    mockFetch(200, [{ id: '1', name: null, prefix: 'sk-1a2b', created_at: 't', revoked_at: null }]);
    const keys = await listManageKeys(BASE, 'jwt');
    expect(keys).toHaveLength(1);
  });

  it('revokes on 204', async () => {
    mockFetch(204);
    await expect(revokeManageKey(BASE, 'jwt', '1')).resolves.toBeUndefined();
  });

  it('maps revoke 404 to not-found', async () => {
    mockFetch(404);
    await expect(revokeManageKey(BASE, 'jwt', 'x')).rejects.toThrow(/no such key/i);
  });
});
