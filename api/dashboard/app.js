(function () {
  'use strict';

  const API = '/api/v1';
  const $ = (s, el) => (el || document).querySelector(s);
  const $$ = (s, el) => [...(el || document).querySelectorAll(s)];

  // --- Navigation ---
  $$('.nav-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      $$('.nav-btn').forEach(b => b.classList.remove('active'));
      $$('.view').forEach(v => v.classList.remove('active'));
      btn.classList.add('active');
      $(`#view-${btn.dataset.view}`).classList.add('active');
      loadView(btn.dataset.view);
    });
  });

  // --- Data cache ---
  let cache = { providers: null, users: {}, inactive: null, events: null, mappings: null };

  async function api(path) {
    const res = await fetch(API + path);
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
  }

  // --- Views ---
  function loadView(name) {
    switch (name) {
      case 'overview': loadOverview(); break;
      case 'users': loadUsers(); break;
      case 'inactive': loadInactive(); break;
      case 'events': loadEvents(); break;
    }
  }

  // --- Overview ---
  async function loadOverview() {
    const [providers, mappings] = await Promise.all([
      cache.providers || api('/providers'),
      cache.mappings || api('/mappings'),
    ]);
    cache.providers = providers;
    cache.mappings = mappings;

    const container = $('#provider-cards');
    if (!providers || providers.length === 0) {
      container.innerHTML = emptyState('No providers synced', 'Run unseat sync run to populate data.');
      return;
    }

    container.innerHTML = providers.map(p => `
      <div class="card">
        <div class="card-header">
          <span class="card-name">${esc(p.provider)}</span>
          <span class="badge ${p.status === 'ok' ? 'badge-ok' : 'badge-error'}">${esc(p.status)}</span>
        </div>
        <div class="card-metric">${p.user_count}</div>
        <div class="card-label">users</div>
        <div class="card-footer">Last sync: ${timeAgo(p.last_synced_at)}</div>
      </div>
    `).join('');

    const mContainer = $('#mappings-list');
    if (!mappings || mappings.length === 0) {
      mContainer.innerHTML = emptyState('No mappings configured', 'Define group-to-provider mappings in unseat.yaml.');
      return;
    }
    mContainer.innerHTML = mappings.map(m => `
      <div class="mapping-card">
        <div class="mapping-group">${esc(m.group)}</div>
        <div class="mapping-providers">
          ${(m.providers || []).map(p =>
            `<span class="mapping-tag">${esc(p.name)} <span style="opacity:.6">${esc(p.role)}</span></span>`
          ).join('')}
        </div>
      </div>
    `).join('');
  }

  // --- Users ---
  async function loadUsers() {
    if (!cache.providers) cache.providers = await api('/providers');
    const providers = cache.providers || [];

    const select = $('#users-provider-filter');
    if (select.options.length <= 1) {
      providers.forEach(p => {
        const opt = document.createElement('option');
        opt.value = p.provider;
        opt.textContent = p.provider;
        select.appendChild(opt);
      });
    }

    await renderUsers();

    select.onchange = renderUsers;
    $('#users-search').oninput = debounce(renderUsers, 200);
  }

  async function renderUsers() {
    const wrap = $('#users-table-wrap');
    const provider = $('#users-provider-filter').value;
    const search = ($('#users-search').value || '').toLowerCase();

    const providerNames = provider
      ? [provider]
      : (cache.providers || []).map(p => p.provider);

    wrap.innerHTML = '<div class="loading">Loading...</div>';

    let allUsers = [];
    for (const name of providerNames) {
      if (!cache.users[name]) {
        cache.users[name] = await api(`/providers/${name}/users`);
      }
      const users = cache.users[name] || [];
      allUsers.push(...users.map(u => ({ ...u, _provider: name })));
    }

    if (search) {
      allUsers = allUsers.filter(u =>
        u.email.toLowerCase().includes(search) ||
        (u.display_name || '').toLowerCase().includes(search)
      );
    }

    if (allUsers.length === 0) {
      wrap.innerHTML = emptyState('No users found', provider ? `No users in ${provider}.` : 'Run unseat sync run first.');
      return;
    }

    wrap.innerHTML = renderTable(
      ['Provider', 'Email', 'Name', 'Role', 'Status', 'Last Active'],
      allUsers.map(u => [
        u._provider,
        { value: u.email, cls: 'mono' },
        u.display_name || '-',
        u.role || '-',
        { value: u.status, badge: statusBadge(u.status) },
        u.last_activity_at ? formatDate(u.last_activity_at) : '-',
      ])
    );
  }

  // --- Inactive ---
  async function loadInactive() {
    await renderInactive();
    $('#inactive-refresh').onclick = () => { cache.inactive = null; renderInactive(); };
  }

  async function renderInactive() {
    const wrap = $('#inactive-table-wrap');
    const days = parseInt($('#inactive-days').value) || 30;

    wrap.innerHTML = '<div class="loading">Loading...</div>';
    cache.inactive = await api(`/inactive?days=${days}`);
    const users = cache.inactive || [];

    if (users.length === 0) {
      wrap.innerHTML = emptyState('No inactive users', `Everyone has been active in the last ${days} days.`);
      return;
    }

    wrap.innerHTML = renderTable(
      ['Provider', 'Email', 'Name', 'Last Active', 'Status'],
      users.map(u => [
        u.provider,
        { value: u.email, cls: 'mono' },
        u.display_name || '-',
        u.last_activity_at ? formatDate(u.last_activity_at) : 'Never',
        { value: u.status, badge: statusBadge(u.status) },
      ])
    );
  }

  // --- Events ---
  async function loadEvents() {
    await renderEvents();
    $('#events-refresh').onclick = () => { cache.events = null; renderEvents(); };
  }

  async function renderEvents() {
    const wrap = $('#events-table-wrap');
    const limit = parseInt($('#events-limit').value) || 50;

    wrap.innerHTML = '<div class="loading">Loading...</div>';
    cache.events = await api(`/history/events?limit=${limit}`);
    const events = cache.events || [];

    if (events.length === 0) {
      wrap.innerHTML = emptyState('No events yet', 'Events appear after running unseat sync.');
      return;
    }

    wrap.innerHTML = renderTable(
      ['Time', 'Type', 'Provider', 'Email', 'Details', 'Trigger'],
      events.map(e => [
        formatDateTime(e.occurred_at),
        { value: eventLabel(e.type), cls: eventCls(e.type) },
        e.provider,
        { value: e.email || '-', cls: 'mono' },
        e.details || '-',
        e.trigger || '-',
      ])
    );
  }

  // --- Helpers ---
  function renderTable(headers, rows) {
    return `<table>
      <thead><tr>${headers.map(h => `<th>${esc(h)}</th>`).join('')}</tr></thead>
      <tbody>${rows.map(row => `<tr>${row.map(cell => {
        if (typeof cell === 'object' && cell !== null) {
          if (cell.badge) return `<td>${cell.badge}</td>`;
          return `<td${cell.cls ? ` class="${cell.cls}"` : ''}>${esc(cell.value)}</td>`;
        }
        return `<td>${esc(String(cell))}</td>`;
      }).join('')}</tr>`).join('')}</tbody>
    </table>`;
  }

  function emptyState(text, hint) {
    return `<div class="empty">
      <div class="empty-text">${esc(text)}</div>
      ${hint ? `<div class="empty-hint">${esc(hint)}</div>` : ''}
    </div>`;
  }

  function statusBadge(status) {
    const cls = status === 'active' ? 'badge-ok'
      : status === 'suspended' ? 'badge-error'
      : 'badge-neutral';
    return `<span class="badge ${cls}">${esc(status)}</span>`;
  }

  function eventLabel(type) {
    return (type || '').replace(/_/g, ' ');
  }

  function eventCls(type) {
    if (type === 'user_added') return 'event-added';
    if (type === 'user_removed') return 'event-removed';
    if (type === 'user_suspended') return 'event-suspended';
    return 'event-sync';
  }

  function esc(str) {
    const d = document.createElement('div');
    d.textContent = str || '';
    return d.innerHTML;
  }

  function formatDate(iso) {
    if (!iso) return '-';
    const d = new Date(iso);
    return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' });
  }

  function formatDateTime(iso) {
    if (!iso) return '-';
    const d = new Date(iso);
    return d.toLocaleString('en-US', {
      month: 'short', day: 'numeric',
      hour: '2-digit', minute: '2-digit',
    });
  }

  function timeAgo(iso) {
    if (!iso) return 'never';
    const diff = Date.now() - new Date(iso).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 1) return 'just now';
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h ago`;
    const days = Math.floor(hrs / 24);
    return `${days}d ago`;
  }

  function debounce(fn, ms) {
    let t;
    return (...args) => { clearTimeout(t); t = setTimeout(() => fn(...args), ms); };
  }

  // Initial load
  loadOverview();
})();
