import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  createManageInstance,
  createManageKey,
  createWalletChallenge,
  deleteManageInstance,
  deleteManageWallet,
  linkManageWallet,
  listManageInstances,
  listManageKeys,
  listManageWallets,
  ManageApiError,
  replaceManageInstanceAcl,
  revokeManageKey,
  updateManageInstance,
} from './manage-api';

const BASE = 'https://api.example';

afterEach(() => vi.restoreAllMocks());

function mockFetch(status: number, body?: unknown) {
  return vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), { status }),
  );
}

describe('manage-api keys', () => {
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
    mockFetch(200, [
      { id: '1', name: null, prefix: 'sk-1a2b', created_at: 't', revoked_at: null },
    ]);
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

describe('manage-api wallets', () => {
  it('creates the wallet challenge with the expected path and body', async () => {
    const fetchSpy = mockFetch(200, {
      id: 'c1',
      message: 'sign this exact string',
      expires_at: '2026-07-29T12:00:00Z',
    });

    await expect(createWalletChallenge(BASE, 'jwt', 'abc')).resolves.toEqual({
      id: 'c1',
      message: 'sign this exact string',
      expires_at: '2026-07-29T12:00:00Z',
    });

    expect(fetchSpy).toHaveBeenCalledWith(
      'https://api.example/manage/wallets/challenges',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ wallet: 'abc' }),
      }),
    );
  });

  it('links a wallet by posting challenge id plus base58 signature', async () => {
    const fetchSpy = mockFetch(201);
    await expect(
      linkManageWallet(BASE, 'jwt', { challenge_id: 'c1', signature: '5Z...' }),
    ).resolves.toBeUndefined();

    expect(fetchSpy).toHaveBeenCalledWith(
      'https://api.example/manage/wallets',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ challenge_id: 'c1', signature: '5Z...' }),
      }),
    );
  });

  it('normalizes wallet lists plus optional identity metadata', async () => {
    mockFetch(200, {
      wallets: [
        {
          id: 101,
          wallet: '9xQeWvG816bUx9EPfEZ2vA8SxVYbdxM2f6u9QhQv5Kxa',
          linked_at: '2026-07-29T10:00:00Z',
          primary: true,
        },
      ],
      identity: {
        email: 'alice@example.org',
        email_domain: 'example.org',
        email_verified: true,
        last_verified_at: '2026-07-29T10:00:00Z',
        expires_at: '2026-08-28T10:00:00Z',
        max_age_seconds: 2_592_000,
      },
    });

    const result = await listManageWallets(BASE, 'jwt');
    expect(result.wallets[0].id).toBe('101');
    expect(result.wallets[0].wallet).toContain('9xQe');
    expect(result.wallets[0].is_primary).toBe(true);
    expect(result.identity?.email_domain).toBe('example.org');
  });

  it('maps wallet delete conflicts to ownership-proof messaging', async () => {
    mockFetch(409, { detail: 'active owner proof' });
    await expect(deleteManageWallet(BASE, 'jwt', 'w1')).rejects.toThrow(/owner proof/i);
  });

  it('preserves plain-text error bodies from http.Error responses', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('challenge expired', {
        status: 400,
        headers: { 'content-type': 'text/plain; charset=utf-8' },
      }),
    );
    await expect(createWalletChallenge(BASE, 'jwt', 'abc')).rejects.toThrow(
      /challenge expired/i,
    );
  });

  it('surfaces offline/network failures as status 0 ManageApiError', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('network down'));
    const error = await listManageWallets(BASE, 'jwt').catch((caught) => caught);
    expect(error).toBeInstanceOf(ManageApiError);
    expect((error as ManageApiError).status).toBe(0);
  });
});

describe('manage-api instances', () => {
  it('normalizes list responses with rules, modes, and online state', async () => {
    mockFetch(200, {
      instances: [
        {
          id: 77,
          label: 'Zurich GPU',
          peer_id: '12D3KooWTest',
          owner_wallet: '9xQeWvG816bUx9EPfEZ2vA8SxVYbdxM2f6u9QhQv5Kxa',
          access_mode: 'restricted',
          acl_rules: [{ kind: 'email_domain', value: 'example.org' }],
          observed_online: true,
          ownership_status: 'active',
          ownership_observed_at: '2026-07-29T10:00:00Z',
          policy_revision: 4,
        },
      ],
    });

    const result = await listManageInstances(BASE, 'jwt');
    expect(result.instances).toHaveLength(1);
    expect(result.instances[0].id).toBe('77');
    expect(result.instances[0]).toMatchObject({
      mode: 'restricted',
      online: true,
      policy_revision: 4,
      ownership_checked_at: '2026-07-29T10:00:00Z',
    });
    expect(result.instances[0].acl_rules).toEqual([
      { kind: 'email_domain', value: 'example.org' },
    ]);
  });

  it('creates an instance and sends the exact claim payload', async () => {
    const fetchSpy = mockFetch(201, {
      id: 'i1',
      label: 'Zurich GPU',
      peer_id: '12D3KooWTest',
      owner_wallet: '9xQeWvG816bUx9EPfEZ2vA8SxVYbdxM2f6u9QhQv5Kxa',
      mode: 'restricted',
      rules: [],
    });

    await createManageInstance(BASE, 'jwt', {
      peer_id: '12D3KooWTest',
      label: 'Zurich GPU',
    });

    expect(fetchSpy).toHaveBeenCalledWith(
      'https://api.example/manage/instances',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          peer_id: '12D3KooWTest',
          label: 'Zurich GPU',
        }),
      }),
    );
  });

  it('replaces ACLs with normalized body shape', async () => {
    const fetchSpy = mockFetch(200, {
      id: 'i1',
      peer_id: '12D3KooWTest',
      owner_wallet: '9xQeWvG816bUx9EPfEZ2vA8SxVYbdxM2f6u9QhQv5Kxa',
      mode: 'public',
      rules: [],
    });

    await replaceManageInstanceAcl(BASE, 'jwt', 'i1', {
      mode: 'public',
      rules: [{ kind: 'wallet', value: '9xQeWvG816bUx9EPfEZ2vA8SxVYbdxM2f6u9QhQv5Kxa' }],
    });

    expect(fetchSpy).toHaveBeenCalledWith(
      'https://api.example/manage/instances/i1/acl',
      expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({
          mode: 'public',
          rules: [
            {
              kind: 'wallet',
              value: '9xQeWvG816bUx9EPfEZ2vA8SxVYbdxM2f6u9QhQv5Kxa',
            },
          ],
        }),
      }),
    );
  });

  it('updates labels without sending a second access mode change', async () => {
	const fetchSpy = mockFetch(200, {
	  id: 'i1',
	  peer_id: '12D3KooWTest',
	  owner_wallet: '9xQeWvG816bUx9EPfEZ2vA8SxVYbdxM2f6u9QhQv5Kxa',
	  mode: 'restricted',
	  rules: [],
	});

	await updateManageInstance(BASE, 'jwt', 'i1', { label: 'Renamed' });

	expect(fetchSpy).toHaveBeenCalledWith(
	  'https://api.example/manage/instances/i1',
	  expect.objectContaining({
		method: 'PATCH',
		body: JSON.stringify({ label: 'Renamed' }),
	  }),
	);
  });

  it('maps 422 claim failures to ownership-verification guidance', async () => {
    mockFetch(422, { detail: 'peer not visible' });
    await expect(
      createManageInstance(BASE, 'jwt', { peer_id: 'bad-peer' }),
    ).rejects.toThrow(/peer not visible/i);
  });

  it('rejects non-lowercase rule kinds from the API payload', async () => {
    mockFetch(200, {
      instances: [
        {
          id: 1,
          peer_id: '12D3KooWTest',
          owner_wallet: '9xQeWvG816bUx9EPfEZ2vA8SxVYbdxM2f6u9QhQv5Kxa',
          acl_rules: [{ kind: 'Wallet', value: '9xQeWvG816bUx9EPfEZ2vA8SxVYbdxM2f6u9QhQv5Kxa' }],
        },
      ],
    });
    await expect(listManageInstances(BASE, 'jwt')).rejects.toThrow(/kind or value/i);
  });

  it('deletes instances on 204', async () => {
    mockFetch(204);
    await expect(deleteManageInstance(BASE, 'jwt', 'i1')).resolves.toBeUndefined();
  });
});
