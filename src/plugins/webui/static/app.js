// OpenTela Dashboard — app.js
// Complete client-side logic: API layer, rendering, helpers, auto-refresh.

// ============================================================================
// 1. API Layer
// ============================================================================

const api = {
  /** Fetch helper — returns parsed JSON or null on any failure. */
  async _fetch(url) {
    try {
      const res = await fetch(url);
      if (!res.ok) return null;
      return await res.json();
    } catch {
      return null;
    }
  },

  async health() {
    return this._fetch('/v1/health');
  },

  async nodeTable() {
    return this._fetch('/v1/dnt/table');
  },

  async peersStatus() {
    return this._fetch('/v1/dnt/peers_status');
  },

  async stats() {
    return this._fetch('/v1/dnt/stats');
  },

  async systemStats() {
    return this._fetch('/v1/system/stats');
  },

  async bootstraps() {
    return this._fetch('/v1/dnt/bootstraps');
  },
};

// ============================================================================
// 2. Helpers
// ============================================================================

/**
 * Truncate a peer ID for display.
 * If longer than 16 chars, show first 8 + "…" + last 4.
 */
function truncateId(id) {
  if (!id) return '\u2014';
  if (id.length > 16) return id.slice(0, 8) + '\u2026' + id.slice(-4);
  return id;
}

/**
 * Return a human-readable relative time string from an ISO timestamp.
 */
function timeAgo(isoString) {
  if (!isoString) return '\u2014';
  const then = new Date(isoString);
  if (isNaN(then.getTime())) return '\u2014';
  const diffMs = Date.now() - then.getTime();
  if (diffMs < 0) return 'just now';

  const seconds = Math.floor(diffMs / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

/**
 * Format a byte count into a human-readable string.
 */
function formatBytes(bytes) {
  if (bytes == null || isNaN(bytes)) return '\u2014';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

/**
 * Map a node status string to a badge colour class.
 */
function statusBadgeClass(status) {
  switch (status) {
    case 'active': return 'badge-green';
    case 'inactive':
    case 'offline': return 'badge-red';
    case 'degraded': return 'badge-yellow';
    default: return 'badge-gray';
  }
}

// ============================================================================
// 3. Rendering Functions
// ============================================================================

/**
 * Update the health-dot indicator in the header.
 */
function renderHealth(data) {
  const dot = document.getElementById('health-dot');
  dot.className = 'health-dot'; // reset
  if (data === null || data === undefined) {
    dot.classList.add('unknown');
  } else if (data.status === 'ok') {
    dot.classList.add('healthy');
  } else {
    dot.classList.add('unhealthy');
  }
}

/**
 * Update the five stat cards in the stats strip.
 */
function renderStats(stats, systemStats) {
  // Peer counts
  const connVal = document.querySelector('#stat-connected-peers .stat-value');
  const totalVal = document.querySelector('#stat-total-peers .stat-value');
  connVal.textContent = stats?.connected_peers ?? '\u2014';
  totalVal.textContent = stats?.total_peers_known ?? '\u2014';

  // CPU
  const cpuVal = document.querySelector('#stat-cpu .stat-value');
  if (systemStats?.cpu) {
    cpuVal.textContent = `${systemStats.cpu.num_cpu} / ${systemStats.cpu.num_goroutine}`;
  } else {
    cpuVal.textContent = '\u2014';
  }

  // Memory
  const memVal = document.querySelector('#stat-memory .stat-value');
  if (systemStats?.memory) {
    memVal.textContent = formatBytes(systemStats.memory.alloc_bytes);
  } else {
    memVal.textContent = '\u2014';
  }

  // GPU — hide the card entirely when no GPU data
  const gpuCard = document.getElementById('stat-gpu');
  const gpuVal = document.querySelector('#stat-gpu .stat-value');
  if (systemStats?.gpu?.length > 0) {
    gpuCard.classList.remove('hidden');
    const g = systemStats.gpu[0];
    const name = (g.name ?? '').replace(/^NVIDIA\s+/, '');
    gpuVal.textContent = `${name} ${g.temperature}\u00B0C`;
  } else {
    gpuCard.classList.add('hidden');
  }
}

/**
 * Build the node table rows (main row + expandable detail row per node).
 */
function renderNodeTable(nodes) {
  const tbody = document.getElementById('node-table-body');
  tbody.textContent = ''; // clear all children safely

  if (!nodes || nodes.length === 0) {
    const tr = document.createElement('tr');
    const td = document.createElement('td');
    td.colSpan = 6;
    td.className = 'text-center text-muted';
    td.textContent = 'No nodes found';
    tr.appendChild(td);
    tbody.appendChild(tr);
    return;
  }

  // Update header peer-id and version from the node list
  let versionSet = false;
  for (const node of nodes) {
    if (!versionSet && node.version) {
      document.getElementById('node-version').textContent = `v${node.version}`;
      versionSet = true;
    }
  }

  // Try to set peer-id from the first node (the local node is typically first)
  if (nodes.length > 0 && nodes[0].id) {
    document.getElementById('peer-id').textContent = truncateId(nodes[0].id);
    document.getElementById('peer-id').title = nodes[0].id;
  }

  nodes.forEach((node) => {
    // --- Main row ---
    const tr = document.createElement('tr');
    tr.style.cursor = 'pointer';

    // Peer ID cell
    const tdId = document.createElement('td');
    tdId.textContent = truncateId(node.id);
    tdId.title = node.id ?? '';
    tr.appendChild(tdId);

    // Services cell — badge per service
    const tdSvc = document.createElement('td');
    if (node.service?.length > 0) {
      node.service.forEach((svc) => {
        const span = document.createElement('span');
        span.className = 'badge badge-blue';
        span.textContent = svc.name;
        span.style.marginRight = '4px';
        tdSvc.appendChild(span);
      });
    } else {
      tdSvc.textContent = '\u2014';
    }
    tr.appendChild(tdSvc);

    // Status cell
    const tdStatus = document.createElement('td');
    const statusSpan = document.createElement('span');
    statusSpan.className = `badge ${statusBadgeClass(node.status)}`;
    statusSpan.textContent = node.status ?? '\u2014';
    tdStatus.appendChild(statusSpan);
    tr.appendChild(tdStatus);

    // Role cell
    const tdRole = document.createElement('td');
    tdRole.textContent = node.role ?? '\u2014';
    tr.appendChild(tdRole);

    // Latency cell
    const tdLat = document.createElement('td');
    tdLat.textContent = node.latency != null ? `${node.latency} ms` : '\u2014';
    tr.appendChild(tdLat);

    // Last Seen cell
    const tdSeen = document.createElement('td');
    tdSeen.textContent = timeAgo(node.last_seen);
    tdSeen.title = node.last_seen ?? '';
    tr.appendChild(tdSeen);

    tbody.appendChild(tr);

    // --- Expandable detail row (built with DOM methods, not innerHTML) ---
    const detailTr = document.createElement('tr');
    detailTr.className = 'expandable-row';
    const detailTd = document.createElement('td');
    detailTd.colSpan = 6;
    buildDetailDOM(detailTd, node);
    detailTr.appendChild(detailTd);
    tbody.appendChild(detailTr);

    // Toggle detail row on click
    tr.addEventListener('click', () => {
      detailTr.classList.toggle('open');
    });
  });
}

/**
 * Build the detail content for an expanded node row using safe DOM methods.
 */
function buildDetailDOM(container, node) {
  const wrapper = document.createElement('div');
  wrapper.style.cssText = 'padding:8px 12px;font-size:0.85rem;line-height:1.7';

  const addLine = (label, value) => {
    const b = document.createElement('strong');
    b.textContent = label + ': ';
    wrapper.appendChild(b);
    wrapper.appendChild(document.createTextNode(value || '\u2014'));
    wrapper.appendChild(document.createElement('br'));
  };

  addLine('Full ID', node.id);
  addLine('Owner', node.owner);
  addLine('Current Offering', node.current_offering || '\u2014');
  addLine('Available Offering', node.available_offering || '\u2014');

  if (node.service?.length > 0) {
    const svcLabel = document.createElement('strong');
    svcLabel.textContent = 'Services:';
    wrapper.appendChild(svcLabel);
    wrapper.appendChild(document.createElement('br'));

    node.service.forEach((svc) => {
      const line = document.createTextNode(
        `  \u2022 ${svc.name} (${svc.status ?? '\u2014'}) \u2014 ${svc.host}:${svc.port} \u2014 v${svc.version ?? '?'}`
      );
      wrapper.appendChild(line);
      wrapper.appendChild(document.createElement('br'));

      // Identity group tags
      const indent = document.createTextNode('    Identity: ');
      wrapper.appendChild(indent);
      if (svc.identity_group?.length > 0) {
        svc.identity_group.forEach((g) => {
          const badge = document.createElement('span');
          badge.className = 'badge badge-gray';
          badge.textContent = g;
          badge.style.marginRight = '4px';
          wrapper.appendChild(badge);
        });
      } else {
        wrapper.appendChild(document.createTextNode('\u2014'));
      }
      wrapper.appendChild(document.createElement('br'));
    });
  }

  container.appendChild(wrapper);
}

/**
 * Render the peer connectivity panel.
 */
function renderPeers(data) {
  const list = document.getElementById('peers-list');
  const empty = document.getElementById('peers-empty');
  list.textContent = ''; // clear safely

  if (data?.peers?.length > 0) {
    empty.classList.add('hidden');
    data.peers.forEach((peer) => {
      const li = document.createElement('li');
      li.className = 'flex items-center justify-between';
      const id = typeof peer === 'string' ? peer : (peer.id ?? peer.peer_id ?? JSON.stringify(peer));
      const span = document.createElement('span');
      span.title = id;
      span.textContent = truncateId(id);
      li.appendChild(span);
      // Status badge
      const status = typeof peer === 'object' ? (peer.status ?? peer.connectedness ?? 'connected') : 'connected';
      const badge = document.createElement('span');
      badge.className = `badge ${statusBadgeClass(status)}`;
      badge.textContent = status;
      li.appendChild(badge);
      list.appendChild(li);
    });
  } else {
    empty.classList.remove('hidden');
  }
}

/**
 * Render the bootstrap nodes panel.
 */
function renderBootstraps(data) {
  const list = document.getElementById('bootstraps-list');
  const empty = document.getElementById('bootstraps-empty');
  list.textContent = ''; // clear safely

  if (data?.bootstraps?.length > 0) {
    empty.classList.add('hidden');
    data.bootstraps.forEach((addr) => {
      const li = document.createElement('li');
      li.className = 'flex items-center justify-between';
      const span = document.createElement('span');
      span.textContent = addr;
      span.title = addr;
      li.appendChild(span);
      const dot = document.createElement('span');
      dot.className = 'health-dot healthy';
      li.appendChild(dot);
      list.appendChild(li);
    });
  } else {
    empty.classList.remove('hidden');
  }
}

/**
 * Show or hide the error banner.
 */
function renderError(show, message) {
  const banner = document.getElementById('error-banner');
  if (show) {
    banner.classList.remove('hidden');
    if (message) banner.textContent = message;
  } else {
    banner.classList.add('hidden');
  }
}

// ============================================================================
// 4. Refresh Controller + Initialization
// ============================================================================

const refreshController = {
  interval: 10000,
  enabled: true,
  timerId: null,

  /** Run a single refresh cycle — fetch all APIs, render results. */
  async refresh() {
    const spinner = document.getElementById('refresh-spinner');
    spinner.classList.add('active');

    const [health, nodeTable, peersStatus, stats, systemStats, bootstraps] =
      await Promise.allSettled([
        api.health(),
        api.nodeTable(),
        api.peersStatus(),
        api.stats(),
        api.systemStats(),
        api.bootstraps(),
      ]);

    // Extract values (fulfilled -> value, rejected -> null)
    const val = (r) => (r.status === 'fulfilled' ? r.value : null);

    const hData = val(health);
    const ntData = val(nodeTable);
    const psData = val(peersStatus);
    const stData = val(stats);
    const ssData = val(systemStats);
    const bsData = val(bootstraps);

    // Render each section
    renderHealth(hData);
    renderStats(stData, ssData);
    renderNodeTable(ntData);
    renderPeers(psData);
    renderBootstraps(bsData);

    // Show error banner only when ALL fetches returned null
    const allFailed = [hData, ntData, psData, stData, ssData, bsData].every((v) => v === null);
    renderError(allFailed, 'Connection lost \u2014 retrying...');

    // Update last-refresh timestamp
    const now = new Date();
    const hh = String(now.getHours()).padStart(2, '0');
    const mm = String(now.getMinutes()).padStart(2, '0');
    const ss = String(now.getSeconds()).padStart(2, '0');
    document.getElementById('last-refresh').textContent = `Last refresh: ${hh}:${mm}:${ss}`;

    spinner.classList.remove('active');
  },

  /** Start the auto-refresh interval. */
  start() {
    this.stop();
    if (this.enabled) {
      this.timerId = setInterval(() => this.refresh(), this.interval);
    }
  },

  /** Stop the auto-refresh interval. */
  stop() {
    if (this.timerId !== null) {
      clearInterval(this.timerId);
      this.timerId = null;
    }
  },

  /** Update the refresh interval (in ms) and restart if running. */
  setInterval(ms) {
    this.interval = ms;
    if (this.enabled) this.start();
  },

  /** Toggle auto-refresh on/off and update the button text. */
  toggle() {
    this.enabled = !this.enabled;
    document.getElementById('refresh-toggle').textContent =
      this.enabled ? 'Auto-refresh: On' : 'Auto-refresh: Off';
    if (this.enabled) {
      this.start();
    } else {
      this.stop();
    }
  },
};

// --- Initialization ---

document.addEventListener('DOMContentLoaded', () => {
  // Restore saved interval preference
  const saved = localStorage.getItem('otela-refresh-interval');
  if (saved) {
    const ms = parseInt(saved, 10) * 1000;
    if (!isNaN(ms) && ms > 0) {
      refreshController.interval = ms;
      const sel = document.getElementById('refresh-interval');
      for (const opt of sel.options) {
        if (parseInt(opt.value, 10) * 1000 === ms) {
          sel.value = opt.value;
          break;
        }
      }
    }
  }

  // Bind toggle button
  document.getElementById('refresh-toggle').addEventListener('click', () => {
    refreshController.toggle();
  });

  // Bind interval selector
  document.getElementById('refresh-interval').addEventListener('change', (e) => {
    const seconds = parseInt(e.target.value, 10);
    localStorage.setItem('otela-refresh-interval', String(seconds));
    refreshController.setInterval(seconds * 1000);
  });

  // First refresh immediately, then start auto-refresh
  refreshController.refresh();
  refreshController.start();
});
