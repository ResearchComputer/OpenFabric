import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cancelManageRegionInvitation,
  createManageInstance,
  createManageKey,
  createManageRegion,
  createManageRegionInvitation,
  createWalletChallenge,
  deleteManageInstance,
  deleteManageWallet,
  linkManageWallet,
  listManageInstanceServices,
  listManageInstances,
  listManageKeys,
  listManageRegions,
  listManageWallets,
  ManageApiError,
  replaceManageInstanceAcl,
  replaceManageInstanceServiceAcl,
  replaceManageInstanceServices,
  revokeManageKey,
  updateManageInstance,
  updateManageRegionMember,
} from "./manage-api";

const BASE = "https://api.example";
const WALLET = "9xQeWvG816bUx9EPfEZ2vA8SxVYbdxM2f6u9QhQv5Kxa";

afterEach(() => vi.restoreAllMocks());

function mockFetch(status: number, body?: unknown) {
  return vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), {
      status,
    }),
  );
}

describe("manage-api keys", () => {
  it("creates a key and returns the plaintext once", async () => {
    mockFetch(201, {
      id: "1",
      key: "sk-abc",
      prefix: "sk-1a2b",
      name: "laptop",
      created_at: "t",
    });
    const created = await createManageKey(BASE, "jwt", "laptop");
    expect(created.key).toBe("sk-abc");
    expect(created.prefix).toBe("sk-1a2b");
  });

  it("maps 409 to a key-cap error", async () => {
    mockFetch(409);
    await expect(createManageKey(BASE, "jwt")).rejects.toThrow(/limit/i);
  });

  it("maps 401 to a not-authenticated error", async () => {
    mockFetch(401);
    await expect(listManageKeys(BASE, "jwt")).rejects.toThrow(/authenticat/i);
  });

  it("lists keys", async () => {
    mockFetch(200, [
      {
        id: "1",
        name: null,
        prefix: "sk-1a2b",
        created_at: "t",
        revoked_at: null,
      },
    ]);
    const keys = await listManageKeys(BASE, "jwt");
    expect(keys).toHaveLength(1);
  });

  it("revokes on 204", async () => {
    mockFetch(204);
    await expect(revokeManageKey(BASE, "jwt", "1")).resolves.toBeUndefined();
  });

  it("maps revoke 404 to not-found", async () => {
    mockFetch(404);
    await expect(revokeManageKey(BASE, "jwt", "x")).rejects.toThrow(
      /no such key/i,
    );
  });
});

describe("manage-api wallets", () => {
  it("creates the wallet challenge with the expected path and body", async () => {
    const fetchSpy = mockFetch(200, {
      id: "c1",
      message: "sign this exact string",
      expires_at: "2026-07-29T12:00:00Z",
    });

    await expect(createWalletChallenge(BASE, "jwt", "abc")).resolves.toEqual({
      id: "c1",
      message: "sign this exact string",
      expires_at: "2026-07-29T12:00:00Z",
    });

    expect(fetchSpy).toHaveBeenCalledWith(
      "https://api.example/manage/wallets/challenges",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ wallet: "abc" }),
      }),
    );
  });

  it("links a wallet by posting challenge id plus base58 signature", async () => {
    const fetchSpy = mockFetch(201);
    await expect(
      linkManageWallet(BASE, "jwt", { challenge_id: "c1", signature: "5Z..." }),
    ).resolves.toBeUndefined();

    expect(fetchSpy).toHaveBeenCalledWith(
      "https://api.example/manage/wallets",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ challenge_id: "c1", signature: "5Z..." }),
      }),
    );
  });

  it("normalizes wallet lists plus optional identity metadata", async () => {
    mockFetch(200, {
      wallets: [
        {
          id: 101,
          wallet: WALLET,
          linked_at: "2026-07-29T10:00:00Z",
          primary: true,
        },
      ],
      identity: {
        email: "alice@example.org",
        email_domain: "example.org",
        email_verified: true,
        last_verified_at: "2026-07-29T10:00:00Z",
        expires_at: "2026-08-28T10:00:00Z",
        max_age_seconds: 2_592_000,
      },
    });

    const result = await listManageWallets(BASE, "jwt");
    expect(result.wallets[0].id).toBe("101");
    expect(result.wallets[0].wallet).toContain("9xQe");
    expect(result.wallets[0].is_primary).toBe(true);
    expect(result.identity?.email_domain).toBe("example.org");
  });

  it("maps wallet delete conflicts to ownership-proof messaging", async () => {
    mockFetch(409, { detail: "active owner proof" });
    await expect(deleteManageWallet(BASE, "jwt", "w1")).rejects.toThrow(
      /owner proof/i,
    );
  });

  it("preserves plain-text error bodies from http.Error responses", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response("challenge expired", {
        status: 400,
        headers: { "content-type": "text/plain; charset=utf-8" },
      }),
    );
    await expect(createWalletChallenge(BASE, "jwt", "abc")).rejects.toThrow(
      /challenge expired/i,
    );
  });

  it("surfaces offline/network failures as status 0 ManageApiError", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(
      new TypeError("network down"),
    );
    const error = await listManageWallets(BASE, "jwt").catch(
      (caught) => caught,
    );
    expect(error).toBeInstanceOf(ManageApiError);
    expect((error as ManageApiError).status).toBe(0);
  });
});

describe("manage-api instances", () => {
  it("normalizes list responses with peer scope and membership metadata", async () => {
    mockFetch(200, {
      instances: [
        {
          id: 77,
          label: "Zurich GPU",
          peer_id: "12D3KooWTest",
          owner_wallet: WALLET,
          access_mode: "restricted",
          acl_rules: [{ kind: "email_domain", value: "example.org" }],
          observed_online: true,
          ownership_status: "active",
          ownership_observed_at: "2026-07-29T10:00:00Z",
          policy_revision: 4,
          policy_scope: "service",
          region_membership: {
            region_id: 8,
            region_slug: "trusted-eu",
            region_name: "Trusted EU",
            node_role: "combined",
            status: "active",
            membership_revision: 2,
            ownership_verified_at: "2026-07-29T10:05:00Z",
            trusted_service_count: 1,
          },
        },
      ],
    });

    const result = await listManageInstances(BASE, "jwt");
    expect(result.instances).toHaveLength(1);
    expect(result.instances[0]).toMatchObject({
      id: "77",
      mode: "restricted",
      online: true,
      policy_revision: 4,
      policy_scope: "service",
      ownership_checked_at: "2026-07-29T10:00:00Z",
      membership: {
        region_slug: "trusted-eu",
        node_role: "combined",
        trusted_service_count: 1,
      },
    });
  });

  it("creates an instance and sends the exact claim payload", async () => {
    const fetchSpy = mockFetch(201, {
      id: "i1",
      label: "Zurich GPU",
      peer_id: "12D3KooWTest",
      owner_wallet: WALLET,
      mode: "restricted",
      rules: [],
    });

    await createManageInstance(BASE, "jwt", {
      peer_id: "12D3KooWTest",
      label: "Zurich GPU",
    });

    expect(fetchSpy).toHaveBeenCalledWith(
      "https://api.example/manage/instances",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          peer_id: "12D3KooWTest",
          label: "Zurich GPU",
        }),
      }),
    );
  });

  it("replaces ACLs with normalized body shape", async () => {
    const fetchSpy = mockFetch(200, {
      id: "i1",
      peer_id: "12D3KooWTest",
      owner_wallet: WALLET,
      mode: "public",
      rules: [],
    });

    await replaceManageInstanceAcl(BASE, "jwt", "i1", {
      mode: "public",
      rules: [{ kind: "wallet", value: WALLET }],
    });

    expect(fetchSpy).toHaveBeenCalledWith(
      "https://api.example/manage/instances/i1/acl",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({
          mode: "public",
          rules: [{ kind: "wallet", value: WALLET }],
        }),
      }),
    );
  });

  it("updates labels without sending a second access mode change", async () => {
    const fetchSpy = mockFetch(200, {
      id: "i1",
      peer_id: "12D3KooWTest",
      owner_wallet: WALLET,
      mode: "restricted",
      rules: [],
    });

    await updateManageInstance(BASE, "jwt", "i1", { label: "Renamed" });

    expect(fetchSpy).toHaveBeenCalledWith(
      "https://api.example/manage/instances/i1",
      expect.objectContaining({
        method: "PATCH",
        body: JSON.stringify({ label: "Renamed" }),
      }),
    );
  });

  it("maps 422 claim failures to ownership-verification guidance", async () => {
    mockFetch(422, { detail: "peer not visible" });
    await expect(
      createManageInstance(BASE, "jwt", { peer_id: "bad-peer" }),
    ).rejects.toThrow(/peer not visible/i);
  });

  it("rejects non-lowercase rule kinds from the API payload", async () => {
    mockFetch(200, {
      instances: [
        {
          id: 1,
          peer_id: "12D3KooWTest",
          owner_wallet: WALLET,
          acl_rules: [{ kind: "Wallet", value: WALLET }],
        },
      ],
    });
    await expect(listManageInstances(BASE, "jwt")).rejects.toThrow(
      /kind or value/i,
    );
  });

  it("deletes instances on 204", async () => {
    mockFetch(204);
    await expect(
      deleteManageInstance(BASE, "jwt", "i1"),
    ).resolves.toBeUndefined();
  });
});

describe("manage-api service policy", () => {
  it("normalizes permissionless, trusted, and disabled service bindings", async () => {
    mockFetch(200, {
      instance_id: "77",
      policy_scope: "service",
      policy_revision: 9,
      supports_service_policy_v2: true,
      observed_at: "2026-07-29T10:20:00Z",
      services: [
        {
          id: 1,
          service_name: "embeddings-public",
          exposure: "permissionless",
          region_id: null,
          region_slug: null,
          access_mode: "public",
          service_policy_revision: 2,
          observed_present: true,
          observed_last_seen_at: "2026-07-29T10:20:00Z",
          rules: [],
        },
        {
          id: 2,
          service_name: "llm-private",
          exposure: "trusted_region",
          region_id: 8,
          region_slug: "trusted-eu",
          access_mode: "restricted",
          service_policy_revision: 3,
          observed_present: true,
          observed_last_seen_at: "2026-07-29T10:20:00Z",
          rules: [{ kind: "email_domain", value: "example.org" }],
        },
        {
          id: 3,
          service_name: "experimental",
          exposure: "disabled",
          region_id: null,
          region_slug: null,
          access_mode: "inherit",
          service_policy_revision: 1,
          observed_present: false,
          observed_last_seen_at: "2026-07-29T10:00:00Z",
          rules: [],
        },
      ],
    });

    const snapshot = await listManageInstanceServices(BASE, "jwt", "77");
    expect(snapshot).toMatchObject({
      instance_id: "77",
      policy_scope: "service",
      policy_revision: 9,
      supports_service_policy_v2: true,
      observed_at: "2026-07-29T10:20:00Z",
      capability: { worker_capability: "ready" },
    });
    expect(snapshot.services.map((service) => service.exposure)).toEqual([
      "permissionless",
      "trusted_region",
      "disabled",
    ]);
    expect(snapshot.services[1].acl_rules).toEqual([
      { kind: "email_domain", value: "example.org" },
    ]);
  });

  it("sends the frozen inventory payload with scope reset acknowledgement", async () => {
    const fetchSpy = mockFetch(200, {
      instance_id: "77",
      policy_scope: "peer",
      policy_revision: 10,
      supports_service_policy_v2: true,
      observed_at: "2026-07-29T10:30:00Z",
      services: [],
    });

    await replaceManageInstanceServices(BASE, "jwt", "77", {
      policy_scope: "peer",
      acknowledge_scope_reset: true,
      services: [
        {
          service_name: "embeddings-public",
          exposure: "permissionless",
          region_id: null,
          access_mode: "public",
          acl_rules: [],
        },
      ],
    });

    expect(fetchSpy).toHaveBeenCalledWith(
      "https://api.example/manage/instances/77/services",
      expect.objectContaining({
        method: "PUT",
        body: expect.any(String),
      }),
    );
    expect(JSON.parse(fetchSpy.mock.calls[0][1]?.body as string)).toEqual({
      policy_scope: "peer",
      services: [
        {
          service_name: "embeddings-public",
          exposure: "permissionless",
          region_id: null,
          access_mode: "public",
          rules: [],
        },
      ],
      acknowledge_scope_reset: true,
    });
  });

  it("updates one service ACL through the canonical acl route", async () => {
    const fetchSpy = mockFetch(200, {
      id: 2,
      service_name: "llm-private",
      exposure: "trusted_region",
      region_id: 8,
      region_slug: "trusted-eu",
      access_mode: "restricted",
      service_policy_revision: 3,
      observed_present: true,
      observed_last_seen_at: "2026-07-29T10:20:00Z",
      rules: [{ kind: "wallet", value: WALLET }],
    });

    const result = await replaceManageInstanceServiceAcl(
      BASE,
      "jwt",
      "77",
      "2",
      {
        access_mode: "restricted",
        rules: [{ kind: "wallet", value: WALLET }],
      },
    );

    expect(result.acl_rules).toEqual([{ kind: "wallet", value: WALLET }]);
    expect(fetchSpy).toHaveBeenCalledWith(
      "https://api.example/manage/instances/77/services/2/acl",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({
          access_mode: "restricted",
          rules: [{ kind: "wallet", value: WALLET }],
        }),
      }),
    );
  });
});

describe("manage-api trusted regions", () => {
  it("normalizes region, member, and invitation payloads", async () => {
    mockFetch(200, {
      regions: [
        {
          id: 8,
          slug: "trusted-eu",
          name: "Trusted EU",
          status: "active",
          region_revision: 5,
          members: [
            {
              instance_id: 77,
              peer_id: "12D3KooWTest",
              label: "Zurich GPU",
              node_role: "combined",
              status: "active",
              membership_revision: 2,
              ownership_verified_at: "2026-07-29T10:05:00Z",
              trusted_service_count: 1,
            },
          ],
          invitations: [
            {
              id: 101,
              instance_id: 99,
              peer_id: "12D3KooWInvite",
              label: "Bern head",
              node_role: "head",
              status: "pending",
              expires_at: "2026-08-01T00:00:00Z",
              created_at: "2026-07-29T11:00:00Z",
            },
          ],
        },
      ],
    });

    const snapshot = await listManageRegions(BASE, "jwt");
    expect(snapshot.regions[0]).toMatchObject({
      slug: "trusted-eu",
      members: [
        {
          instance_id: "77",
          node_role: "combined",
          trusted_service_count: 1,
        },
      ],
      invitations: [
        {
          id: "101",
          node_role: "head",
          status: "pending",
        },
      ],
    });
  });

  it("creates a trusted region through the canonical route", async () => {
    const fetchSpy = mockFetch(201, {
      id: 8,
      slug: "trusted-eu",
      name: "Trusted EU",
      status: "active",
      region_revision: 1,
      members: [],
      invitations: [],
    });

    await createManageRegion(BASE, "jwt", {
      slug: "trusted-eu",
      name: "Trusted EU",
    });

    expect(fetchSpy).toHaveBeenCalledWith(
      "https://api.example/manage/regions",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ slug: "trusted-eu", name: "Trusted EU" }),
      }),
    );
  });

  it("updates members through the canonical members route", async () => {
    const fetchSpy = mockFetch(200, {
      instance_id: 77,
      peer_id: "12D3KooWTest",
      label: "Zurich GPU",
      node_role: "head",
      status: "suspended",
      membership_revision: 3,
      trusted_service_count: 0,
    });

    await updateManageRegionMember(BASE, "jwt", "8", "77", {
      node_role: "head",
      status: "suspended",
    });

    expect(fetchSpy).toHaveBeenCalledWith(
      "https://api.example/manage/regions/8/members/77",
      expect.objectContaining({
        method: "PATCH",
        body: JSON.stringify({ node_role: "head", status: "suspended" }),
      }),
    );
  });

  it("creates invitations through the canonical members route", async () => {
    const fetchSpy = mockFetch(201, {
      id: 101,
      instance_id: 99,
      node_role: "worker",
      status: "pending",
    });
    await createManageRegionInvitation(BASE, "jwt", "8", {
      instance_id: "99",
      node_role: "worker",
    });
    expect(fetchSpy).toHaveBeenCalledWith(
      "https://api.example/manage/regions/8/members",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          instance_id: 99,
          node_role: "worker",
          expires_at: null,
          auto_accept: false,
        }),
      }),
    );
  });

  it("cancels invitations through the canonical members route", async () => {
    const fetchSpy = mockFetch(204);
    await cancelManageRegionInvitation(BASE, "jwt", "8", "99");
    expect(fetchSpy).toHaveBeenCalledWith(
      "https://api.example/manage/regions/8/members/99",
      expect.objectContaining({ method: "DELETE" }),
    );
  });
});
