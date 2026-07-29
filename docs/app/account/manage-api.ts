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

export type ManageInstanceAccessMode = "public" | "restricted";
export type ManageAclRuleKind = "email_domain" | "wallet";
export type ManageInstancePolicyScope = "peer" | "service";
export type ManageServiceExposure =
  | "permissionless"
  | "trusted_region"
  | "disabled";
export type ManageServiceAccessMode = "inherit" | "public" | "restricted";
export type ManageRegionStatus = "active" | "disabled";
export type ManageNodeRole = "worker" | "head" | "combined";
export type ManageMembershipStatus =
  | "pending"
  | "active"
  | "suspended"
  | "expired"
  | "revoked"
  | "ownership_mismatch"
  | "ownership_unavailable";
export type ManageInvitationStatus =
  | "pending"
  | "accepted"
  | "expired"
  | "cancelled";
export type ManageWorkerCapability = "ready" | "legacy" | "stale" | "unknown";

export interface ManageAclRule {
  kind: ManageAclRuleKind;
  value: string;
}

export interface ManageRegionOption {
  id: string;
  slug: string;
  name: string;
  status: ManageRegionStatus;
}

export interface ManageRegionMembership {
  region_id: string;
  region_slug: string;
  region_name: string;
  node_role: ManageNodeRole;
  status: ManageMembershipStatus;
  membership_revision: number | null;
  expires_at: string | null;
  ownership_verified_at: string | null;
  trusted_service_count: number | null;
}

export interface ManageRegionMember {
  instance_id: string;
  peer_id: string | null;
  label: string | null;
  node_role: ManageNodeRole;
  status: ManageMembershipStatus;
  membership_revision: number | null;
  expires_at: string | null;
  ownership_verified_at: string | null;
  trusted_service_count: number | null;
}

export interface ManageRegionInvitation {
  id: string;
  instance_id: string;
  peer_id: string | null;
  label: string | null;
  node_role: ManageNodeRole;
  status: ManageInvitationStatus;
  expires_at: string | null;
  created_at: string | null;
}

export interface ManageRegion {
  id: string;
  slug: string;
  name: string;
  status: ManageRegionStatus;
  region_revision: number | null;
  members: ManageRegionMember[];
  invitations: ManageRegionInvitation[];
}

export interface ManageRegionsSnapshot {
  regions: ManageRegion[];
  identity: ManageIdentitySnapshot | null;
}

export interface ManageObservedService {
  service_name: string;
  observed_present: boolean;
  observed_last_seen_at: string | null;
  duplicate_live: boolean;
}

export interface ManageServiceCapability {
  worker_capability: ManageWorkerCapability;
  capability_checked_at: string | null;
  blockers: string[];
  exact_name_duplicates: string[];
}

export interface ManageInstanceService {
  id: string | null;
  service_name: string;
  exposure: ManageServiceExposure;
  region_id: string | null;
  region_slug: string | null;
  access_mode: ManageServiceAccessMode;
  acl_rules: ManageAclRule[];
  service_policy_revision: number | null;
  observed_present: boolean;
  observed_last_seen_at: string | null;
  duplicate_live: boolean;
}

export interface ManageInstanceServiceSnapshot {
  instance_id: string;
  policy_scope: ManageInstancePolicyScope;
  policy_revision: number | null;
  supports_service_policy_v2: boolean | null;
  observed_at: string | null;
  services: ManageInstanceService[];
  available_regions: ManageRegionOption[];
  observed_services: ManageObservedService[];
  capability: ManageServiceCapability | null;
  identity: ManageIdentitySnapshot | null;
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
  policy_scope: ManageInstancePolicyScope;
  membership: ManageRegionMembership | null;
}

export interface ManageInstancesSnapshot {
  instances: ManageInstance[];
  identity: ManageIdentitySnapshot | null;
}

export interface ManageRegionCreateInput {
  slug: string;
  name: string;
}

export interface ManageRegionUpdateInput {
  name?: string;
  status?: ManageRegionStatus;
}

export interface ManageRegionMemberCreateInput {
  instance_id: string;
  node_role: ManageNodeRole;
  expires_at?: string | null;
}

export interface ManageRegionMemberUpdateInput {
  node_role?: ManageNodeRole;
  status?: Exclude<ManageMembershipStatus, "pending">;
  expires_at?: string | null;
}

export interface ManageInstanceServiceDraft {
  id?: string | null;
  service_name: string;
  exposure: ManageServiceExposure;
  region_id?: string | null;
  access_mode: ManageServiceAccessMode;
  acl_rules?: ManageAclRule[];
}

export interface ManageInstanceServicesReplaceInput {
  policy_scope: ManageInstancePolicyScope;
  services: ManageInstanceServiceDraft[];
  acknowledge_scope_reset?: boolean;
}

export interface ManageServiceAclInput {
  access_mode: ManageServiceAccessMode;
  rules: ManageAclRule[];
}

export class ManageApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ManageApiError";
    this.status = status;
  }
}

type RecordValue = Record<string, unknown>;
type NormalizedSnapshot<T> = {
  items: T[];
  identity: ManageIdentitySnapshot | null;
};

function authHeaders(jwt: string): HeadersInit {
  return { Authorization: `Bearer ${jwt}`, "Content-Type": "application/json" };
}

function isRecord(value: unknown): value is RecordValue {
  return typeof value === "object" && value !== null;
}

function readString(value: unknown): string | null {
  return typeof value === "string" ? value : null;
}

function readId(value: unknown): string | null {
  if (typeof value === "string" && value.length > 0) return value;
  if (
    typeof value === "number" &&
    Number.isInteger(value) &&
    Number.isFinite(value) &&
    value >= 0
  ) {
    return String(value);
  }
  return null;
}

function readBoolean(value: unknown, fallback = false): boolean {
  return typeof value === "boolean" ? value : fallback;
}

function readArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function readNumber(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function readStringArray(value: unknown): string[] {
  return readArray(value)
    .map(readString)
    .filter((entry): entry is string => entry !== null);
}

function readFirstString(
  value: RecordValue,
  keys: readonly string[],
): string | null {
  for (const key of keys) {
    const result = readString(value[key]);
    if (result !== null) return result;
  }
  return null;
}

function readFirstId(
  value: RecordValue,
  keys: readonly string[],
): string | null {
  for (const key of keys) {
    const result = readId(value[key]);
    if (result !== null) return result;
  }
  return null;
}

async function readErrorDetail(response: Response): Promise<string> {
  const contentType = response.headers.get("content-type") ?? "";
  try {
    const text = (await response.text()).trim();
    if (!text) return response.statusText || `HTTP ${response.status}`;
    if (/json/i.test(contentType)) {
      const body = JSON.parse(text) as {
        error?: string;
        detail?: string;
        message?: string;
      } | null;
      if (body && typeof body === "object") {
        return body.detail ?? body.error ?? body.message ?? text;
      }
    }
    return text;
  } catch {
    return response.statusText || `HTTP ${response.status}`;
  }
}

function requestFailedMessage(error: unknown): string {
  if (typeof navigator !== "undefined" && navigator.onLine === false) {
    return "You appear offline — reconnect and try again";
  }
  return error instanceof Error
    ? error.message
    : "The request could not be completed";
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
    throw new Error("Wallet payload must be an object");
  }

  const id = readId(value.id);
  const wallet = readString(value.wallet);
  if (!id || !wallet) {
    throw new Error("Wallet payload is missing id or wallet");
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
    throw new Error("ACL rule payload must be an object");
  }

  const kind = readString(value.kind);
  const rawValue = readString(value.value);
  if (
    (kind !== "email_domain" && kind !== "wallet") ||
    kind !== kind.toLowerCase() ||
    !rawValue
  ) {
    throw new Error("ACL rule payload is missing kind or value");
  }

  return { kind, value: rawValue };
}

function normalizeInstanceMode(value: unknown): ManageInstanceAccessMode {
  return readString(value) === "public" ? "public" : "restricted";
}

function normalizePolicyScope(value: unknown): ManageInstancePolicyScope {
  return readString(value) === "service" ? "service" : "peer";
}

function normalizeServiceExposure(value: unknown): ManageServiceExposure {
  switch (readString(value)) {
    case "permissionless":
      return "permissionless";
    case "trusted_region":
      return "trusted_region";
    default:
      return "disabled";
  }
}

function normalizeServiceAccessMode(value: unknown): ManageServiceAccessMode {
  switch (readString(value)) {
    case "inherit":
      return "inherit";
    case "public":
      return "public";
    default:
      return "restricted";
  }
}

function normalizeRegionStatus(value: unknown): ManageRegionStatus {
  return readString(value) === "disabled" ? "disabled" : "active";
}

function normalizeNodeRole(value: unknown): ManageNodeRole {
  switch (readString(value)) {
    case "head":
      return "head";
    case "combined":
      return "combined";
    default:
      return "worker";
  }
}

function normalizeMembershipStatus(value: unknown): ManageMembershipStatus {
  switch (readString(value)) {
    case "pending":
      return "pending";
    case "suspended":
      return "suspended";
    case "expired":
      return "expired";
    case "revoked":
      return "revoked";
    case "ownership_mismatch":
      return "ownership_mismatch";
    case "ownership_unavailable":
      return "ownership_unavailable";
    default:
      return "active";
  }
}

function normalizeInvitationStatus(value: unknown): ManageInvitationStatus {
  switch (readString(value)) {
    case "accepted":
      return "accepted";
    case "expired":
      return "expired";
    case "cancelled":
      return "cancelled";
    default:
      return "pending";
  }
}

function normalizeWorkerCapability(value: unknown): ManageWorkerCapability {
  switch (readString(value)) {
    case "ready":
      return "ready";
    case "legacy":
      return "legacy";
    case "stale":
      return "stale";
    default:
      return "unknown";
  }
}

function readInstanceRules(value: RecordValue): unknown[] {
  const aclRules = value.acl_rules;
  if (Array.isArray(aclRules)) return aclRules;

  const rules = value.rules;
  if (Array.isArray(rules)) return rules;

  return [];
}

function readInstanceOnline(value: RecordValue): boolean | null {
  if (typeof value.online === "boolean") return value.online;
  if (typeof value.observed_online === "boolean") return value.observed_online;

  const status = readString(value.status);
  if (status === null) return null;
  return status === "online";
}

function readOwnershipCheckedAt(value: RecordValue): string | null {
  return readFirstString(value, [
    "ownership_observed_at",
    "ownership_checked_at",
    "last_verified_at",
    "last_observed_at",
  ]);
}

function normalizeRegionOption(value: unknown): ManageRegionOption {
  if (!isRecord(value)) {
    throw new Error("Region payload must be an object");
  }

  const id = readFirstId(value, ["id", "region_id"]);
  const slug = readFirstString(value, ["slug", "region_slug"]);
  const name = readFirstString(value, ["name", "region_name"]);
  if (!id || !slug || !name) {
    throw new Error("Region payload is missing id, slug, or name");
  }

  return {
    id,
    slug,
    name,
    status: normalizeRegionStatus(value.status),
  };
}

function normalizeMembership(value: unknown): ManageRegionMembership | null {
  if (!isRecord(value)) return null;

  const regionId = readFirstId(value, ["region_id", "id"]);
  const regionSlug = readFirstString(value, ["region_slug", "slug"]);
  const regionName = readFirstString(value, ["region_name", "name"]);
  if (!regionId || !regionSlug || !regionName) return null;

  return {
    region_id: regionId,
    region_slug: regionSlug,
    region_name: regionName,
    node_role: normalizeNodeRole(value.node_role),
    status: normalizeMembershipStatus(value.status),
    membership_revision: readNumber(value.membership_revision),
    expires_at: readString(value.expires_at),
    ownership_verified_at: readFirstString(value, [
      "ownership_verified_at",
      "ownership_checked_at",
      "ownership_observed_at",
    ]),
    trusted_service_count: readNumber(value.trusted_service_count),
  };
}

function normalizeRegionMember(value: unknown): ManageRegionMember {
  if (!isRecord(value)) {
    throw new Error("Region member payload must be an object");
  }

  const instanceId = readFirstId(value, ["instance_id", "id"]);
  if (!instanceId) {
    throw new Error("Region member payload is missing instance_id");
  }

  return {
    instance_id: instanceId,
    peer_id: readFirstString(value, ["peer_id", "peerId"]),
    label: readString(value.label),
    node_role: normalizeNodeRole(value.node_role),
    status: normalizeMembershipStatus(value.status),
    membership_revision: readNumber(value.membership_revision),
    expires_at: readString(value.expires_at),
    ownership_verified_at: readFirstString(value, [
      "ownership_verified_at",
      "ownership_checked_at",
      "ownership_observed_at",
    ]),
    trusted_service_count: readNumber(value.trusted_service_count),
  };
}

function normalizeRegionInvitation(value: unknown): ManageRegionInvitation {
  if (!isRecord(value)) {
    throw new Error("Region invitation payload must be an object");
  }

  const id = readId(value.id);
  const instanceId = readFirstId(value, ["instance_id", "id"]);
  if (!id || !instanceId) {
    throw new Error("Region invitation payload is missing id or instance_id");
  }

  return {
    id,
    instance_id: instanceId,
    peer_id: readFirstString(value, ["peer_id", "peerId"]),
    label: readString(value.label),
    node_role: normalizeNodeRole(value.node_role),
    status: normalizeInvitationStatus(value.status),
    expires_at: readString(value.expires_at),
    created_at: readString(value.created_at),
  };
}

function normalizeRegion(value: unknown): ManageRegion {
  if (!isRecord(value)) {
    throw new Error("Region payload must be an object");
  }

  const option = normalizeRegionOption(value);
  return {
    ...option,
    region_revision: readNumber(value.region_revision),
    members: readArray(value.members).map(normalizeRegionMember),
    invitations: readArray(value.invitations).map(normalizeRegionInvitation),
  };
}

function normalizeObservedService(value: unknown): ManageObservedService {
  if (!isRecord(value)) {
    throw new Error("Observed service payload must be an object");
  }

  const serviceName = readFirstString(value, ["service_name", "name"]);
  if (!serviceName) {
    throw new Error("Observed service payload is missing service_name");
  }

  return {
    service_name: serviceName,
    observed_present: readBoolean(value.observed_present, true),
    observed_last_seen_at: readFirstString(value, [
      "observed_last_seen_at",
      "last_seen_at",
      "observed_at",
    ]),
    duplicate_live: readBoolean(value.duplicate_live),
  };
}

function normalizeCapability(value: unknown): ManageServiceCapability | null {
  if (!isRecord(value)) return null;
  return {
    worker_capability: normalizeWorkerCapability(
      value.worker_capability ?? value.state ?? value.status,
    ),
    capability_checked_at: readFirstString(value, [
      "capability_checked_at",
      "checked_at",
      "observed_at",
    ]),
    blockers: readStringArray(value.blockers),
    exact_name_duplicates: readStringArray(
      value.exact_name_duplicates ?? value.duplicate_service_names,
    ),
  };
}

function normalizeService(value: unknown): ManageInstanceService {
  if (!isRecord(value)) {
    throw new Error("Service payload must be an object");
  }

  const serviceName = readFirstString(value, ["service_name", "name"]);
  if (!serviceName) {
    throw new Error("Service payload is missing service_name");
  }

  return {
    id: readId(value.id),
    service_name: serviceName,
    exposure: normalizeServiceExposure(value.exposure),
    region_id: readFirstId(value, ["region_id"]),
    region_slug: readFirstString(value, ["region_slug"]),
    access_mode: normalizeServiceAccessMode(value.access_mode ?? value.mode),
    acl_rules: readInstanceRules(value).map(normalizeRule),
    service_policy_revision: readNumber(value.service_policy_revision),
    observed_present: readBoolean(value.observed_present),
    observed_last_seen_at: readFirstString(value, [
      "observed_last_seen_at",
      "last_seen_at",
      "observed_at",
    ]),
    duplicate_live: readBoolean(value.duplicate_live),
  };
}

function normalizeInstance(value: unknown): ManageInstance {
  if (!isRecord(value)) {
    throw new Error("Instance payload must be an object");
  }

  const id = readId(value.id);
  const peerId = readFirstString(value, ["peer_id", "peerId"]);
  const ownerWallet = readFirstString(value, ["owner_wallet", "ownerWallet"]);
  if (!id || !peerId || !ownerWallet) {
    throw new Error("Instance payload is missing id, peer_id, or owner_wallet");
  }

  return {
    id,
    label: readString(value.label),
    peer_id: peerId,
    owner_wallet: ownerWallet,
    mode: normalizeInstanceMode(value.mode ?? value.access_mode),
    acl_rules: readInstanceRules(value).map(normalizeRule),
    online: readInstanceOnline(value),
    ownership_status: readFirstString(value, [
      "ownership_status",
      "verification_status",
    ]),
    ownership_checked_at: readOwnershipCheckedAt(value),
    policy_revision: readNumber(value.policy_revision),
    policy_scope: normalizePolicyScope(value.policy_scope),
    membership:
      normalizeMembership(value.region_membership) ??
      normalizeMembership(value.membership) ??
      null,
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
    "wallets",
    normalizeWallet,
    "Wallet list payload must be an array or object",
  );
  return { wallets: snapshot.items, identity: snapshot.identity };
}

function normalizeInstances(body: unknown): ManageInstancesSnapshot {
  const snapshot = normalizeSnapshot(
    body,
    "instances",
    normalizeInstance,
    "Instance list payload must be an array or object",
  );
  return { instances: snapshot.items, identity: snapshot.identity };
}

function normalizeRegions(body: unknown): ManageRegionsSnapshot {
  const snapshot = normalizeSnapshot(
    body,
    "regions",
    normalizeRegion,
    "Region list payload must be an array or object",
  );
  return { regions: snapshot.items, identity: snapshot.identity };
}

function normalizeServiceSnapshot(
  body: unknown,
  instanceId: string,
): ManageInstanceServiceSnapshot {
  if (Array.isArray(body)) {
    return {
      instance_id: instanceId,
      policy_scope: "peer",
      policy_revision: null,
      supports_service_policy_v2: null,
      observed_at: null,
      services: body.map(normalizeService),
      available_regions: [],
      observed_services: [],
      capability: null,
      identity: null,
    };
  }

  if (!isRecord(body)) {
    throw new Error("Service inventory payload must be an array or object");
  }

  const instance = isRecord(body.instance)
    ? normalizeInstance(body.instance)
    : null;
  const observedAt = readFirstString(body, ["observed_at"]);
  const supportsServicePolicyV2 =
    typeof body.supports_service_policy_v2 === "boolean"
      ? body.supports_service_policy_v2
      : null;
  const capability =
    normalizeCapability(body.capability) ??
    (supportsServicePolicyV2 !== null || observedAt !== null
      ? {
          worker_capability:
            supportsServicePolicyV2 === true
              ? "ready"
              : supportsServicePolicyV2 === false
                ? "legacy"
                : "unknown",
          capability_checked_at: observedAt,
          blockers:
            supportsServicePolicyV2 === false
              ? [
                  "This peer does not yet report service-policy v2 support, so service-managed mode must stay blocked.",
                ]
              : [],
          exact_name_duplicates: [],
        }
      : null);
  return {
    instance_id:
      readFirstId(body, ["instance_id"]) ?? instance?.id ?? instanceId,
    policy_scope: normalizePolicyScope(
      body.policy_scope ?? instance?.policy_scope,
    ),
    policy_revision: readNumber(body.policy_revision),
    supports_service_policy_v2: supportsServicePolicyV2,
    observed_at: observedAt,
    services: readArray(body.services).map(normalizeService),
    available_regions: readArray(body.available_regions ?? body.regions).map(
      normalizeRegionOption,
    ),
    observed_services: readArray(body.observed_services ?? body.observed).map(
      normalizeObservedService,
    ),
    capability,
    identity:
      normalizeIdentity(body.identity) ??
      normalizeIdentity(body.email_identity) ??
      null,
  };
}

function keyError(status: number): string {
  switch (status) {
    case 400:
      return "Bad request (name too long?)";
    case 401:
      return "Not authenticated — sign in again";
    case 409:
      return "Key limit reached";
    default:
      return `Key request failed: ${status}`;
  }
}

function walletListError(status: number): string {
  switch (status) {
    case 401:
      return "Not authenticated — sign in again";
    case 503:
      return "Wallet bindings are temporarily unavailable";
    default:
      return `Wallet list failed: ${status}`;
  }
}

function walletChallengeError(status: number, detail: string): string {
  switch (status) {
    case 400:
      return detail || "Enter a valid Solana wallet address";
    case 401:
      return "Not authenticated — sign in again";
    case 409:
      return detail || "This wallet is already linked elsewhere";
    case 503:
      return "Wallet challenge service is temporarily unavailable";
    default:
      return detail || `Wallet challenge failed: ${status}`;
  }
}

function walletLinkError(status: number, detail: string): string {
  switch (status) {
    case 400:
      return detail || "The wallet proof is invalid, expired, or already used";
    case 401:
      return "Not authenticated — sign in again";
    case 409:
      return detail || "This wallet is already linked to another account";
    case 503:
      return "Wallet verification is temporarily unavailable";
    default:
      return detail || `Wallet link failed: ${status}`;
  }
}

function walletDeleteError(status: number, detail: string): string {
  switch (status) {
    case 401:
      return "Not authenticated — sign in again";
    case 404:
      return "No such linked wallet for this account";
    case 409:
      return (
        detail || "This wallet still proves ownership of an active instance"
      );
    default:
      return detail || `Wallet delete failed: ${status}`;
  }
}

function instanceListError(status: number): string {
  switch (status) {
    case 401:
      return "Not authenticated — sign in again";
    case 503:
      return "Instance management is temporarily unavailable";
    default:
      return `Instance list failed: ${status}`;
  }
}

function instanceCreateError(status: number, detail: string): string {
  switch (status) {
    case 400:
      return detail || "Enter a valid peer ID and label";
    case 401:
      return "Not authenticated — sign in again";
    case 409:
      return detail || "That instance is already claimed";
    case 422:
      return (
        detail ||
        "This peer is offline, unverifiable, or not owned by one of your linked wallets"
      );
    case 503:
      return "Ownership verification is temporarily unavailable";
    default:
      return detail || `Create failed: ${status}`;
  }
}

function instanceUpdateError(status: number, detail: string): string {
  switch (status) {
    case 400:
      return detail || "The instance update is invalid";
    case 401:
      return "Not authenticated — sign in again";
    case 404:
      return "No such instance for this account";
    case 409:
      return (
        detail ||
        "This instance cannot be changed until ownership is re-verified"
      );
    case 422:
      return detail || "Ownership verification failed for this instance";
    case 503:
      return "Instance verification is temporarily unavailable";
    default:
      return detail || `Update failed: ${status}`;
  }
}

function instanceDeleteError(status: number, detail: string): string {
  switch (status) {
    case 401:
      return "Not authenticated — sign in again";
    case 404:
      return "No such instance for this account";
    case 503:
      return "Instance deletion is temporarily unavailable";
    default:
      return detail || `Delete failed: ${status}`;
  }
}

function serviceInventoryError(status: number, detail: string): string {
  switch (status) {
    case 400:
      return detail || "The service inventory is invalid";
    case 401:
      return "Not authenticated — sign in again";
    case 404:
      return "No such instance for this account";
    case 409:
      return (
        detail ||
        "This instance must disable or reclassify trusted services before changing scope"
      );
    case 422:
      return (
        detail ||
        "This peer cannot enter service-managed mode until worker capability and observed services are fresh"
      );
    case 503:
      return "Service inventory is temporarily unavailable";
    default:
      return detail || `Service inventory failed: ${status}`;
  }
}

function regionListError(status: number): string {
  switch (status) {
    case 401:
      return "Not authenticated — sign in again";
    case 503:
      return "Trusted-region management is temporarily unavailable";
    default:
      return `Region list failed: ${status}`;
  }
}

function regionWriteError(status: number, detail: string): string {
  switch (status) {
    case 400:
      return detail || "This trusted-region change is invalid";
    case 401:
      return "Not authenticated — sign in again";
    case 404:
      return "No such trusted region for this account";
    case 409:
      return (
        detail ||
        "This trusted-region lifecycle change is blocked by active bindings or invitations"
      );
    case 422:
      return (
        detail ||
        "This instance cannot be invited or admitted into the selected trusted region"
      );
    case 503:
      return "Trusted-region management is temporarily unavailable";
    default:
      return detail || `Trusted-region update failed: ${status}`;
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
      method: "POST",
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
          return "Not authenticated — sign in again";
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
      method: "DELETE",
      headers: authHeaders(jwt),
    },
    (status) => {
      switch (status) {
        case 404:
          return "No such key for this account";
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
      method: "POST",
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
      method: "POST",
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
      method: "DELETE",
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
      method: "POST",
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
      method: "PATCH",
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
      method: "PUT",
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
      method: "DELETE",
      headers: authHeaders(jwt),
    },
    instanceDeleteError,
  );
}

export async function listManageInstanceServices(
  baseUrl: string,
  jwt: string,
  instanceId: string,
): Promise<ManageInstanceServiceSnapshot> {
  const body = await requestJson<unknown>(
    `${baseUrl}/manage/instances/${encodeURIComponent(instanceId)}/services`,
    { headers: authHeaders(jwt) },
    serviceInventoryError,
  );
  return normalizeServiceSnapshot(body, instanceId);
}

export async function replaceManageInstanceServices(
  baseUrl: string,
  jwt: string,
  instanceId: string,
  input: ManageInstanceServicesReplaceInput,
): Promise<ManageInstanceServiceSnapshot> {
  const body = await requestJson<unknown>(
    `${baseUrl}/manage/instances/${encodeURIComponent(instanceId)}/services`,
    {
      method: "PUT",
      headers: authHeaders(jwt),
      body: JSON.stringify({
        policy_scope: input.policy_scope,
        services: input.services.map((service) => ({
          service_name: service.service_name,
          exposure: service.exposure,
          region_id: service.region_id ?? null,
          access_mode: service.access_mode,
          rules: service.acl_rules ?? [],
        })),
        acknowledge_scope_reset: input.acknowledge_scope_reset ?? false,
      }),
    },
    serviceInventoryError,
  );
  return normalizeServiceSnapshot(body, instanceId);
}

export async function replaceManageInstanceServiceAcl(
  baseUrl: string,
  jwt: string,
  instanceId: string,
  serviceId: string,
  input: ManageServiceAclInput,
): Promise<ManageInstanceService> {
  const body = await requestJson<unknown>(
    `${baseUrl}/manage/instances/${encodeURIComponent(instanceId)}/services/${encodeURIComponent(serviceId)}/acl`,
    {
      method: "PUT",
      headers: authHeaders(jwt),
      body: JSON.stringify({
        access_mode: input.access_mode,
        rules: input.rules,
      }),
    },
    serviceInventoryError,
  );
  return normalizeService(body);
}

export async function listManageRegions(
  baseUrl: string,
  jwt: string,
): Promise<ManageRegionsSnapshot> {
  const body = await requestJson<unknown>(
    `${baseUrl}/manage/regions`,
    { headers: authHeaders(jwt) },
    (status) => regionListError(status),
  );
  return normalizeRegions(body);
}

export async function createManageRegion(
  baseUrl: string,
  jwt: string,
  input: ManageRegionCreateInput,
): Promise<ManageRegion> {
  const body = await requestJson<unknown>(
    `${baseUrl}/manage/regions`,
    {
      method: "POST",
      headers: authHeaders(jwt),
      body: JSON.stringify(input),
    },
    regionWriteError,
  );
  return normalizeRegion(body);
}

export async function updateManageRegion(
  baseUrl: string,
  jwt: string,
  regionId: string,
  input: ManageRegionUpdateInput,
): Promise<ManageRegion> {
  const body = await requestJson<unknown>(
    `${baseUrl}/manage/regions/${encodeURIComponent(regionId)}`,
    {
      method: "PATCH",
      headers: authHeaders(jwt),
      body: JSON.stringify(input),
    },
    regionWriteError,
  );
  return normalizeRegion(body);
}

export async function addManageRegionMember(
  baseUrl: string,
  jwt: string,
  regionId: string,
  input: ManageRegionMemberCreateInput,
): Promise<void> {
  const instanceId = Number(input.instance_id);
  if (!Number.isSafeInteger(instanceId) || instanceId <= 0) {
    throw new ManageApiError("The selected instance id is invalid", 0);
  }
  const body = await requestJson<unknown>(
    `${baseUrl}/manage/regions/${encodeURIComponent(regionId)}/members`,
    {
      method: "POST",
      headers: authHeaders(jwt),
      body: JSON.stringify({
        instance_id: instanceId,
        node_role: input.node_role,
        expires_at: input.expires_at ?? null,
        auto_accept: true,
      }),
    },
    regionWriteError,
  );
  if (!isRecord(body) || readId(body.instance_id) === null) {
    throw new Error("Region membership response is missing instance_id");
  }
}

export async function cancelManageRegionInvitation(
  baseUrl: string,
  jwt: string,
  regionId: string,
  instanceId: string,
): Promise<void> {
  await request(
    `${baseUrl}/manage/regions/${encodeURIComponent(regionId)}/members/${encodeURIComponent(instanceId)}`,
    {
      method: "DELETE",
      headers: authHeaders(jwt),
    },
    regionWriteError,
  );
}

export async function updateManageRegionMember(
  baseUrl: string,
  jwt: string,
  regionId: string,
  instanceId: string,
  input: ManageRegionMemberUpdateInput,
): Promise<ManageRegionMember> {
  const body = await requestJson<unknown>(
    `${baseUrl}/manage/regions/${encodeURIComponent(regionId)}/members/${encodeURIComponent(instanceId)}`,
    {
      method: "PATCH",
      headers: authHeaders(jwt),
      body: JSON.stringify(input),
    },
    regionWriteError,
  );
  return normalizeRegionMember(body);
}

export async function deleteManageRegionMember(
  baseUrl: string,
  jwt: string,
  regionId: string,
  instanceId: string,
): Promise<void> {
  await request(
    `${baseUrl}/manage/regions/${encodeURIComponent(regionId)}/members/${encodeURIComponent(instanceId)}`,
    {
      method: "DELETE",
      headers: authHeaders(jwt),
    },
    regionWriteError,
  );
}
