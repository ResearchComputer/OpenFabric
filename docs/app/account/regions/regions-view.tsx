"use client";

import {
  useEffect,
  useMemo,
  useState,
  type CSSProperties,
  type FormEvent,
} from "react";
import { Loader2, Plus, RefreshCw, ShieldOff, Trash2 } from "lucide-react";
import { useAccount } from "../account-context";
import { tokenManagerConfig } from "../config";
import { formatDate, middleEllipsis } from "../format";
import { getAuthJwt } from "../neon-auth";
import {
  cancelManageRegionInvitation,
  createManageRegion,
  createManageRegionInvitation,
  deleteManageRegionMember,
  listManageRegions,
  updateManageRegion,
  updateManageRegionMember,
  type ManageMembershipStatus,
  type ManageNodeRole,
  type ManageRegion,
  type ManageRegionInvitation,
  type ManageRegionMember,
  type ManageRegionStatus,
} from "../manage-api";

const selectStyle: CSSProperties = {
  minHeight: 36,
  width: "100%",
  border: "1px solid var(--ac-line)",
  borderRadius: 8,
  padding: "0 11px",
  background: "var(--ac-panel)",
  color: "var(--ac-ink)",
  fontSize: "0.875rem",
};

function regionStatusTone(status: ManageRegionStatus): "active" | "revoked" {
  return status === "active" ? "active" : "revoked";
}

function memberStatusTone(
  status: ManageRegionMember["status"],
): "active" | "revoked" {
  return status === "active" ? "active" : "revoked";
}

type EditableMemberStatus = Exclude<ManageMembershipStatus, "pending">;

function totalMembers(regions: ManageRegion[]): number {
  return regions.reduce((sum, region) => sum + region.members.length, 0);
}

function totalInvites(regions: ManageRegion[]): number {
  return regions.reduce(
    (sum, region) =>
      sum +
      region.invitations.filter((invitation) => invitation.status === "pending")
        .length,
    0,
  );
}

function RegionCard({
  region,
  reload,
}: {
  region: ManageRegion;
  reload: (announce: boolean) => Promise<void>;
}) {
  const {
    managedInstances,
    showNotice,
    handleApiError,
    pending,
    setPending,
    releasePending,
  } = useAccount();
  const [inviteInstanceId, setInviteInstanceId] = useState("");
  const [inviteRole, setInviteRole] = useState<ManageNodeRole>("worker");
  const [inviteExpiry, setInviteExpiry] = useState("");

  const availableInstances = useMemo(
    () =>
      managedInstances.filter(
        (instance) =>
          !region.members.some(
            (member) => member.instance_id === instance.id,
          ) &&
          !region.invitations.some(
            (invitation) =>
              invitation.instance_id === instance.id &&
              invitation.status === "pending",
          ),
      ),
    [managedInstances, region.invitations, region.members],
  );

  useEffect(() => {
    if (!inviteInstanceId && availableInstances.length > 0) {
      setInviteInstanceId(availableInstances[0].id);
    }
  }, [availableInstances, inviteInstanceId]);

  async function toggleRegionStatus(nextStatus: ManageRegionStatus) {
    setPending(`region-status-${region.id}`);
    try {
      const jwt = await getAuthJwt();
      await updateManageRegion(tokenManagerConfig.apiBaseUrl, jwt, region.id, {
        status: nextStatus,
      });
      await reload(true);
      showNotice(
        "success",
        nextStatus === "active"
          ? `Trusted region ${region.slug} re-enabled`
          : `Trusted region ${region.slug} disabled`,
      );
    } catch (error) {
      handleApiError(error);
    } finally {
      releasePending(`region-status-${region.id}`);
    }
  }

  async function handleInvite(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!inviteInstanceId) return;
    setPending(`region-invite-${region.id}`);
    try {
      const jwt = await getAuthJwt();
      await createManageRegionInvitation(
        tokenManagerConfig.apiBaseUrl,
        jwt,
        region.id,
        {
          instance_id: inviteInstanceId,
          node_role: inviteRole,
          expires_at: inviteExpiry || null,
        },
      );
      await reload(true);
      showNotice("success", `Invitation queued for ${region.slug}`);
    } catch (error) {
      handleApiError(error);
    } finally {
      releasePending(`region-invite-${region.id}`);
    }
  }

  async function cancelInvite(invitation: ManageRegionInvitation) {
    setPending(`region-invite-cancel-${invitation.id}`);
    try {
      const jwt = await getAuthJwt();
      await cancelManageRegionInvitation(
        tokenManagerConfig.apiBaseUrl,
        jwt,
        region.id,
        invitation.instance_id,
      );
      await reload(false);
      showNotice("success", "Invitation cancelled");
    } catch (error) {
      handleApiError(error);
    } finally {
      releasePending(`region-invite-cancel-${invitation.id}`);
    }
  }

  async function updateMember(
    member: ManageRegionMember,
    input: { node_role?: ManageNodeRole; status?: EditableMemberStatus },
  ) {
    setPending(`region-member-${region.id}-${member.instance_id}`);
    try {
      const jwt = await getAuthJwt();
      await updateManageRegionMember(
        tokenManagerConfig.apiBaseUrl,
        jwt,
        region.id,
        member.instance_id,
        input,
      );
      await reload(false);
      showNotice("success", "Member policy saved");
    } catch (error) {
      handleApiError(error);
    } finally {
      releasePending(`region-member-${region.id}-${member.instance_id}`);
    }
  }

  async function releaseMember(member: ManageRegionMember) {
    if (member.trusted_service_count && member.trusted_service_count > 0)
      return;
    if (
      !window.confirm(
        `Release ${member.peer_id ?? member.instance_id} from ${region.slug}? Trusted services must already be disabled or reclassified.`,
      )
    ) {
      return;
    }

    setPending(`region-release-${region.id}-${member.instance_id}`);
    try {
      const jwt = await getAuthJwt();
      await deleteManageRegionMember(
        tokenManagerConfig.apiBaseUrl,
        jwt,
        region.id,
        member.instance_id,
      );
      await reload(false);
      showNotice("success", "Member released");
    } catch (error) {
      handleApiError(error);
    } finally {
      releasePending(`region-release-${region.id}-${member.instance_id}`);
    }
  }

  return (
    <section className="otm-panel">
      <div className="otm-panel-heading">
        <div>
          <p className="otm-eyebrow">Trusted region</p>
          <h2>{region.name}</h2>
          <p className="acct-page-sub" style={{ marginTop: 6 }}>
            <code>{region.slug}</code>
          </p>
        </div>
        <div className="acct-page-actions">
          <span
            className={`otm-status-pill ${regionStatusTone(region.status)}`}
          >
            {region.status === "active" ? "Active" : "Disabled"}
          </span>
          <button
            type="button"
            className="otm-secondary-button"
            onClick={() => {
              toggleRegionStatus(
                region.status === "active" ? "disabled" : "active",
              ).catch(() => undefined);
            }}
            disabled={pending === `region-status-${region.id}`}
          >
            {pending === `region-status-${region.id}` ? (
              <Loader2 className="otm-spin" size={16} />
            ) : region.status === "active" ? (
              <ShieldOff size={16} />
            ) : (
              <RefreshCw size={16} />
            )}
            {region.status === "active" ? "Disable region" : "Re-enable region"}
          </button>
        </div>
      </div>

      <div style={{ padding: 16, display: "grid", gap: 16 }}>
        <div className="otm-status-grid" style={{ marginBottom: 0 }}>
          <div className="otm-metric-panel">
            <span>Members</span>
            <strong>{region.members.length}</strong>
            <small>Revision {region.region_revision ?? "unknown"}</small>
          </div>
          <div className="otm-metric-panel">
            <span>Pending invites</span>
            <strong>
              {
                region.invitations.filter(
                  (invitation) => invitation.status === "pending",
                ).length
              }
            </strong>
            <small>Invitations never change traffic by themselves</small>
          </div>
          <div className="otm-metric-panel">
            <span>Release safety</span>
            <strong>Fail closed</strong>
            <small>
              Membership release stays blocked while trusted services remain
              bound
            </small>
          </div>
        </div>

        <div className="otm-notice" role="note" style={{ margin: 0 }}>
          Disabling a region removes trusted ingress and trusted provider
          eligibility for that region, but it does not publish private services
          into the public catalogue. Reclassify or disable each trusted binding
          explicitly before releasing membership or moving an instance back to
          peer-wide policy.
        </div>

        <section style={{ display: "grid", gap: 12 }}>
          <div className="otm-panel-heading">
            <h3>Invite claimed instances</h3>
          </div>
          <form className="otm-create-key-form" onSubmit={handleInvite}>
            <label>
              Instance
              <select
                value={inviteInstanceId}
                onChange={(event) => setInviteInstanceId(event.target.value)}
                style={selectStyle}
                disabled={availableInstances.length === 0}
              >
                {availableInstances.length === 0 ? (
                  <option value="">No eligible claimed instances</option>
                ) : (
                  availableInstances.map((instance) => (
                    <option key={instance.id} value={instance.id}>
                      {(instance.label?.trim() ||
                        middleEllipsis(instance.peer_id, 8, 8)) +
                        ` (${instance.policy_scope})`}
                    </option>
                  ))
                )}
              </select>
            </label>
            <label>
              Node role
              <select
                value={inviteRole}
                onChange={(event) =>
                  setInviteRole(event.target.value as ManageNodeRole)
                }
                style={selectStyle}
              >
                <option value="worker">Worker</option>
                <option value="head">Head</option>
                <option value="combined">Combined</option>
              </select>
            </label>
            <label>
              Invitation expiry
              <input
                type="datetime-local"
                value={inviteExpiry}
                onChange={(event) => setInviteExpiry(event.target.value)}
              />
            </label>
            <button
              type="submit"
              className="otm-primary-button"
              disabled={
                pending === `region-invite-${region.id}` || !inviteInstanceId
              }
            >
              {pending === `region-invite-${region.id}` ? (
                <Loader2 className="otm-spin" size={16} />
              ) : (
                <Plus size={16} />
              )}
              Invite instance
            </button>
          </form>
        </section>

        <section style={{ display: "grid", gap: 12 }}>
          <div className="otm-panel-heading">
            <h3>Members</h3>
          </div>
          {region.members.length === 0 ? (
            <div className="otm-empty-state">
              No accepted members yet. Invitations only become a trusted
              capability after the control plane marks the membership active.
            </div>
          ) : (
            <div className="otm-table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Peer</th>
                    <th>Role</th>
                    <th>Status</th>
                    <th>Trusted services</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {region.members.map((member) => (
                    <tr key={member.instance_id}>
                      <td>
                        <div style={{ display: "grid", gap: 4 }}>
                          <strong>
                            {member.label?.trim() ||
                              middleEllipsis(
                                member.peer_id ?? member.instance_id,
                                10,
                                10,
                              )}
                          </strong>
                          <small>{member.peer_id ?? member.instance_id}</small>
                        </div>
                      </td>
                      <td>
                        <select
                          defaultValue={member.node_role}
                          style={selectStyle}
                          onChange={(event) => {
                            updateMember(member, {
                              node_role: event.target.value as ManageNodeRole,
                            }).catch(() => undefined);
                          }}
                          disabled={
                            pending ===
                            `region-member-${region.id}-${member.instance_id}`
                          }
                        >
                          <option value="worker">Worker</option>
                          <option value="head">Head</option>
                          <option value="combined">Combined</option>
                        </select>
                      </td>
                      <td>
                        <div style={{ display: "grid", gap: 8 }}>
                          <span
                            className={`otm-status-pill ${memberStatusTone(member.status)}`}
                          >
                            {member.status}
                          </span>
                          <select
                            defaultValue={member.status}
                            style={selectStyle}
                            onChange={(event) => {
                              updateMember(member, {
                                status: event.target
                                  .value as EditableMemberStatus,
                              }).catch(() => undefined);
                            }}
                            disabled={
                              pending ===
                              `region-member-${region.id}-${member.instance_id}`
                            }
                          >
                            <option value="active">active</option>
                            <option value="suspended">suspended</option>
                            <option value="revoked">revoked</option>
                            {!["active", "suspended", "revoked"].includes(
                              member.status,
                            ) ? (
                              <option value={member.status} disabled>
                                {member.status}
                              </option>
                            ) : null}
                          </select>
                        </div>
                      </td>
                      <td>
                        <div style={{ display: "grid", gap: 4 }}>
                          <strong>{member.trusted_service_count ?? 0}</strong>
                          <small>
                            {member.ownership_verified_at
                              ? `Ownership ${formatDate(member.ownership_verified_at)}`
                              : "Ownership freshness not reported"}
                          </small>
                        </div>
                      </td>
                      <td>
                        <button
                          type="button"
                          className="otm-icon-button danger"
                          title={
                            member.trusted_service_count &&
                            member.trusted_service_count > 0
                              ? "Disable or reclassify trusted services before release"
                              : "Release member"
                          }
                          onClick={() => {
                            releaseMember(member).catch(() => undefined);
                          }}
                          disabled={
                            pending ===
                              `region-release-${region.id}-${member.instance_id}` ||
                            (member.trusted_service_count ?? 0) > 0
                          }
                        >
                          {pending ===
                          `region-release-${region.id}-${member.instance_id}` ? (
                            <Loader2 className="otm-spin" size={18} />
                          ) : (
                            <Trash2 size={18} />
                          )}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>

        <section style={{ display: "grid", gap: 12 }}>
          <div className="otm-panel-heading">
            <h3>Invitations</h3>
          </div>
          {region.invitations.length === 0 ? (
            <div className="otm-empty-state">
              No invitations for this region yet.
            </div>
          ) : (
            <div className="otm-table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Instance</th>
                    <th>Role</th>
                    <th>Status</th>
                    <th>Expiry</th>
                    <th aria-label="Actions" />
                  </tr>
                </thead>
                <tbody>
                  {region.invitations.map((invitation) => (
                    <tr key={invitation.id}>
                      <td>
                        <div style={{ display: "grid", gap: 4 }}>
                          <strong>
                            {invitation.label?.trim() ||
                              middleEllipsis(
                                invitation.peer_id ?? invitation.instance_id,
                                10,
                                10,
                              )}
                          </strong>
                          <small>
                            {invitation.peer_id ?? invitation.instance_id}
                          </small>
                        </div>
                      </td>
                      <td>{invitation.node_role}</td>
                      <td>
                        <span
                          className={`otm-status-pill ${
                            invitation.status === "pending"
                              ? "active"
                              : "revoked"
                          }`}
                        >
                          {invitation.status}
                        </span>
                      </td>
                      <td>
                        {invitation.expires_at
                          ? formatDate(invitation.expires_at)
                          : "No expiry"}
                      </td>
                      <td>
                        <button
                          type="button"
                          className="otm-icon-button danger"
                          title="Cancel invitation"
                          onClick={() => {
                            cancelInvite(invitation).catch(() => undefined);
                          }}
                          disabled={
                            invitation.status !== "pending" ||
                            pending === `region-invite-cancel-${invitation.id}`
                          }
                        >
                          {pending ===
                          `region-invite-cancel-${invitation.id}` ? (
                            <Loader2 className="otm-spin" size={18} />
                          ) : (
                            <Trash2 size={18} />
                          )}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>
    </section>
  );
}

export default function RegionsView() {
  const {
    neonUser,
    showNotice,
    handleApiError,
    pending,
    setPending,
    releasePending,
  } = useAccount();
  const [regions, setRegions] = useState<ManageRegion[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [regionsError, setRegionsError] = useState<string | null>(null);
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");

  async function loadRegions(announce: boolean) {
    if (!neonUser) return;
    setPending("regions-refresh");
    setRegionsError(null);
    try {
      const jwt = await getAuthJwt();
      const snapshot = await listManageRegions(
        tokenManagerConfig.apiBaseUrl,
        jwt,
      );
      setRegions(snapshot.regions);
      setLoaded(true);
      if (announce) showNotice("success", "Trusted regions refreshed");
    } catch (error) {
      setLoaded(true);
      setRegionsError(error instanceof Error ? error.message : String(error));
      handleApiError(error);
    } finally {
      releasePending("regions-refresh");
    }
  }

  useEffect(() => {
    if (!neonUser) return;
    loadRegions(false).catch(() => undefined);
  }, [neonUser]);

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending("region-create");
    try {
      const jwt = await getAuthJwt();
      await createManageRegion(tokenManagerConfig.apiBaseUrl, jwt, {
        slug: slug.trim(),
        name: name.trim(),
      });
      setSlug("");
      setName("");
      await loadRegions(false);
      showNotice("success", "Trusted region created");
    } catch (error) {
      handleApiError(error);
    } finally {
      releasePending("region-create");
    }
  }

  return (
    <div className="acct-page">
      <header className="acct-page-head">
        <div>
          <h1>Trusted regions</h1>
          <p className="acct-page-sub">
            Define the trusted partition, invite claimed peers into it, and keep
            membership lifecycle separate from the public mesh. Public service
            names are not authenticated here; only the trusted route is.
          </p>
        </div>
        <div className="acct-page-actions">
          <button
            type="button"
            className="otm-secondary-button"
            onClick={() => {
              loadRegions(true).catch(() => undefined);
            }}
            disabled={pending === "regions-refresh"}
          >
            {pending === "regions-refresh" ? (
              <Loader2 className="otm-spin" size={16} />
            ) : (
              <RefreshCw size={16} />
            )}
            Refresh
          </button>
        </div>
      </header>

      <section className="otm-status-grid" aria-label="Trusted-region summary">
        <div className="otm-metric-panel">
          <span>Regions</span>
          <strong>{regions.length}</strong>
          <small>
            {regions.filter((region) => region.status === "active").length}{" "}
            active
          </small>
        </div>
        <div className="otm-metric-panel">
          <span>Members</span>
          <strong>{totalMembers(regions)}</strong>
          <small>Membership alone does not publish any service</small>
        </div>
        <div className="otm-metric-panel">
          <span>Pending invites</span>
          <strong>{totalInvites(regions)}</strong>
          <small>
            Invitation rows are advisory until accepted and activated
          </small>
        </div>
      </section>

      <div className="otm-notice" role="note">
        Trusted traffic never falls back to the permissionless partition, uses
        direct-only worker hops, skips the trusted cache, and is revalidated at
        the worker against the immediate upstream peer. Keep backend services on
        loopback or behind a firewall so direct backend access cannot bypass
        these checks.
      </div>

      <section className="otm-panel">
        <div className="otm-panel-heading">
          <h2>Create a trusted region</h2>
        </div>
        <form className="otm-create-key-form" onSubmit={handleCreate}>
          <label>
            Region slug
            <input
              value={slug}
              onChange={(event) => setSlug(event.target.value)}
              placeholder="swissai-prod"
              required
            />
          </label>
          <label>
            Name
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="SwissAI production"
              required
            />
          </label>
          <button
            type="submit"
            className="otm-primary-button"
            disabled={pending === "region-create"}
          >
            {pending === "region-create" ? (
              <Loader2 className="otm-spin" size={16} />
            ) : (
              <Plus size={16} />
            )}
            Create region
          </button>
        </form>
      </section>

      <section style={{ display: "grid", gap: 16, marginTop: 16 }}>
        {!loaded && !regionsError ? (
          <div className="otm-empty-state">Loading trusted regions…</div>
        ) : regionsError ? (
          <div className="otm-notice" role="alert">
            {regionsError}
          </div>
        ) : regions.length === 0 ? (
          <div className="otm-empty-state">
            No trusted regions yet. Create one before binding any service into
            the trusted partition.
          </div>
        ) : (
          regions.map((region) => (
            <RegionCard key={region.id} region={region} reload={loadRegions} />
          ))
        )}
      </section>
    </div>
  );
}
