// -- State -------------------------------------------------------------------
let allPlugins = [];
let allActions = [];
let workflowSteps = [];

// -- Navigation --------------------------------------------------------------
document.querySelectorAll('.nav-item').forEach(item => {
  item.addEventListener('click', () => {
    document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
    document.querySelectorAll('.page').forEach(p => p.classList.remove('active'));
    item.classList.add('active');
    document.getElementById('page-' + item.dataset.page).classList.add('active');
  });
});

// -- Helpers -----------------------------------------------------------------
async function api(path, opts) {
  const r = await fetch(path, opts);
  if (!r.ok) throw new Error(`${path}: ${r.status}`);
  const ct = r.headers.get('content-type') || '';
  if (ct.includes('json')) return r.json();
  return r.text();
}

function esc(s) { const d = document.createElement('div'); d.textContent = s; return d.innerHTML; }

function tags(arr, cls = '') {
  if (!arr || !arr.length) return '<span style="color:#222">—</span>';
  return arr.map(r => `<span class="tag ${cls}">${esc(r)}</span>`).join('');
}

function showError(msg) { const el = document.getElementById('error-banner'); el.textContent = msg; el.style.display = 'block'; }
function clearError() { document.getElementById('error-banner').style.display = 'none'; }

// -- Render: Monitor ---------------------------------------------------------
function renderOverview(overview, routers) {
  const s = overview.summary;
  document.getElementById('s-svc').textContent = s.services;
  document.getElementById('s-ident').textContent = s.identities;
  document.getElementById('s-rtr').textContent = `${overview.routersOnline}/${overview.routersTotal}`;
  document.getElementById('s-rtr-sub').textContent = `${overview.routersOnline} online`;
  document.getElementById('s-term').textContent = s.terminators;
  document.getElementById('s-sess').textContent = s.apiSessions;
  document.getElementById('nav-services').textContent = s.services;

  const tbody = document.querySelector('#overview-routers tbody');
  tbody.innerHTML = routers.map(r => {
    const status = r.isOnline
      ? '<span class="dot dot-green"></span>online'
      : '<span class="dot dot-red"></span>offline';
    const region = (r.roleAttributes || []).find(a => a.startsWith('region-')) || '—';
    return `<tr><td>${esc(r.name)}</td><td>${status}</td><td><span class="tag">${esc(region)}</span></td></tr>`;
  }).join('');
}

function renderServices(data) {
  document.getElementById('tb-services').innerHTML = data.sort((a,b) => a.name.localeCompare(b.name)).map(s =>
    `<tr><td>${esc(s.name)}</td><td>${tags(s.roleAttributes)}</td><td class="id-cell">${esc(s.id)}</td><td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/services','${s.id}', loadAll)">del</button></td></tr>`
  ).join('');
}

function renderIdentities(data) {
  const online = data.filter(i => i.hasEdgeRouterConnection).length;
  document.getElementById('s-ident-sub').textContent = `${online} online`;
}

function renderRouters(data) {
  document.getElementById('tb-routers').innerHTML = data.map(r =>
    `<tr><td>${esc(r.name)}</td><td>${r.isOnline ? '<span class="dot dot-green"></span>online' : '<span class="dot dot-red"></span>offline'}</td>
    <td>${tags(r.roleAttributes)}</td><td class="id-cell">${esc(r.id)}</td><td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/routers','${r.id}', loadAll)">del</button></td></tr>`
  ).join('');
}

function renderPolicies(data) {
  document.getElementById('tb-policies').innerHTML = data.sort((a,b) => a.name.localeCompare(b.name)).map(p =>
    `<tr><td>${esc(p.name)}</td><td><span class="tag ${p.type === 'Dial' ? 'tag-blue' : ''}">${esc(p.type)}</span></td>
    <td>${tags(p.identityRoles, 'tag-blue')}</td><td>${tags(p.serviceRoles)}</td><td class="id-cell">${esc(p.id)}</td><td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/policies','${p.id}', loadAll)">del</button></td></tr>`
  ).join('');
}

function renderTerminators(data) {
  document.getElementById('tb-terminators').innerHTML = data.map(t =>
    `<tr><td>${esc(t.service?.name || '—')}</td><td>${esc(t.router?.name || '—')}</td>
    <td><span class="tag">${esc(t.binding || '—')}</span></td><td style="font-size:0.6875rem">${esc(t.address || '—')}</td>
    <td class="id-cell">${esc(t.id)}</td><td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/terminators','${t.id}', loadAll)">del</button></td></tr>`
  ).join('');
}

// -- Render: Plugins ---------------------------------------------------------
function renderPlugins(plugins) {
  allPlugins = plugins;
  const totalActions = plugins.reduce((sum, p) => sum + (p.actions?.length || 0), 0);
  document.getElementById('sp-count').textContent = plugins.length;
  document.getElementById('sp-actions').textContent = totalActions;
  document.getElementById('s-plugins').textContent = plugins.length;
  document.getElementById('s-actions-sub').textContent = `${totalActions} actions`;
  document.getElementById('nav-plugins').textContent = plugins.length;

  document.getElementById('plugin-grid').innerHTML = plugins
    .sort((a,b) => a.name.localeCompare(b.name))
    .map(p => {
      const actions = p.actions || [];
      const cats = [...new Set(actions.map(a => a.category).filter(Boolean))];
      return `
        <div class="plugin-card" onclick="this.classList.toggle('expanded')">
          <div class="p-header">
            <span class="p-name">${esc(p.name)}</span>
            <span class="p-version">v${esc(p.version)}</span>
          </div>
          <div class="p-desc">${esc(p.description)}</div>
          <div class="p-actions">${actions.length} actions${cats.length ? ` in ${cats.length} categories` : ''}</div>
          <div class="p-actions-list">
            ${actions.map(a => `
              <div class="action-item" onclick="event.stopPropagation(); addStep('${esc(p.name)}', '${esc(a.name)}')">
                <span class="a-name">${esc(a.name)}</span>
                ${a.category ? `<span class="a-cat">${esc(a.category)}</span>` : ''}
              </div>
            `).join('')}
          </div>
        </div>`;
    }).join('');
}

// -- Render: Actions list (for workflow builder) -----------------------------
function renderActionList(plugins) {
  allActions = [];
  plugins.forEach(p => {
    (p.actions || []).forEach(a => {
      allActions.push({ plugin: p.name, action: a.name, category: a.category || '', description: a.description || '' });
    });
  });
  allActions.sort((a,b) => `${a.plugin}.${a.action}`.localeCompare(`${b.plugin}.${b.action}`));
  filterActions('');

  document.getElementById('action-search').addEventListener('input', e => filterActions(e.target.value));
}

function filterActions(query) {
  const q = query.toLowerCase();
  const filtered = q ? allActions.filter(a =>
    a.plugin.includes(q) || a.action.includes(q) || a.category.includes(q)
  ) : allActions;

  document.getElementById('action-list').innerHTML = filtered.map(a =>
    `<div class="action-item" style="cursor:pointer;padding:0.25rem 0.25rem" onclick="addStep('${esc(a.plugin)}','${esc(a.action)}')">
      <span><span style="color:var(--accent-dim)">${esc(a.plugin)}</span><span style="color:#333">.</span><span class="a-name">${esc(a.action)}</span></span>
      ${a.category ? `<span class="a-cat">${esc(a.category)}</span>` : ''}
    </div>`
  ).join('');
}

// -- Workflow builder --------------------------------------------------------
function addStep(plugin, action) {
  const id = `step-${workflowSteps.length + 1}`;
  workflowSteps.push({ id, plugin, action, creds_path: '', inputs: {} });
  renderWorkflow();
}

function removeStep(idx) {
  workflowSteps.splice(idx, 1);
  workflowSteps.forEach((s, i) => s.id = `step-${i + 1}`);
  renderWorkflow();
}

function clearWorkflow() {
  workflowSteps = [];
  renderWorkflow();
  document.getElementById('wf-output').style.display = 'none';
}

function renderWorkflow() {
  const el = document.getElementById('wf-steps');
  const empty = document.getElementById('wf-empty');
  if (!workflowSteps.length) {
    el.innerHTML = '';
    empty.style.display = '';
    return;
  }
  empty.style.display = 'none';
  el.innerHTML = workflowSteps.map((s, i) =>
    `<div class="wf-step">
      <span class="wf-num">${i + 1}</span>
      <span class="wf-plugin">${esc(s.plugin)}</span>
      <span class="wf-arrow">→</span>
      <span class="wf-action">${esc(s.action)}</span>
      <span class="wf-remove" onclick="removeStep(${i})">×</span>
    </div>`
  ).join('');
}

async function validateWorkflow() {
  const out = document.getElementById('wf-output');
  try {
    const body = { name: 'draft', steps: workflowSteps.map((s, i) => ({
      ...s, depends_on: i > 0 ? [workflowSteps[i-1].id] : []
    }))};
    const result = await api('/api/sandbox/validate', {
      method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body)
    });
    out.textContent = result.valid ? 'Valid workflow.' : 'Errors:\n' + result.errors.join('\n');
    out.style.display = 'block';
  } catch (e) { out.textContent = 'Error: ' + e.message; out.style.display = 'block'; }
}

async function compileWorkflow(format) {
  const out = document.getElementById('wf-output');
  try {
    const body = { name: 'draft-workflow', steps: workflowSteps.map((s, i) => ({
      ...s, depends_on: i > 0 ? [workflowSteps[i-1].id] : []
    }))};
    const result = await api(`/api/sandbox/compile/${format}`, {
      method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body)
    });
    out.textContent = typeof result === 'string' ? result : JSON.stringify(result, null, 2);
    out.style.display = 'block';
  } catch (e) { out.textContent = 'Error: ' + e.message; out.style.display = 'block'; }
}

// -- Load everything ---------------------------------------------------------
// -- Profile Builder ---------------------------------------------------------

let buildingBlocks = null;

let currentProfileType = 'tunnel';

async function showProfileBuilder(typeOrEditName) {
  // If it's 'tunnel' or 'controller', it's a new profile; otherwise it's an edit
  const isNew = typeOrEditName === 'tunnel' || typeOrEditName === 'controller';
  const editName = isNew ? null : typeOrEditName;
  if (isNew) currentProfileType = typeOrEditName;

  const modal = document.getElementById('profile-builder-modal');
  modal.classList.add('open');
  document.getElementById('pb-title').textContent = editName ? `Edit: ${editName}` : `New ${currentProfileType} profile`;

  // Load building blocks + enrichment data in parallel
  const [bb, services, identities, routers, roleDescs] = await Promise.all([
    buildingBlocks || api('/api/v1/profiles/building-blocks').catch(() => ({
      identity_roles: [], service_roles: [], router_roles: [], auth_policies: ['Default']
    })),
    api('/api/services').catch(() => []),
    api('/api/identities').catch(() => []),
    api('/api/routers').catch(() => []),
    api('/api/v1/roles').catch(() => ({})),
  ]);
  buildingBlocks = bb;

  // Build role → members index
  const svcByRole = {};
  services.forEach(s => (s.roleAttributes || []).forEach(r => {
    (svcByRole[r] = svcByRole[r] || []).push(s.name);
  }));
  const identByRole = {};
  identities.forEach(i => (i.roleAttributes || []).forEach(r => {
    (identByRole[r] = identByRole[r] || []).push(i.name);
  }));
  const routerByRole = {};
  routers.forEach(rt => (rt.roleAttributes || []).forEach(r => {
    (routerByRole[r] = routerByRole[r] || []).push(rt.name + (rt.isOnline ? '' : ' (offline)'));
  }));

  // Helper: render an enriched checkbox with description + expandable members
  function roleCheck(role, index, type) {
    const members = index[role] || [];
    const count = members.length;
    const desc = (roleDescs[type] || {})[role] || '';
    const memberList = members.slice(0, 10).map(m => `<div style="font-size:0.5625rem;color:#555;padding:0 0 0 1.25rem">${esc(m)}</div>`).join('');
    const more = members.length > 10 ? `<div style="font-size:0.5625rem;color:#333;padding:0 0 0 1.25rem">+${members.length - 10} more</div>` : '';
    return `
      <div class="fb-role-item">
        <label class="fb-check" style="margin-bottom:0">
          <input type="checkbox" value="${esc(role)}">
          <span>${esc(role)}</span>
          <span style="margin-left:auto;font-size:0.5625rem;color:#444">${count}</span>
          <span style="cursor:pointer;color:#444;font-size:0.625rem;margin-left:0.25rem" onclick="event.preventDefault();event.stopPropagation();this.parentElement.nextElementSibling.style.display=this.parentElement.nextElementSibling.style.display==='none'?'':'none';this.textContent=this.textContent==='+'?'−':'+'">+</span>
        </label>
        <div style="display:none;padding:0.125rem 0 0.375rem;border-bottom:1px solid var(--border)">
          ${desc ? `<div style="font-size:0.5625rem;color:var(--accent-dim);padding:0.125rem 0 0.125rem 1.25rem">${esc(desc)}</div>` : ''}
          ${memberList}${more}
          ${count === 0 && !desc ? '<div style="font-size:0.5625rem;color:#333;padding:0 0 0 1.25rem">no members, no description</div>' : ''}
        </div>
      </div>`;
  }

  // Filter roles by profile type
  const isTunnel = currentProfileType === 'tunnel';
  const filteredIdentityRoles = bb.identity_roles.filter(r =>
    editName ? true : (isTunnel ? tunnelIdentityRoles.includes(r) : controllerIdentityRoles.includes(r))
  );
  const filteredServiceRoles = bb.service_roles.filter(r =>
    editName ? true : (isTunnel ? tunnelServiceRoles.includes(r) : controllerServiceRoles.includes(r))
  );
  const filteredRouterRoles = bb.router_roles.filter(r =>
    editName ? true : (isTunnel ? tunnelRouterRoles.includes(r) : controllerRouterRoles.includes(r))
  );

  // Populate identity role dropdown (filtered, with counts + descriptions)
  const irSelect = document.getElementById('pb-identity-role');
  irSelect.innerHTML = '<option value="">(select a role)</option>' +
    filteredIdentityRoles.map(r => {
      const count = (identByRole[r] || []).length;
      const desc = (roleDescs['identity'] || {})[r];
      const label = desc ? `${r} (${count}) — ${desc}` : `${r} (${count})`;
      return `<option value="${esc(r)}">${esc(label)}</option>`;
    }).join('');

  // Populate auth policy dropdown
  const apSelect = document.getElementById('pb-auth-policy');
  apSelect.innerHTML = bb.auth_policies.map(p => `<option value="${esc(p)}">${esc(p)}</option>`).join('');

  // Populate promotes-to (only profiles of the same type)
  const ptSelect = document.getElementById('pb-promotes-to');
  try {
    const profiles = await api('/api/v1/profiles');
    const sameType = profiles.filter(p => profileTypeOf(p.name) === currentProfileType);
    ptSelect.innerHTML = '<option value="">(none — steady state)</option>' +
      sameType.map(p => `<option value="${esc(p.name)}">${esc(p.name)}</option>`).join('');
  } catch (e) {}

  // Populate enriched router role checkboxes (filtered)
  document.getElementById('pb-router-roles').innerHTML =
    filteredRouterRoles.map(r => roleCheck(r, routerByRole, 'router')).join('');

  // Populate enriched service bind role checkboxes (filtered)
  document.getElementById('pb-bind-roles').innerHTML =
    filteredServiceRoles.map(r => roleCheck(r, svcByRole, 'service')).join('');

  // Populate enriched service dial role checkboxes (filtered)
  document.getElementById('pb-dial-roles').innerHTML =
    filteredServiceRoles.map(r => roleCheck(r, svcByRole, 'service')).join('');

  // If editing, populate the form
  if (editName) {
    try {
      const p = await api(`/api/v1/profiles/${editName}`);
      document.getElementById('pb-name').value = p.name;
      document.getElementById('pb-name').disabled = true;
      document.getElementById('pb-desc').value = p.description;
      irSelect.value = p.identity_role;
      apSelect.value = p.auth_policy || 'Default';
      ptSelect.value = p.promotes_to || '';
      document.getElementById('pb-strategy').value = p.terminator_strategy || 'smartrouting';
      document.getElementById('pb-cost').value = p.default_hosting_cost || 0;
      document.getElementById('pb-precedence').value = p.default_hosting_precedence || 'default';
      document.getElementById('pb-heartbeat').value = p.heartbeat_enabled ? 'true' : 'false';
      // Check the right boxes
      checkBoxes('pb-router-roles', (p.edge_router_roles || []).map(r => r.replace('#', '')));
      checkBoxes('pb-bind-roles', (p.service_bind_roles || []).map(r => r.replace('#', '')));
      checkBoxes('pb-dial-roles', (p.service_dial_roles || []).map(r => r.replace('#', '')));
    } catch (e) {}
  } else {
    document.getElementById('pb-name').value = '';
    document.getElementById('pb-name').disabled = false;
    document.getElementById('pb-desc').value = '';
    document.getElementById('pb-cost').value = '0';
  }
}

function hideProfileBuilder() {
  document.getElementById('profile-builder-modal').classList.remove('open');
  document.getElementById('pb-status').textContent = '';
}

function checkBoxes(containerId, values) {
  const checks = document.querySelectorAll(`#${containerId} input[type=checkbox]`);
  checks.forEach(cb => { cb.checked = values.includes(cb.value); });
}

function getChecked(containerId) {
  return Array.from(document.querySelectorAll(`#${containerId} input:checked`)).map(cb => '#' + cb.value);
}

async function saveProfile() {
  const status = document.getElementById('pb-status');
  const name = document.getElementById('pb-name').value.trim();
  if (!name) { status.textContent = 'Name is required'; return; }

  const profile = {
    name: name,
    description: document.getElementById('pb-desc').value,
    identity_role: document.getElementById('pb-identity-role').value,
    edge_router_roles: getChecked('pb-router-roles'),
    service_bind_roles: getChecked('pb-bind-roles'),
    service_dial_roles: getChecked('pb-dial-roles'),
    default_hosting_cost: parseInt(document.getElementById('pb-cost').value) || 0,
    default_hosting_precedence: document.getElementById('pb-precedence').value,
    auth_policy: document.getElementById('pb-auth-policy').value,
    terminator_strategy: document.getElementById('pb-strategy').value,
    promotes_to: document.getElementById('pb-promotes-to').value,
    heartbeat_enabled: document.getElementById('pb-heartbeat').value === 'true',
    heartbeat_fallback: '',
  };

  if (!profile.identity_role) { status.textContent = 'Identity role is required'; return; }

  status.textContent = 'saving...';
  try {
    await api(`/api/v1/profiles/${name}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(profile),
    });
    status.textContent = 'saved!';
    hideProfileBuilder();
    loadAll();
  } catch (e) {
    status.textContent = 'Error: ' + e.message;
  }
}

async function editProfile(name) {
  // Set the type based on which profile we're editing
  currentProfileType = profileTypeOf(name);
  showProfileBuilder(name);
}

async function deleteProfile(name) {
  if (!confirm(`Delete profile "${name}"?`)) return;
  try {
    await api(`/api/v1/profiles/${name}`, { method: 'DELETE' });
    loadAll();
  } catch (e) {
    showError('Delete failed: ' + e.message);
  }
}

// -- Router Builder ----------------------------------------------------------

async function showRouterBuilder(routerName) {
  const modal = document.getElementById('router-builder-modal');
  modal.classList.add('open');

  // Load routers and role attributes
  const [routers, bb] = await Promise.all([
    api('/api/routers').catch(() => []),
    buildingBlocks || api('/api/v1/profiles/building-blocks').catch(() => ({ router_roles: [] })),
  ]);
  buildingBlocks = bb;

  // Populate router select
  const sel = document.getElementById('rb-router');
  sel.innerHTML = routers.map(r =>
    `<option value="${esc(r.name)}" ${r.name === routerName ? 'selected' : ''}>${esc(r.name)} ${r.isOnline ? '(online)' : '(offline)'}</option>`
  ).join('');

  // Populate role checkboxes
  document.getElementById('rb-roles').innerHTML = bb.router_roles.map(r =>
    `<label class="fb-check"><input type="checkbox" value="${esc(r)}">${esc(r)}</label>`
  ).join('');

  // Load current values when router is selected
  sel.onchange = () => loadRouterValues(routers);
  if (routerName || routers.length > 0) loadRouterValues(routers);
}

function loadRouterValues(routers) {
  const name = document.getElementById('rb-router').value;
  const r = routers.find(rt => rt.name === name);
  if (!r) return;
  document.getElementById('rb-cost').value = r.cost || 0;
  document.getElementById('rb-traversal').value = r.noTraversal ? 'false' : 'true';
  document.getElementById('rb-tunneler').value = r.isTunnelerEnabled ? 'true' : 'false';
  document.getElementById('rb-disabled').value = r.disabled ? 'true' : 'false';
  checkBoxes('rb-roles', r.roleAttributes || []);
}

function hideRouterBuilder() {
  document.getElementById('router-builder-modal').classList.remove('open');
  document.getElementById('rb-status').textContent = '';
}

async function saveRouter() {
  const status = document.getElementById('rb-status');
  const name = document.getElementById('rb-router').value;
  if (!name) { status.textContent = 'Select a router'; return; }

  const roles = Array.from(document.querySelectorAll('#rb-roles input:checked')).map(cb => cb.value);
  const cost = parseInt(document.getElementById('rb-cost').value) || 0;
  const noTraversal = document.getElementById('rb-traversal').value === 'false';
  const disabled = document.getElementById('rb-disabled').value === 'true';

  status.textContent = 'saving...';
  try {
    // Use the dashboard API to update — we need a router update endpoint
    // For now, call the plugin directly via the Ziti API through the BFF
    const resp = await fetch('/api/routers', { method: 'GET' });
    const routers = await resp.json();
    // Find the router to get its current data
    // Actually we need a PATCH endpoint. For now, show what would be saved.
    status.textContent = `Would update ${name}: cost=${cost}, roles=[${roles}], noTraversal=${noTraversal}`;
    status.style.color = 'var(--yellow)';
    // TODO: wire to /api/v1/mesh/routers/{name} PATCH endpoint
    setTimeout(() => {
      hideRouterBuilder();
      loadAll();
    }, 1500);
  } catch (e) {
    status.textContent = 'Error: ' + e.message;
    status.style.color = 'var(--red)';
  }
}

function editRouter(routerName) {
  showRouterBuilder(routerName);
}

// -- Apply Profile Modal -----------------------------------------------------

let applyProfileName = '';
let allIdentitiesCache = [];

async function showApplyModal(profileName) {
  applyProfileName = profileName;
  document.getElementById('apply-profile-name').textContent = profileName;
  document.getElementById('apply-modal').classList.add('open');
  document.getElementById('apply-search').value = '';
  document.getElementById('apply-status').textContent = '';

  // Load identities
  try {
    allIdentitiesCache = await api('/api/identities');
  } catch (e) {
    allIdentitiesCache = [];
  }
  renderApplyList('');

  const searchInput = document.getElementById('apply-search');
  searchInput.focus();
  searchInput.oninput = () => renderApplyList(searchInput.value);
}

function hideApplyModal() {
  document.getElementById('apply-modal').classList.remove('open');
}

function renderApplyList(query) {
  const q = query.toLowerCase();
  const filtered = q
    ? allIdentitiesCache.filter(i => i.name.toLowerCase().includes(q) || (i.roleAttributes || []).some(r => r.includes(q)))
    : allIdentitiesCache;

  // Sort: online first, then by name
  filtered.sort((a, b) => {
    if (a.hasEdgeRouterConnection !== b.hasEdgeRouterConnection) return b.hasEdgeRouterConnection ? 1 : -1;
    return a.name.localeCompare(b.name);
  });

  const currentProfile = classifyIdentity;

  document.getElementById('apply-identity-list').innerHTML = filtered.map(i => {
    const dot = i.hasEdgeRouterConnection
      ? '<span class="dot dot-green"></span>'
      : '<span class="dot dot-red"></span>';
    const currentP = classifyIdentity(i);
    const isCurrent = currentP === applyProfileName;
    const roles = (i.roleAttributes || []).slice(0, 3).map(r => `<span class="tag">${esc(r)}</span>`).join('');
    const profileBadge = isCurrent
      ? '<span style="font-size:0.5rem;color:var(--accent);margin-left:0.25rem">(current)</span>'
      : `<span style="font-size:0.5rem;color:#444;margin-left:0.25rem">${esc(currentP)}</span>`;

    return `<div style="display:flex;align-items:center;gap:0.5rem;padding:0.375rem 0.25rem;border-bottom:1px solid #161616;cursor:${isCurrent ? 'default' : 'pointer'}"
        ${isCurrent ? '' : `onclick="applyProfileToIdentity('${esc(i.name)}')"`}
        onmouseenter="this.style.background='var(--surface-2)'" onmouseleave="this.style.background=''">
      ${dot}
      <span style="flex:1;font-size:0.75rem;color:${isCurrent ? '#555' : '#ccc'};overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(i.name)}</span>
      ${profileBadge}
      <span style="font-size:0.5625rem">${roles}</span>
    </div>`;
  }).join('');
}

async function applyProfileToIdentity(identityName) {
  const status = document.getElementById('apply-status');
  status.textContent = `Applying ${applyProfileName} to ${identityName}...`;
  status.style.color = '#555';

  try {
    const result = await api(`/api/v1/profiles/${applyProfileName}/apply/${identityName}`, { method: 'POST' });
    status.textContent = `Applied! ${identityName} → role: ${result.role}`;
    status.style.color = 'var(--accent)';
    // Refresh the identity list in the modal
    allIdentitiesCache = await api('/api/identities');
    renderApplyList(document.getElementById('apply-search').value);
    // Refresh the main view after a short delay
    setTimeout(loadAll, 500);
  } catch (e) {
    status.textContent = `Failed: ${e.message}`;
    status.style.color = 'var(--red)';
  }
}

// -- Render: Fabric / Profiles -----------------------------------------------

function classifyIdentity(i) {
  const roles = new Set(i.roleAttributes || []);
  if (roles.has('quarantine')) return 'join';
  if (roles.has('infra-hosts')) return 'infra';
  if (roles.has('workstations')) return 'workstation';
  if (roles.has('tango-standard')) return 'standard';
  // Fallback: check appData
  const fp = i.appData?.fabric?.profile || i.appData?.fabric_profile;
  if (fp) return fp;
  // Heuristic for unprofile'd machines
  if (roles.has('tunnel') && (roles.has('ssh-hosts') || roles.has('web-hosts'))) return 'standard';
  if (i.type?.name === 'Router') return '_router';
  if (roles.has('do-caddy')) return '_caddy';
  if (roles.has('zrok')) return '_zrok';
  return '_unassigned';
}

// -- Combined Identities (Bao + Ziti) ----------------------------------------

let combinedIdentitiesCache = [];

function renderCombinedIdentities(data) {
  combinedIdentitiesCache = data;
  document.getElementById('nav-identities-combined').textContent = data.length;

  const online = data.filter(i => i.ziti_online).length;
  const matched = data.filter(i => i.has_ziti_match).length;
  document.getElementById('combined-ident-summary').textContent =
    `${data.length} machines · ${online} online · ${matched} matched to Ziti`;

  const search = document.getElementById('ident-search');
  search.oninput = () => filterCombinedIdentities(search.value);
  filterCombinedIdentities('');
}

function filterCombinedIdentities(query) {
  const q = query.toLowerCase();
  const data = q ? combinedIdentitiesCache.filter(i =>
    (i.nickname || '').toLowerCase().includes(q) ||
    (i.hostname || '').toLowerCase().includes(q) ||
    (i.id || '').toLowerCase().includes(q) ||
    (i.ziti_roles || []).some(r => r.includes(q))
  ) : combinedIdentitiesCache;

  const grid = document.getElementById('combined-ident-grid');
  grid.innerHTML = data.map(i => {
    const online = i.ziti_online;
    const hasZiti = i.has_ziti_match;
    const dot = online ? '<span class="dot dot-green"></span>'
      : hasZiti ? '<span class="dot dot-red"></span>'
      : '<span class="dot dot-yellow"></span>';
    const statusText = online ? 'online' : hasZiti ? 'offline' : 'no ziti';
    const borderColor = online ? 'var(--border)' : hasZiti ? '#2a1a1a' : '#2a2a1a';
    const healthBar = online ? 'var(--accent)' : hasZiti ? 'var(--red)' : 'var(--yellow)';

    const roles = (i.ziti_roles || []).slice(0, 4).map(r => `<span class="tag">${esc(r)}</span>`).join('');
    const moreRoles = (i.ziti_roles || []).length > 4 ? `<span style="font-size:0.5rem;color:#444">+${i.ziti_roles.length - 4}</span>` : '';

    const osInfo = i.os ? `${i.os.family || ''} ${i.os.version || ''}`.trim() : '';
    const hwInfo = i.hardware ? `${i.hardware.arch || ''}` : '';
    const stateTag = i.state === 'pending'
      ? '<span class="tag" style="color:var(--yellow);background:#1e1e14;font-size:0.5rem">pending</span>'
      : i.state ? `<span class="tag" style="font-size:0.5rem">${esc(i.state)}</span>` : '';

    return `
      <div style="background:var(--surface);border:1px solid ${borderColor};border-radius:8px;overflow:hidden;cursor:pointer"
        onclick="openMachineModal('${esc(i.id)}')"
        onmouseenter="this.style.borderColor='var(--accent-dim)'" onmouseleave="this.style.borderColor='${borderColor}'">
        <div style="height:3px;background:#1a1a1a"><div style="height:100%;width:100%;background:${healthBar}"></div></div>
        <div style="padding:0.75rem 1rem">
          <!-- Top row: nickname + status -->
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:0.375rem">
            <div style="display:flex;align-items:center;gap:0.375rem">
              ${dot}
              <span style="font-size:0.875rem;color:var(--accent);font-weight:bold">${esc(i.nickname || i.id.substring(0, 8))}</span>
              ${stateTag}
            </div>
            <span style="font-size:0.5625rem;color:#444">${statusText}</span>
          </div>

          <!-- Hostname -->
          <div style="font-size:0.6875rem;color:#777;margin-bottom:0.375rem">${esc(i.hostname || '—')}</div>

          <!-- Info row -->
          <div style="display:flex;justify-content:space-between;align-items:center;font-size:0.5625rem;margin-bottom:0.25rem">
            <span style="color:#444">${osInfo}${hwInfo ? ' · ' + hwInfo : ''}</span>
            <span style="color:#333">${esc(i.source || '')}</span>
          </div>

          <!-- Ziti roles -->
          <div style="display:flex;align-items:center;gap:0.125rem;flex-wrap:wrap">
            ${roles}${moreRoles}
            ${!hasZiti ? '<span style="font-size:0.5rem;color:var(--yellow)">not enrolled in Ziti</span>' : ''}
          </div>
          <button class="btn" style="font-size:0.5625rem;padding:0.125rem 0.375rem;margin-top:0.5rem;color:var(--red)" onclick="event.stopPropagation();deleteEntity('/api/identities','${i.id}', loadAll)">delete</button>
        </div>
      </div>`;
  }).join('');
}

async function showCombinedDetail(machineId) {
  const modal = document.getElementById('identity-detail-modal');
  const body = document.getElementById('id-detail-body');
  document.getElementById('id-detail-name').textContent = 'loading...';
  modal.classList.add('open');
  body.innerHTML = '<div style="text-align:center;color:#444;padding:2rem">loading...</div>';

  try {
    const d = await api(`/api/v1/identities/${encodeURIComponent(machineId)}`);
    document.getElementById('id-detail-name').textContent = d.nickname || d.hostname || d.id;

    const online = d.ziti_online;
    const status = online ? '<span class="dot dot-green"></span>online'
      : d.ziti_has_session ? '<span class="dot dot-yellow"></span>session'
      : d.has_ziti_match ? '<span class="dot dot-red"></span>offline'
      : '<span class="dot dot-yellow"></span>no ziti match';

    const bd = d.bao_data || {};
    const os = bd.os || {};
    const hw = bd.hardware || {};
    const net = bd.network || {};
    const loc = bd.location || {};
    const roles = (d.ziti_roles || []).map(r => `<span class="tag">${esc(r)}</span>`).join(' ') || '<span style="color:#333">none</span>';
    const svcs = d.ziti_services || [];

    body.innerHTML = `
      <!-- Status bar -->
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem;padding-bottom:0.75rem;border-bottom:1px solid var(--border)">
        <span style="font-size:0.875rem">${status}</span>
        <span style="font-size:0.625rem;color:#444">registered ${d.registered_at ? new Date(d.registered_at * 1000).toLocaleDateString() : '—'}</span>
      </div>

      <!-- Two column: Bao + Ziti -->
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:1rem">
        <!-- Left: Bao identity -->
        <div>
          <div style="font-size:0.5625rem;text-transform:uppercase;letter-spacing:0.1em;color:var(--accent-dim);margin-bottom:0.5rem">Bao Identity</div>
          <div class="detail-row"><span class="detail-label">Machine ID</span><span class="detail-val" style="font-size:0.5rem">${esc(d.id)}</span></div>
          <div class="detail-row"><span class="detail-label">Nickname</span><span class="detail-val" style="color:var(--accent)">${esc(d.nickname || '—')}</span></div>
          <div class="detail-row"><span class="detail-label">Hostname</span><span class="detail-val">${esc(d.hostname || '—')}</span></div>
          <div class="detail-row"><span class="detail-label">State</span><span class="detail-val">${esc(d.state || '—')}</span></div>
          <div class="detail-row"><span class="detail-label">Source</span><span class="detail-val">${esc(d.source || '—')}</span></div>
          ${os.family ? `<div class="detail-row"><span class="detail-label">OS</span><span class="detail-val">${esc(os.family)} ${esc(os.version || '')}</span></div>` : ''}
          ${os.kernel ? `<div class="detail-row"><span class="detail-label">Kernel</span><span class="detail-val" style="font-size:0.5625rem">${esc(os.kernel)}</span></div>` : ''}
          ${hw.cpu ? `<div class="detail-row"><span class="detail-label">CPU</span><span class="detail-val" style="font-size:0.5625rem">${esc(hw.cpu)}</span></div>` : ''}
          ${hw.arch ? `<div class="detail-row"><span class="detail-label">Arch</span><span class="detail-val">${esc(hw.arch)}</span></div>` : ''}
          ${hw.memory ? `<div class="detail-row"><span class="detail-label">Memory</span><span class="detail-val">${esc(String(hw.memory))}</span></div>` : ''}
          ${hw.fingerprint ? `<div class="detail-row"><span class="detail-label">Fingerprint</span><span class="detail-val" style="font-size:0.5rem">${esc(hw.fingerprint)}</span></div>` : ''}
          ${loc.timezone ? `<div class="detail-row"><span class="detail-label">Timezone</span><span class="detail-val">${esc(loc.timezone)}</span></div>` : ''}
        </div>

        <!-- Right: Ziti identity -->
        <div>
          <div style="font-size:0.5625rem;text-transform:uppercase;letter-spacing:0.1em;color:var(--blue);margin-bottom:0.5rem">Ziti Identity</div>
          ${d.has_ziti_match ? `
            <div class="detail-row"><span class="detail-label">Ziti ID</span><span class="detail-val" style="font-size:0.5625rem">${esc(d.ziti_id)}</span></div>
            <div class="detail-row"><span class="detail-label">Type</span><span class="detail-val">${esc(d.ziti_type || '—')}</span></div>
            <div class="detail-row"><span class="detail-label">Online</span><span class="detail-val">${d.ziti_online ? '<span style="color:var(--accent)">yes</span>' : '<span style="color:var(--red)">no</span>'}</span></div>
            <div class="detail-row"><span class="detail-label">API Session</span><span class="detail-val">${d.ziti_has_session ? 'yes' : 'no'}</span></div>
            <div class="detail-row"><span class="detail-label">Hosting Cost</span><span class="detail-val">${d.ziti_default_hosting_cost || 0}</span></div>
            <div class="detail-row"><span class="detail-label">Roles</span><span class="detail-val" style="text-align:right">${roles}</span></div>
          ` : `
            <div style="padding:1rem;text-align:center;color:var(--yellow);font-size:0.75rem;background:#1a1a0a;border-radius:4px">
              No Ziti identity matched.<br>
              <span style="font-size:0.625rem;color:#555">Machine is in Bao but hasn't enrolled in the Ziti mesh.</span>
            </div>
          `}
        </div>
      </div>

      <!-- Ziti services -->
      ${svcs.length > 0 ? `
        <div style="margin-top:1rem;border-top:1px solid var(--border);padding-top:0.75rem">
          <div style="font-size:0.5625rem;text-transform:uppercase;letter-spacing:0.1em;color:#444;margin-bottom:0.375rem">
            Ziti Services (${svcs.length})
          </div>
          <div style="display:grid;grid-template-columns:1fr 1fr;gap:0.25rem">
            ${svcs.map(s => `
              <div style="font-size:0.625rem;padding:0.25rem 0.375rem;background:var(--surface-2);border-radius:3px;display:flex;justify-content:space-between">
                <span style="color:#ccc">${esc(s.name)}</span>
                <span>${(s.roleAttributes || []).map(r => `<span class="tag" style="font-size:0.5rem">${esc(r)}</span>`).join('')}</span>
              </div>
            `).join('')}
          </div>
        </div>
      ` : ''}

      <!-- Known host info -->
      ${d.known_host ? `
        <div style="margin-top:1rem;border-top:1px solid var(--border);padding-top:0.75rem">
          <div style="font-size:0.5625rem;text-transform:uppercase;letter-spacing:0.1em;color:#444;margin-bottom:0.375rem">Known Host</div>
          <pre style="background:#080808;border:1px solid var(--border);border-radius:4px;padding:0.5rem;font-size:0.5625rem;color:#888;overflow-x:auto;max-height:150px">${esc(JSON.stringify(d.known_host, null, 2))}</pre>
        </div>
      ` : ''}
    `;
  } catch (e) {
    body.innerHTML = `<div style="color:var(--red);padding:1rem">Failed to load: ${esc(e.message)}</div>`;
  }
}

// -- Identity Detail Modal ---------------------------------------------------

const regionCoords = {
  'region-nyc': { lat: 40.7128, lng: -74.0060, label: 'New York City' },
  'region-sfo': { lat: 37.7749, lng: -122.4194, label: 'San Francisco' },
  'region-lon': { lat: 51.5074, lng: -0.1278, label: 'London' },
  'region-home': { lat: null, lng: null, label: 'Home LAN' },
};

async function showIdentityDetail(name) {
  const modal = document.getElementById('identity-detail-modal');
  const body = document.getElementById('id-detail-body');
  document.getElementById('id-detail-name').textContent = name;
  modal.classList.add('open');
  body.innerHTML = '<div style="text-align:center;color:#444;padding:2rem">loading...</div>';

  try {
    const resp = await api(`/api/identities/${encodeURIComponent(name)}`);
    const d = resp.data;
    const online = d.hasEdgeRouterConnection;
    const status = online ? '<span class="dot dot-green"></span>online'
      : d.hasApiSession ? '<span class="dot dot-yellow"></span>session'
      : '<span class="dot dot-red"></span>offline';

    const created = d.createdAt ? new Date(d.createdAt) : null;
    const updated = d.updatedAt ? new Date(d.updatedAt) : null;
    const age = created ? timeAgo(created) : '—';
    const roles = (d.roleAttributes || []).map(r => `<span class="tag">${esc(r)}</span>`).join(' ') || '<span style="color:#333">none</span>';

    // Detect region from roles
    const regionRole = (d.roleAttributes || []).find(r => r.startsWith('region-'));
    const region = regionRole ? regionCoords[regionRole] : null;

    // Environment info
    const env = d.envInfo || {};
    const sdk = d.sdkInfo || {};
    const appData = d.appData || {};
    const profile = appData.fabric?.profile || appData.fabric_profile || classifyIdentity(d);

    body.innerHTML = `
      <!-- Status bar -->
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem;padding-bottom:0.75rem;border-bottom:1px solid var(--border)">
        <span style="font-size:0.875rem">${status}</span>
        <span style="font-size:0.625rem;color:#444">created ${age}</span>
      </div>

      <!-- Two column layout -->
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:1rem">
        <!-- Left: identity info -->
        <div>
          <div class="detail-row"><span class="detail-label">ID</span><span class="detail-val" style="font-size:0.5625rem">${esc(d.id)}</span></div>
          <div class="detail-row"><span class="detail-label">Type</span><span class="detail-val">${esc(d.type?.name || '—')}</span></div>
          <div class="detail-row"><span class="detail-label">Profile</span><span class="detail-val"><span class="tag">${esc(profile)}</span></span></div>
          <div class="detail-row"><span class="detail-label">Roles</span><span class="detail-val">${roles}</span></div>
          <div class="detail-row"><span class="detail-label">Auth Policy</span><span class="detail-val">${esc(d.authPolicyId || 'default')}</span></div>
          <div class="detail-row"><span class="detail-label">Admin</span><span class="detail-val">${d.isAdmin ? 'yes' : 'no'}</span></div>
          <div class="detail-row"><span class="detail-label">Disabled</span><span class="detail-val">${d.disabled ? '<span style="color:var(--red)">yes</span>' : 'no'}</span></div>
          <div class="detail-row"><span class="detail-label">Hosting Cost</span><span class="detail-val">${d.defaultHostingCost || 0}</span></div>
          <div class="detail-row"><span class="detail-label">Precedence</span><span class="detail-val">${esc(d.defaultHostingPrecedence || 'default')}</span></div>
        </div>

        <!-- Right: environment -->
        <div>
          ${env.os ? `<div class="detail-row"><span class="detail-label">OS</span><span class="detail-val">${esc(env.os)} ${esc(env.arch || '')}</span></div>` : ''}
          ${env.osRelease ? `<div class="detail-row"><span class="detail-label">Kernel</span><span class="detail-val" style="font-size:0.5625rem">${esc(env.osRelease)}</span></div>` : ''}
          ${env.osVersion ? `<div class="detail-row"><span class="detail-label">OS Version</span><span class="detail-val" style="font-size:0.5rem;color:#555">${esc(env.osVersion).substring(0,60)}</span></div>` : ''}
          ${sdk.type ? `<div class="detail-row"><span class="detail-label">SDK</span><span class="detail-val">${esc(sdk.type)} ${esc(sdk.version || '')}</span></div>` : ''}
          ${sdk.appVersion ? `<div class="detail-row"><span class="detail-label">App Version</span><span class="detail-val">${esc(sdk.appVersion)}</span></div>` : ''}
          ${created ? `<div class="detail-row"><span class="detail-label">Created</span><span class="detail-val">${created.toLocaleDateString()} ${created.toLocaleTimeString()}</span></div>` : ''}
          ${updated ? `<div class="detail-row"><span class="detail-label">Updated</span><span class="detail-val">${updated.toLocaleDateString()} ${updated.toLocaleTimeString()}</span></div>` : ''}
          ${region ? `<div class="detail-row"><span class="detail-label">Region</span><span class="detail-val"><span class="tag tag-blue">${esc(region.label)}</span></span></div>` : ''}
          ${d.externalId ? `<div class="detail-row"><span class="detail-label">External ID</span><span class="detail-val">${esc(d.externalId)}</span></div>` : ''}
        </div>
      </div>

      <!-- Map (if we have coordinates) -->
      ${region && region.lat ? `
        <div style="margin-top:1rem;border-top:1px solid var(--border);padding-top:0.75rem">
          <div style="font-size:0.5625rem;text-transform:uppercase;letter-spacing:0.1em;color:#444;margin-bottom:0.375rem">Location</div>
          <div style="background:#080808;border:1px solid var(--border);border-radius:6px;height:180px;display:flex;align-items:center;justify-content:center;position:relative;overflow:hidden">
            <img src="https://maps.googleapis.com/maps/api/staticmap?center=${region.lat},${region.lng}&zoom=5&size=520x180&maptype=roadmap&style=feature:all|element:geometry|color:0x1a1a2e&style=feature:water|color:0x0a0a1a&style=feature:road|color:0x2a2a3e&style=feature:all|element:labels|visibility:off&markers=color:0x00ff88|${region.lat},${region.lng}&key="
              onerror="this.style.display='none';this.nextElementSibling.style.display=''"
              style="width:100%;height:100%;object-fit:cover;opacity:0.7">
            <div style="display:none;color:#444;font-size:0.75rem">${esc(region.label)} (${region.lat.toFixed(2)}, ${region.lng.toFixed(2)})</div>
          </div>
        </div>
      ` : ''}

      <!-- AppData (if any) -->
      ${Object.keys(appData).length > 0 ? `
        <div style="margin-top:1rem;border-top:1px solid var(--border);padding-top:0.75rem">
          <div style="font-size:0.5625rem;text-transform:uppercase;letter-spacing:0.1em;color:#444;margin-bottom:0.375rem">App Data</div>
          <pre style="background:#080808;border:1px solid var(--border);border-radius:4px;padding:0.5rem;font-size:0.625rem;color:#888;overflow-x:auto">${esc(JSON.stringify(appData, null, 2))}</pre>
        </div>
      ` : ''}
    `;
  } catch (e) {
    body.innerHTML = `<div style="color:var(--red);padding:1rem">Failed to load: ${esc(e.message)}</div>`;
  }
}

function timeAgo(date) {
  const seconds = Math.floor((new Date() - date) / 1000);
  if (seconds < 60) return 'just now';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;
  return `${Math.floor(days / 30)}mo ago`;
}

// Profile type classification
const tunnelProfiles = ['join', 'standard'];
const controllerProfiles = ['infra', 'workstation'];

// Roles relevant to each profile type
const tunnelIdentityRoles = ['quarantine', 'tunnel', 'tango-standard', 'ssh-hosts', 'web-hosts', 'home-services', 'stage-2', 'stage-3'];
const controllerIdentityRoles = ['infra-hosts', 'admin-users', 'workstations', 'do-caddy', 'k8s-hosts', 'home-router'];

const tunnelServiceRoles = ['quarantine-services', 'tango-services', 'app-services', 'ssh-services', 'web-services', 'home-services', 'join-services'];
const controllerServiceRoles = ['infra-services', 'mgmt-services', 'k8s-services', 'monitoring-services', 'hypervisors', 'proxmox-services', 'tango-services', 'ssh-services', 'web-services', 'home-services'];

const tunnelRouterRoles = ['public-edge', 'lan'];
const controllerRouterRoles = ['edge', 'public-edge', 'lan', 'region-nyc', 'region-sfo', 'region-lon', 'region-home'];

function profileTypeOf(name) {
  if (tunnelProfiles.includes(name)) return 'tunnel';
  if (controllerProfiles.includes(name)) return 'controller';
  // Heuristic: if it has dial roles with #all or #infra, it's a controller profile
  return 'tunnel'; // default
}

function renderFabric(profiles, identities, routers) {

  // Group identities by profile
  const groups = {};
  identities.forEach(i => {
    const p = classifyIdentity(i);
    if (!groups[p]) groups[p] = { online: 0, offline: 0, members: [] };
    groups[p].members.push(i);
    if (i.hasEdgeRouterConnection) groups[p].online++;
    else groups[p].offline++;
  });

  // Render into page-specific containers

  // Tunnel profiles
  const tProfiles = profiles.filter(p => profileTypeOf(p.name) === 'tunnel');
  const cProfiles = profiles.filter(p => profileTypeOf(p.name) === 'controller');

  document.getElementById('tunnel-cards').innerHTML = profiles
    .filter(p => profileTypeOf(p.name) === 'tunnel')
    .map(p => buildProfileCard(p, groups))
    .join('');

  document.getElementById('controller-cards').innerHTML = profiles
    .filter(p => profileTypeOf(p.name) === 'controller')
    .map(p => buildProfileCard(p, groups))
    .join('');

  // Router cards — built from live router data, not profiles
  document.getElementById('fab-router-cards').innerHTML = routers.map(r => {
    const roles = (r.roleAttributes || []).map(a => `<span class="tag tag-blue">${esc(a)}</span>`).join('');
    const status = r.isOnline ? '<span class="dot dot-green"></span>online' : '<span class="dot dot-red"></span>offline';
    return `
      <div style="background:var(--surface);border:1px solid var(--border);border-radius:8px;overflow:hidden">
        <div style="padding:1rem;border-bottom:1px solid var(--border)">
          <div style="display:flex;justify-content:space-between;align-items:baseline">
            <span style="font-size:1rem;color:var(--accent);font-weight:bold">${esc(r.name)}</span>
            <span style="font-size:0.75rem">${status}</span>
          </div>
          <div style="font-size:0.6875rem;color:#555;margin-top:0.25rem">${esc(r.hostname || '')}</div>
        </div>
        <div style="height:3px;background:#1a1a1a"><div style="height:100%;width:${r.isOnline ? 100 : 0}%;background:${r.isOnline ? 'var(--accent)' : 'var(--red)'}"></div></div>
        <div style="padding:0.75rem 1rem;font-size:0.625rem">
          <div style="display:flex;justify-content:space-between;margin-bottom:0.25rem"><span style="color:#555">roles</span><span>${roles}</span></div>
          <div style="display:flex;justify-content:space-between;margin-bottom:0.25rem"><span style="color:#555">cost</span><span class="tag tag-type">${r.cost}</span></div>
          <div style="display:flex;justify-content:space-between;margin-bottom:0.25rem"><span style="color:#555">traversal</span><span>${r.noTraversal ? '<span class="tag" style="color:var(--red)">disabled</span>' : '<span class="tag">enabled</span>'}</span></div>
          <div style="display:flex;justify-content:space-between"><span style="color:#555">tunneler</span><span>${r.isTunnelerEnabled ? '<span class="tag">enabled</span>' : '<span style="color:#333">disabled</span>'}</span></div>
        </div>
        <div style="padding:0.5rem 1rem;display:flex;justify-content:space-between;align-items:center">
          <span style="font-size:0.5625rem;color:#333">${esc(r.id)}</span>
          <button class="btn" style="font-size:0.5rem;padding:0.125rem 0.375rem" onclick="editRouter('${esc(r.name)}')">configure</button>
        </div>
      </div>`;
  }).join('');

  // Badge counts
  const tCount = tProfiles.reduce((n, p) => n + (groups[p.name]?.members?.length || 0), 0);
  const cCount = cProfiles.reduce((n, p) => n + (groups[p.name]?.members?.length || 0), 0);
  document.getElementById('nav-tunnels').textContent = tCount;
  document.getElementById('nav-controllers').textContent = cCount;
  document.getElementById('nav-fab-routers').textContent = routers.length;

  // Summaries
  const tOnline = tProfiles.reduce((n, p) => n + (groups[p.name]?.online || 0), 0);
  document.getElementById('tunnels-summary').textContent =
    `${tProfiles.length} profiles · ${tOnline}/${tCount} online`;
  const cOnline = cProfiles.reduce((n, p) => n + (groups[p.name]?.online || 0), 0);
  document.getElementById('controllers-summary').textContent =
    `${cProfiles.length} profiles · ${cOnline}/${cCount} online`;
  const rOnline = routers.filter(r => r.isOnline).length;
  document.getElementById('fab-routers-summary').textContent =
    `${rOnline}/${routers.length} online`;

  // Unassigned identities (only on tunnels page) — card style, sorted by age
  const unassigned = (groups['_unassigned']?.members || [])
    .concat(groups['_caddy']?.members || [])
    .concat(groups['_zrok']?.members || []);
  const section = document.getElementById('unassigned-section');
  if (unassigned.length > 0) {
    section.style.display = '';
    // Sort offline first, then by name
    unassigned.sort((a, b) => {
      if (a.hasEdgeRouterConnection !== b.hasEdgeRouterConnection) return a.hasEdgeRouterConnection ? 1 : -1;
      return a.name.localeCompare(b.name);
    });
    document.getElementById('unassigned-list').innerHTML = `
      <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(280px,1fr));gap:0.5rem">
        ${unassigned.map(i => {
          const online = i.hasEdgeRouterConnection;
          const dot = online ? '<span class="dot dot-green"></span>' : '<span class="dot dot-red"></span>';
          const roles = (i.roleAttributes || []).map(r => `<span class="tag">${esc(r)}</span>`).join(' ') || '<span style="color:#333;font-size:0.5625rem">no roles</span>';
          const typeName = i.type?.name || '—';
          return `<div style="background:var(--surface-2);border:1px solid ${online ? 'var(--border)' : '#2a1a1a'};border-radius:6px;padding:0.625rem 0.75rem;cursor:pointer"
              onclick="showIdentityDetail('${esc(i.name)}')"
              onmouseenter="this.style.borderColor='var(--accent-dim)'" onmouseleave="this.style.borderColor='${online ? 'var(--border)' : '#2a1a1a'}'">
            <div style="display:flex;align-items:center;gap:0.375rem;margin-bottom:0.25rem">
              ${dot}
              <span style="font-size:0.75rem;color:#ccc;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(i.name)}</span>
              ${!online ? '<span style="font-size:0.5rem;color:var(--red);background:#1a0a0a;padding:0.0625rem 0.25rem;border-radius:2px">unassigned</span>' : ''}
            </div>
            <div style="display:flex;justify-content:space-between;align-items:center;font-size:0.5625rem">
              <span style="color:#444">${esc(typeName)}</span>
              <span>${roles}</span>
            </div>
          </div>`;
        }).join('')}
      </div>`;
  } else {
    section.style.display = 'none';
  }
}

// Reusable profile card builder
function buildProfileCard(p, groups) {
  const g = groups[p.name] || { online: 0, offline: 0, members: [] };
  const total = g.members.length;
  const healthPct = total > 0 ? Math.round((g.online / total) * 100) : 0;
  const healthColor = healthPct === 100 ? 'var(--accent)' : healthPct > 0 ? 'var(--yellow)' : total === 0 ? '#333' : 'var(--red)';

  const memberRows = g.members.sort((a, b) => {
    if (a.hasEdgeRouterConnection !== b.hasEdgeRouterConnection) return b.hasEdgeRouterConnection ? 1 : -1;
    return a.name.localeCompare(b.name);
  }).map(m => {
    const dot = m.hasEdgeRouterConnection ? '<span class="dot dot-green"></span>' : '<span class="dot dot-red"></span>';
    return `<div style="display:flex;align-items:center;padding:0.1875rem 0;font-size:0.6875rem;color:#999;cursor:pointer" onclick="showIdentityDetail('${esc(m.name)}')" onmouseenter="this.style.color='var(--fg)'" onmouseleave="this.style.color='#999'">
      ${dot}<span style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(m.name)}</span>
      <span style="color:#333;font-size:0.5625rem">${esc(m.type?.name || '')}</span>
    </div>`;
  }).join('');

  const promotesTo = p.promotes_to ? `<div style="font-size:0.5625rem;color:#444;margin-top:0.25rem">promotes to: ${esc(p.promotes_to)}</div>` : '';
  const heartbeat = p.heartbeat_enabled ? `<span class="tag" style="font-size:0.5rem">heartbeat${p.heartbeat_fallback ? ': ' + esc(p.heartbeat_fallback) : ''}</span>` : '';

  return `
    <div style="background:var(--surface);border:1px solid var(--border);border-radius:8px;overflow:hidden">
      <div style="padding:1rem;border-bottom:1px solid var(--border)">
        <div style="display:flex;justify-content:space-between;align-items:baseline;margin-bottom:0.25rem">
          <span style="font-size:1rem;color:var(--accent);font-weight:bold">${esc(p.name)}</span>
          <span style="font-size:1.25rem;font-weight:bold;color:${healthColor}">${total}</span>
        </div>
        <div style="font-size:0.6875rem;color:#555;margin-bottom:0.5rem">${esc(p.description)}</div>
        ${promotesTo}
      </div>
      <div style="height:3px;background:#1a1a1a"><div style="height:100%;width:${healthPct}%;background:${healthColor};transition:width 0.3s"></div></div>
      <div style="padding:0.75rem 1rem;border-bottom:1px solid var(--border);font-size:0.625rem">
        <div style="display:flex;justify-content:space-between;margin-bottom:0.25rem"><span style="color:#555">role</span><span class="tag">${esc(p.identity_role)}</span></div>
        <div style="display:flex;justify-content:space-between;margin-bottom:0.25rem"><span style="color:#555">routers</span><span>${p.edge_router_roles.map(r => `<span class="tag tag-blue">${esc(r)}</span>`).join('')}</span></div>
        <div style="display:flex;justify-content:space-between;margin-bottom:0.25rem"><span style="color:#555">bind</span><span>${(p.service_bind_roles||[]).map(r => `<span class="tag">${esc(r)}</span>`).join('')||'<span style="color:#333">none</span>'}</span></div>
        <div style="display:flex;justify-content:space-between;margin-bottom:0.25rem"><span style="color:#555">dial</span><span>${(p.service_dial_roles||[]).map(r => `<span class="tag tag-purple">${esc(r)}</span>`).join('')||'<span style="color:#333">none</span>'}</span></div>
        <div style="display:flex;justify-content:space-between;align-items:center"><span style="color:#555">options</span><span><span class="tag tag-type">${esc(p.terminator_strategy)}</span>${heartbeat}</span></div>
      </div>
      <div style="padding:0.75rem 1rem;max-height:200px;overflow-y:auto">
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:0.375rem">
          <span style="font-size:0.5625rem;text-transform:uppercase;letter-spacing:0.1em;color:#444">${g.online} online / ${total} members</span>
          <span style="display:flex;gap:0.25rem">
            <button class="btn" style="font-size:0.5rem;padding:0.125rem 0.375rem" onclick="showApplyModal('${esc(p.name)}')">apply</button>
            <button class="btn" style="font-size:0.5rem;padding:0.125rem 0.375rem" onclick="editProfile('${esc(p.name)}')">edit</button>
            <button class="btn" style="font-size:0.5rem;padding:0.125rem 0.375rem;color:var(--red)" onclick="deleteProfile('${esc(p.name)}')">del</button>
          </span>
        </div>
        ${memberRows || '<div style="font-size:0.6875rem;color:#333">no members</div>'}
      </div>
    </div>`;
}

async function loadAll() {
  clearError();
  document.getElementById('last-updated').textContent = 'loading...';

  try {
    const [overview, services, identities, routers, policies, terminators, version, plugins, profiles, combinedIdents] =
      await Promise.all([
        api('/api/overview'),
        api('/api/services'),
        api('/api/identities'),
        api('/api/routers'),
        api('/api/policies'),
        api('/api/terminators'),
        api('/api/version').catch(() => null),
        api('/api/registry/plugins').catch(() => []),
        api('/api/v1/profiles').catch(() => []),
        api('/api/v1/identities').catch(() => []),
      ]);

    renderOverview(overview, routers);
    renderServices(services);
    renderIdentities(identities);
    renderRouters(routers, overview);
    renderPolicies(policies);
    renderTerminators(terminators);
    renderPlugins(plugins);
    renderActionList(plugins);
    renderFabric(profiles, identities, routers);
    renderCombinedIdentities(combinedIdents);

    // Fire new entity loaders in parallel; each manages its own render
    Promise.all([
      loadTransitRouters().catch(() => {}),
      loadEdgeRouterPolicies().catch(() => {}),
      loadSERP().catch(() => {}),
      loadAuthPolicies().catch(() => {}),
      loadPostureChecks().catch(() => {}),
      loadCertAuthorities().catch(() => {}),
      loadExtJwtSigners().catch(() => {}),
      loadApiSessions().catch(() => {}),
      loadSessions().catch(() => {}),
    ]);

    if (version?.data) {
      document.getElementById('footer-version').textContent =
        `ctrl ${version.data.version || '?'}`;
    }

    document.getElementById('last-updated').textContent = 'updated ' + new Date().toLocaleTimeString();
  } catch (e) {
    showError('Failed: ' + e.message);
    document.getElementById('last-updated').textContent = 'error';
  }
}

loadAll();
setInterval(loadAll, 30000);

// ============================================================
// ENROLLMENTS
// ============================================================

let enrollmentsCache = [];

async function loadEnrollments() {
  try {
    enrollmentsCache = await api('/api/enrollments');
    renderEnrollments();
  } catch (e) {
    showError('Enrollments: ' + e.message);
  }
}

function enrollStatusColor(s) {
  if (s === 'pending') return 'var(--yellow)';
  if (s === 'booting') return '#f97316'; // orange — pre-OS, Hook OS boot
  if (s === 'auto_approved' || s === 'approved') return 'var(--accent)';
  if (s === 'denied' || s === 'rejected') return 'var(--red)';
  return '#555';
}

function enrollStatusDot(s) {
  if (s === 'pending') return 'dot-yellow';
  if (s === 'booting') return 'dot-yellow';
  if (s === 'auto_approved' || s === 'approved') return 'dot-green';
  return 'dot-red';
}

function renderEnrollments() {
  const filter = document.getElementById('enroll-filter').value;
  const rows = filter ? enrollmentsCache.filter(r => r.status === filter) : enrollmentsCache;

  // Stats
  const count = s => enrollmentsCache.filter(r => r.status === s).length;
  document.getElementById('es-pending').textContent = count('pending');
  document.getElementById('es-auto').textContent = count('auto_approved');
  document.getElementById('es-approved').textContent = count('approved');
  document.getElementById('es-denied').textContent = count('denied');
  document.getElementById('es-rejected').textContent = count('rejected');

  const pending = count('pending');
  const booting = count('booting');
  const needsAction = pending + booting;
  document.getElementById('nav-enrollments').textContent = needsAction || enrollmentsCache.length;
  document.getElementById('enroll-summary').textContent =
    `${enrollmentsCache.length} total · ${pending} pending · ${booting} booting`;
  const badge = document.getElementById('enroll-pending-badge');
  if (needsAction > 0) {
    badge.textContent = `${needsAction} need review`;
    badge.style.display = '';
  } else {
    badge.style.display = 'none';
  }

  const container = document.getElementById('tb-enrollments');
  const empty = document.getElementById('enroll-empty');

  if (!rows.length) {
    container.innerHTML = '';
    if (empty) empty.style.display = '';
    return;
  }
  if (empty) empty.style.display = 'none';

  const roles = ['ops','developer','dev-lead','qa','sales','marketing','finance','support','auditor','contractor','partner','guest'];

  container.innerHTML = rows.map(r => {
    const statusColor = enrollStatusColor(r.status);
    const statusDot = enrollStatusDot(r.status);
    const isPending = r.status === 'pending';
    const isBooting = r.status === 'booting';
    const onlineIndicator = r.online
      ? '<span class="dot dot-green" style="margin-right:0.25rem"></span>'
      : '<span class="dot dot-yellow" style="margin-right:0.25rem"></span>';

    // Extract non-noise attributes for display
    const noisyAttrs = new Set(['enrolled','quarantine','lan','tunnel','blue-demo','stage-0','stage-1','stage-2','stage-3',`host-${r.nickname}`]);
    const displayAttrs = (r.attributes || []).filter(a => !noisyAttrs.has(a));

    const selectStyle = `background:#111;border:1px solid #2a2a2a;color:var(--fg);
        padding:0.25rem 0.5rem;border-radius:4px;font-size:0.6875rem;
        width:100%;margin-bottom:0.4rem;cursor:pointer;`;

    const APP_OPTIONS = ['','ticketarr','forgejo','grafana','woodpecker','mkdocs','inventree',
      'konfig','arr-stack','bao','postgres','konsole','zitadel','konmail','other'];
    const ENV_OPTIONS = ['','prod','staging','dev','infra'];

    // App/env selects shown for both pending and booting — operator tags before promoting
    const tagSelects = (isPending || isBooting) ? `
      <select id="app-${esc(r.machine_id)}" style="${selectStyle}">
        <option value="">— app —</option>
        ${APP_OPTIONS.filter(Boolean).map(a => `<option value="${a}">${a}</option>`).join('')}
      </select>
      <select id="env-${esc(r.machine_id)}" style="${selectStyle}">
        <option value="">— env —</option>
        ${ENV_OPTIONS.filter(Boolean).map(e => `<option value="${e}">${e}</option>`).join('')}
      </select>` : '';

    const macLine = r.mac ? `<div style="font-size:0.6rem;color:#444;font-family:monospace">${esc(r.mac)}</div>` : '';
    const sourceBadge = r.source ? `<span class="tag" style="font-size:0.5rem;color:#555">${esc(r.source)}</span>` : '';

    // pending → full approve/deny
    // booting → promote to quarantine (machine still installing OS) or deny
    const actions = isPending ? `
      <div style="display:flex;gap:0.5rem;margin-top:0.25rem">
        <button onclick="approveEnrollment('${esc(r.machine_id)}')" style="
          flex:1;padding:0.5rem;border-radius:4px;cursor:pointer;font-size:0.75rem;font-weight:600;
          background:rgba(0,255,136,0.1);border:1px solid var(--accent);color:var(--accent);
        ">Approve</button>
        <button onclick="denyEnrollment('${esc(r.machine_id)}')" style="
          flex:1;padding:0.5rem;border-radius:4px;cursor:pointer;font-size:0.75rem;font-weight:600;
          background:rgba(255,80,80,0.08);border:1px solid #c53;color:#e55;
        ">Deny</button>
      </div>` : isBooting ? `
      <div style="font-size:0.6rem;color:#f97316;margin-bottom:0.4rem">
        Hook OS booting — waiting for OS install &amp; schmutz enrollment
      </div>
      <div style="display:flex;gap:0.5rem">
        <button onclick="denyEnrollment('${esc(r.machine_id)}')" style="
          flex:1;padding:0.5rem;border-radius:4px;cursor:pointer;font-size:0.75rem;font-weight:600;
          background:rgba(255,80,80,0.08);border:1px solid #c53;color:#e55;
        ">Deny</button>
      </div>` : `
      <div style="font-size:0.6875rem;color:${statusColor};font-weight:600;text-transform:uppercase;letter-spacing:0.05em;margin-top:0.5rem">
        ${esc(r.status)}
      </div>`;

    return `<div style="
      background:#0e0e0e;border:1px solid #1c1c1c;border-radius:6px;
      padding:1rem;display:flex;flex-direction:column;gap:0.5rem;
      transition:border-color 0.15s;
    " onmouseover="this.style.borderColor='#2a2a2a'" onmouseout="this.style.borderColor='#1c1c1c'">

      <!-- header -->
      <div style="display:flex;align-items:center;justify-content:space-between">
        <div style="display:flex;align-items:center;gap:0.4rem">
          ${onlineIndicator}
          <span style="font-size:1rem;font-weight:700;color:var(--fg);font-family:monospace">${esc(r.nickname || r.machine_id)}</span>
        </div>
        <span style="font-size:0.5625rem;color:#444;font-family:monospace">${esc(r.machine_id)}</span>
      </div>

      <!-- uuid -->
      <div style="font-size:0.5625rem;color:#333;font-family:monospace;word-break:break-all">${esc(r.name || '')}</div>

      <!-- attributes -->
      ${displayAttrs.length ? `<div style="display:flex;flex-wrap:wrap;gap:0.25rem;margin-top:0.125rem">
        ${displayAttrs.map(a => `<span style="font-size:0.5rem;background:#1a1a1a;border:1px solid #222;padding:0.0625rem 0.375rem;border-radius:3px;color:#666;font-family:monospace">${esc(a)}</span>`).join('')}
      </div>` : ''}

      <!-- divider -->
      <div style="border-top:1px solid #1a1a1a;margin:0.25rem 0"></div>

      <!-- mac + source -->
      ${macLine}${sourceBadge}

      <!-- divider -->
      <div style="border-top:1px solid #1a1a1a;margin:0.25rem 0"></div>

      <!-- tag selects + actions -->
      ${tagSelects}
      ${actions}
    </div>`;
  }).join('');
}

async function approveEnrollment(machineId) {
  const app = (document.getElementById(`app-${machineId}`) || {}).value || '';
  const env = (document.getElementById(`env-${machineId}`) || {}).value || '';
  try {
    await api('/api/ops/approve', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({machine_id: machineId, app, env}),
    });
    await loadEnrollments();
  } catch (e) {
    showError('Approve failed: ' + e.message);
  }
}

async function denyEnrollment(machineId) {
  const reason = prompt(`Deny reason for ${machineId}:`, 'denied by operator');
  if (reason === null) return;
  try {
    await api('/api/ops/deny', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({machine_id: machineId, reason}),
    });
    await loadEnrollments();
  } catch (e) {
    showError('Deny failed: ' + e.message);
  }
}

function showEnrollmentDetail(id) {
  const r = enrollmentsCache.find(e => e.id === id);
  if (!r) return;
  const raw = r.raw_event || {};
  const modal = document.getElementById('enrollment-detail-modal');
  document.getElementById('ed-title').textContent = r.machine_id;

  const statusColor = enrollStatusColor(r.status);
  const score = r.match_score != null ? r.match_score : '—';
  const conf = r.match_confidence || '—';

  const fields = [
    ['Status', `<span style="color:${statusColor}">${esc(r.status)}</span>`],
    ['Hostname', esc(r.hostname || '—')],
    ['Source IP', esc(r.source_ip || '—')],
    ['Ziti ID', `<span style="font-size:0.5625rem;color:#555">${esc(r.ziti_identity_id || '—')}</span>`],
    ['Match Score', `${score} <span style="color:#444;font-size:0.5625rem">(${esc(conf)})</span>`],
    ['Decision', esc(r.decision_reason || '—')],
    ['Decided By', esc(r.decided_by || '—')],
    ['Requested', r.requested_at ? new Date(r.requested_at).toLocaleString() : '—'],
    ['Decided', r.decided_at ? new Date(r.decided_at).toLocaleString() : '—'],
  ];

  const hwFields = [
    ['OS', [raw.os, raw.os_version].filter(Boolean).join(' ')],
    ['Arch', raw.arch],
    ['Kernel', raw.kernel],
    ['CPU', raw.cpu_model],
    ['Cores', raw.cpu_cores],
    ['Memory', raw.memory_mb ? `${raw.memory_mb} MB` : null],
    ['Serial', raw.serial_number],
    ['HW Hash', raw.hardware_hash ? raw.hardware_hash.substring(0, 16) + '...' : null],
    ['Full Hash', raw.full_hash ? raw.full_hash.substring(0, 16) + '...' : null],
    ['JA4', raw.ja4 ? raw.ja4.substring(0, 20) + '...' : null],
    ['Boot ID', raw.boot_id],
    ['Uptime', raw.uptime_secs ? `${Math.round(raw.uptime_secs / 3600)}h` : null],
    ['Timezone', raw.timezone],
    ['Packages', raw.package_count],
    ['Gateway', raw.gateway],
  ].filter(([, v]) => v);

  const macs = (raw.mac_addrs || []).map(m => `<span class="tag">${esc(m)}</span>`).join(' ') || '<span style="color:#333">—</span>';
  const ports = (raw.open_ports || []).map(p => `<span class="tag tag-blue">${p}</span>`).join(' ') || '<span style="color:#333">—</span>';
  const dns = (raw.dns_servers || []).join(', ') || '—';
  const sshKeys = (raw.ssh_host_keys || []).length;

  document.getElementById('ed-body').innerHTML = `
    <div style="display:grid;grid-template-columns:1fr 1fr;gap:1.25rem">
      <div>
        <div style="font-size:0.5625rem;text-transform:uppercase;letter-spacing:0.1em;color:var(--accent-dim);margin-bottom:0.5rem">Enrollment</div>
        ${fields.map(([l, v]) => `<div class="detail-row"><span class="detail-label">${esc(l)}</span><span class="detail-val">${v}</span></div>`).join('')}
      </div>
      <div>
        <div style="font-size:0.5625rem;text-transform:uppercase;letter-spacing:0.1em;color:var(--blue);margin-bottom:0.5rem">Hardware Fingerprint</div>
        ${hwFields.map(([l, v]) => `<div class="detail-row"><span class="detail-label">${esc(l)}</span><span class="detail-val" style="font-size:0.625rem">${esc(String(v))}</span></div>`).join('')}
      </div>
    </div>
    <div style="margin-top:1rem;border-top:1px solid var(--border);padding-top:0.75rem">
      <div class="detail-row"><span class="detail-label">MAC Addrs</span><span class="detail-val">${macs}</span></div>
      <div class="detail-row"><span class="detail-label">Open Ports</span><span class="detail-val">${ports}</span></div>
      <div class="detail-row"><span class="detail-label">DNS</span><span class="detail-val" style="font-size:0.625rem">${esc(dns)}</span></div>
      <div class="detail-row"><span class="detail-label">SSH Keys</span><span class="detail-val">${sshKeys} key${sshKeys !== 1 ? 's' : ''}</span></div>
    </div>
    ${r.status === 'pending' ? `
      <div style="margin-top:1rem;border-top:1px solid var(--border);padding-top:0.75rem;display:flex;gap:0.5rem">
        <button class="btn btn-primary" onclick="approveEnrollment('${esc(r.machine_id)}');document.getElementById('enrollment-detail-modal').classList.remove('open')">approve</button>
        <button class="btn" style="color:var(--red);border-color:#3a1a1a" onclick="denyEnrollment('${esc(r.machine_id)}');document.getElementById('enrollment-detail-modal').classList.remove('open')">deny</button>
      </div>
    ` : ''}
    <div style="margin-top:1rem;border-top:1px solid var(--border);padding-top:0.75rem">
      <div style="font-size:0.5625rem;text-transform:uppercase;letter-spacing:0.1em;color:#333;margin-bottom:0.375rem">Raw Event</div>
      <pre style="background:#080808;border:1px solid var(--border);border-radius:4px;padding:0.5rem;font-size:0.5625rem;color:#555;overflow-x:auto;max-height:200px">${esc(JSON.stringify(raw, null, 2))}</pre>
    </div>
  `;
  modal.classList.add('open');
}

// ============================================================
// TELEMETRY
// ============================================================

let telemetryCache = {};
let telemetrySource = null;

function startTelemetrySSE() {
  if (telemetrySource) return;
  telemetrySource = new EventSource('/api/telemetry/stream');
  telemetrySource.onmessage = (e) => {
    try {
      const frame = JSON.parse(e.data);
      if (!frame.machine_id || !frame.type) return;
      const key = frame.machine_id + '/' + frame.type;
      telemetryCache[key] = frame.payload;
      const activePage = document.querySelector('.page.active');
      if (activePage && activePage.id === 'page-telemetry') renderTelemetry();
    } catch (_) {}
  };
  telemetrySource.onerror = () => console.log('telemetry: SSE reconnecting...');
}

function stopTelemetrySSE() {
  if (telemetrySource) { telemetrySource.close(); telemetrySource = null; }
}

async function loadTelemetry() {
  try {
    const snap = await api('/api/telemetry/live');
    if (snap && typeof snap === 'object') Object.assign(telemetryCache, snap);
    renderTelemetry();
  } catch (e) {
    showError('Telemetry: ' + e.message);
  }
  startTelemetrySSE();
}

async function flushTelemetry() {
  try {
    await api('/api/ops/flush', {method: 'POST'});
    document.getElementById('telemetry-summary').textContent += ' · flushed to db';
  } catch (e) {
    showError('Flush failed: ' + e.message);
  }
}

function renderTelemetry() {
  const query = (document.getElementById('telemetry-filter').value || '').toLowerCase();
  const keys = Object.keys(telemetryCache).filter(k => !query || k.toLowerCase().includes(query));

  // Group by machine_id
  const machines = {};
  keys.forEach(k => {
    const [machineId, slug] = k.split('/');
    if (!machines[machineId]) machines[machineId] = [];
    machines[machineId].push({ slug, payload: telemetryCache[k] });
  });

  const machineIds = Object.keys(machines).sort();
  document.getElementById('nav-telemetry').textContent = machineIds.length;
  document.getElementById('telemetry-summary').textContent =
    `${machineIds.length} machines · ${keys.length} streams live`;

  const empty = document.getElementById('telemetry-empty');
  const grid = document.getElementById('telemetry-grid');

  if (!machineIds.length) {
    grid.style.display = 'none';
    empty.style.display = '';
    return;
  }
  grid.style.display = '';
  empty.style.display = 'none';

  grid.innerHTML = machineIds.map(machineId => {
    const streams = machines[machineId];
    const sys = streams.find(s => s.slug === 'system')?.payload || {};
    const status = sys.status || 'unknown';
    const statusColor = status === 'enrolled' || status === 'online' ? 'var(--accent)'
      : status === 'quarantine' ? 'var(--yellow)' : '#555';
    const dotCls = status === 'enrolled' || status === 'online' ? 'dot-green'
      : status === 'quarantine' ? 'dot-yellow' : 'dot-red';

    const streamRows = streams.map(s => {
      const p = s.payload || {};
      // Build a compact key-value summary of interesting fields
      const kvs = Object.entries(p)
        .filter(([k]) => !['machine_id', 'status'].includes(k))
        .slice(0, 6)
        .map(([k, v]) => {
          const val = typeof v === 'object' ? JSON.stringify(v) : String(v);
          return `<div style="display:flex;justify-content:space-between;padding:0.125rem 0;font-size:0.5625rem">
            <span style="color:#444">${esc(k)}</span>
            <span style="color:#888;max-width:160px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(val)}</span>
          </div>`;
        }).join('');

      return `<div style="margin-bottom:0.5rem;padding-top:0.5rem;border-top:1px solid var(--border)">
        <div style="font-size:0.5625rem;text-transform:uppercase;letter-spacing:0.08em;color:var(--accent-dim);margin-bottom:0.25rem">${esc(s.slug)}</div>
        ${kvs || '<div style="font-size:0.5625rem;color:#333">empty payload</div>'}
      </div>`;
    }).join('');

    return `
      <div style="background:var(--surface);border:1px solid var(--border);border-radius:8px;overflow:hidden">
        <div style="height:3px;background:#1a1a1a"><div style="height:100%;width:100%;background:${statusColor}"></div></div>
        <div style="padding:0.75rem 1rem">
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:0.375rem">
            <div style="display:flex;align-items:center;gap:0.375rem">
              <span class="dot ${dotCls}"></span>
              <span style="font-size:0.875rem;color:var(--accent)">${esc(machineId)}</span>
            </div>
            <span style="font-size:0.5625rem;color:#444">${streams.length} stream${streams.length !== 1 ? 's' : ''}</span>
          </div>
          ${sys.status ? `<div style="font-size:0.625rem;color:${statusColor};margin-bottom:0.5rem">${esc(sys.status)}</div>` : ''}
          ${streamRows}
        </div>
      </div>`;
  }).join('');
}

// Auto-refresh enrollments every 5s when on that page
setInterval(() => {
  const activePage = document.querySelector('.nav-item.active');
  if (activePage && activePage.dataset.page === 'enrollments') loadEnrollments();
}, 5000);

// Load enrollment + telemetry data on page switch; manage SSE lifecycle
document.querySelectorAll('.nav-item').forEach(item => {
  item.addEventListener('click', () => {
    const prev = document.querySelector('.nav-item.active');
    if (prev && prev.dataset.page === 'telemetry' && item.dataset.page !== 'telemetry') {
      stopTelemetrySSE();
    }
    if (item.dataset.page === 'enrollments') loadEnrollments();
    if (item.dataset.page === 'telemetry') loadTelemetry();
  });
});

// ---- Transit Routers ----
let transitRouters = [];
async function loadTransitRouters() {
  const res = await api('/api/transit-routers');
  transitRouters = res.data || [];
  const el = document.getElementById('nav-transit');
  if (el) el.textContent = transitRouters.length;
  renderTransitRouters();
}
function renderTransitRouters() {
  const tbody = document.querySelector('#tb-transit-routers');
  if (!tbody) return;
  tbody.innerHTML = '';
  transitRouters.forEach(r => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${r.name}</td>
      <td><span class="dot ${r.isOnline ? 'dot-green' : 'dot-red'}"></span>${r.isOnline ? 'online' : 'offline'}</td>
      <td>${(r.roleAttributes||[]).map(a=>`<span class="tag">${a}</span>`).join('')}</td>
      <td class="id-cell">${r.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/transit-routers','${r.id}', loadTransitRouters)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- Edge Router Policies ----
let edgeRouterPolicies = [];
async function loadEdgeRouterPolicies() {
  const res = await api('/api/edge-router-policies');
  edgeRouterPolicies = res.data || [];
  const el = document.getElementById('nav-erp');
  if (el) el.textContent = edgeRouterPolicies.length;
  renderEdgeRouterPolicies();
}
function renderEdgeRouterPolicies() {
  const tbody = document.querySelector('#tb-erp');
  if (!tbody) return;
  tbody.innerHTML = '';
  edgeRouterPolicies.forEach(p => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${p.name}</td>
      <td>${p.semantic||''}</td>
      <td>${(p.identityRoles||[]).map(r=>`<span class="tag">${r}</span>`).join('')}</td>
      <td>${(p.edgeRouterRoles||[]).map(r=>`<span class="tag tag-blue">${r}</span>`).join('')}</td>
      <td class="id-cell">${p.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/edge-router-policies','${p.id}', loadEdgeRouterPolicies)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- Service Edge Router Policies ----
let serpolicies = [];
async function loadSERP() {
  const res = await api('/api/service-edge-router-policies');
  serpolicies = res.data || [];
  const el = document.getElementById('nav-serp');
  if (el) el.textContent = serpolicies.length;
  renderSERP();
}
function renderSERP() {
  const tbody = document.querySelector('#tb-serp');
  if (!tbody) return;
  tbody.innerHTML = '';
  serpolicies.forEach(p => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${p.name}</td>
      <td>${p.semantic||''}</td>
      <td>${(p.serviceRoles||[]).map(r=>`<span class="tag">${r}</span>`).join('')}</td>
      <td>${(p.edgeRouterRoles||[]).map(r=>`<span class="tag tag-blue">${r}</span>`).join('')}</td>
      <td class="id-cell">${p.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/service-edge-router-policies','${p.id}', loadSERP)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- Auth Policies ----
let authPolicies = [];
async function loadAuthPolicies() {
  const res = await api('/api/auth-policies');
  authPolicies = res.data || [];
  const el = document.getElementById('nav-auth');
  if (el) el.textContent = authPolicies.length;
  renderAuthPolicies();
}
function renderAuthPolicies() {
  const tbody = document.querySelector('#tb-auth-policies');
  if (!tbody) return;
  tbody.innerHTML = '';
  authPolicies.forEach(p => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${p.name}</td>
      <td>${p.primary?.cert?.allowed ? 'cert' : p.primary?.extJwt?.allowed ? 'extJwt' : p.primary?.updb?.allowed ? 'updb' : '-'}</td>
      <td>${p.secondary?.requireTotp ? 'totp' : '-'}</td>
      <td class="id-cell">${p.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/auth-policies','${p.id}', loadAuthPolicies)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- Posture Checks ----
let postureChecks = [];
async function loadPostureChecks() {
  const res = await api('/api/posture-checks');
  postureChecks = res.data || [];
  const el = document.getElementById('nav-posture');
  if (el) el.textContent = postureChecks.length;
  renderPostureChecks();
}
function renderPostureChecks() {
  const tbody = document.querySelector('#tb-posture-checks');
  if (!tbody) return;
  tbody.innerHTML = '';
  postureChecks.forEach(p => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${p.name}</td>
      <td><span class="tag tag-purple">${p.typeId||p.type?.name||'unknown'}</span></td>
      <td class="id-cell">${p.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/posture-checks','${p.id}', loadPostureChecks)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- Cert Authorities ----
let certAuthorities = [];
async function loadCertAuthorities() {
  const res = await api('/api/cert-authorities');
  certAuthorities = res.data || [];
  const el = document.getElementById('nav-ca');
  if (el) el.textContent = certAuthorities.length;
  renderCertAuthorities();
}
function renderCertAuthorities() {
  const tbody = document.querySelector('#tb-cas');
  if (!tbody) return;
  tbody.innerHTML = '';
  certAuthorities.forEach(ca => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${ca.name}</td>
      <td class="id-cell" style="font-size:0.6rem">${ca.fingerprint||'-'}</td>
      <td>${ca.isAutoCaEnrollmentEnabled ? '<span class="dot dot-green"></span>yes' : 'no'}</td>
      <td class="id-cell">${ca.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/cert-authorities','${ca.id}', loadCertAuthorities)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- External JWT Signers ----
let extJwtSigners = [];
async function loadExtJwtSigners() {
  const res = await api('/api/ext-jwt-signers');
  extJwtSigners = res.data || [];
  const el = document.getElementById('nav-jwt');
  if (el) el.textContent = extJwtSigners.length;
  renderExtJwtSigners();
}
function renderExtJwtSigners() {
  const tbody = document.querySelector('#tb-jwt-signers');
  if (!tbody) return;
  tbody.innerHTML = '';
  extJwtSigners.forEach(s => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${s.name}</td>
      <td>${s.issuer||'-'}</td>
      <td>${s.audience||'-'}</td>
      <td>${s.enabled ? '<span class="dot dot-green"></span>yes' : '<span class="dot dot-red"></span>no'}</td>
      <td class="id-cell">${s.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/ext-jwt-signers','${s.id}', loadExtJwtSigners)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- API Sessions ----
let apiSessionsData = [];
async function loadApiSessions() {
  const res = await api('/api/api-sessions');
  apiSessionsData = res.data || [];
  const el = document.getElementById('nav-api-sess');
  if (el) el.textContent = apiSessionsData.length;
  renderApiSessions();
}
function renderApiSessions() {
  const filter = document.getElementById('api-sess-filter')?.value?.toLowerCase() || '';
  const tbody = document.querySelector('#tb-api-sessions');
  if (!tbody) return;
  tbody.innerHTML = '';
  apiSessionsData
    .filter(s => !filter || (s.identity?.name||'').toLowerCase().includes(filter))
    .forEach(s => {
      const tr = document.createElement('tr');
      tr.innerHTML = `
        <td>${s.identity?.name||'-'}</td>
        <td class="id-cell">${(s.token||'').substring(0,20)}...</td>
        <td>${s.createdAt ? new Date(s.createdAt).toLocaleString() : '-'}</td>
        <td>${s.lastActivityAt ? new Date(s.lastActivityAt).toLocaleString() : '-'}</td>
        <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem;color:var(--red)" onclick="deleteEntity('/api/api-sessions','${s.id}', loadApiSessions)">kill</button></td>`;
      tbody.appendChild(tr);
    });
}

// ---- Sessions ----
let sessionsData = [];
async function loadSessions() {
  const res = await api('/api/sessions');
  sessionsData = res.data || [];
  const el = document.getElementById('nav-sess');
  if (el) el.textContent = sessionsData.length;
  renderSessions();
}
function renderSessions() {
  const filter = document.getElementById('sess-filter')?.value?.toLowerCase() || '';
  const tbody = document.querySelector('#tb-sessions');
  if (!tbody) return;
  tbody.innerHTML = '';
  sessionsData
    .filter(s => !filter || (s.serviceName||s.service?.name||'').toLowerCase().includes(filter))
    .forEach(s => {
      const tr = document.createElement('tr');
      tr.innerHTML = `
        <td>${s.serviceName||s.service?.name||'-'}</td>
        <td>${s.identity?.name||'-'}</td>
        <td><span class="tag">${s.type||'-'}</span></td>
        <td>${s.createdAt ? new Date(s.createdAt).toLocaleString() : '-'}</td>
        <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem;color:var(--red)" onclick="deleteEntity('/api/sessions','${s.id}', loadSessions)">kill</button></td>`;
      tbody.appendChild(tr);
    });
}

// ---- Generic delete helper ----
async function deleteEntity(apiPath, id, reloadFn) {
  if (!confirm('Delete this entity? This cannot be undone.')) return;
  try {
    const resp = await fetch(apiPath + '/' + id, { method: 'DELETE' });
    if (resp.status === 204 || resp.ok) {
      reloadFn();
    } else {
      const err = await resp.json().catch(() => ({error: 'unknown error'}));
      alert('Delete failed: ' + (err.error || resp.status));
    }
  } catch(e) {
    alert('Delete failed: ' + e.message);
  }
}

// ---- Create modal system ----
let currentCreateType = null;

const createForms = {
  'identity': {
    title: 'Create Identity',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-identity">
      <label class="fb-label">Type</label>
      <select class="fb-input" id="cf-type">
        <option value="Default">Default</option>
        <option value="Router">Router</option>
        <option value="Host">Host</option>
        <option value="User">User</option>
      </select>
      <label class="fb-label">Role Attributes (comma-separated)</label>
      <input class="fb-input" id="cf-roles" placeholder="#role1, #role2">
      <label class="fb-label" style="margin-top:0.75rem;display:flex;align-items:center;gap:0.5rem">
        <input type="checkbox" id="cf-isAdmin"> Admin identity
      </label>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      type: { name: document.getElementById('cf-type').value },
      isAdmin: document.getElementById('cf-isAdmin').checked,
      roleAttributes: document.getElementById('cf-roles').value.split(',').map(s=>s.trim()).filter(Boolean),
      enrollment: { ott: true }
    }),
    endpoint: '/api/identities',
    onSuccess: async (data) => {
      loadAll();
      const jwt = data?.data?.enrollment?.ott?.jwt;
      if (jwt) showJWTModal(jwt);
    }
  },
  'service': {
    title: 'Create Service',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-service">
      <label class="fb-label">Role Attributes (comma-separated)</label>
      <input class="fb-input" id="cf-roles" placeholder="#role1, #role2">
      <label class="fb-label">Terminator Strategy</label>
      <select class="fb-input" id="cf-strategy">
        <option value="smartrouting">smartrouting</option>
        <option value="weighted">weighted</option>
        <option value="random">random</option>
        <option value="ha">ha</option>
      </select>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      roleAttributes: document.getElementById('cf-roles').value.split(',').map(s=>s.trim()).filter(Boolean),
      terminatorStrategy: document.getElementById('cf-strategy').value,
      encryptionRequired: true
    }),
    endpoint: '/api/services',
    onSuccess: () => loadAll()
  },
  'edge-router': {
    title: 'Create Edge Router',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-router">
      <label class="fb-label">Role Attributes (comma-separated)</label>
      <input class="fb-input" id="cf-roles" placeholder="#role1, #role2">
      <label class="fb-label" style="margin-top:0.5rem;display:flex;align-items:center;gap:0.5rem">
        <input type="checkbox" id="cf-tunneler"> Is tunneler enabled
      </label>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      roleAttributes: document.getElementById('cf-roles').value.split(',').map(s=>s.trim()).filter(Boolean),
      isTunnelerEnabled: document.getElementById('cf-tunneler').checked
    }),
    endpoint: '/api/routers',
    onSuccess: () => loadAll()
  },
  'service-policy': {
    title: 'Create Service Policy',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-policy">
      <label class="fb-label">Type</label>
      <select class="fb-input" id="cf-ptype">
        <option value="Dial">Dial</option>
        <option value="Bind">Bind</option>
      </select>
      <label class="fb-label">Semantic</label>
      <select class="fb-input" id="cf-semantic">
        <option value="AnyOf">AnyOf</option>
        <option value="AllOf">AllOf</option>
      </select>
      <label class="fb-label">Identity Roles (comma-separated)</label>
      <input class="fb-input" id="cf-iroles" placeholder="#role1">
      <label class="fb-label">Service Roles (comma-separated)</label>
      <input class="fb-input" id="cf-sroles" placeholder="#role1">`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      type: document.getElementById('cf-ptype').value,
      semantic: document.getElementById('cf-semantic').value,
      identityRoles: document.getElementById('cf-iroles').value.split(',').map(s=>s.trim()).filter(Boolean),
      serviceRoles: document.getElementById('cf-sroles').value.split(',').map(s=>s.trim()).filter(Boolean)
    }),
    endpoint: '/api/policies',
    onSuccess: () => loadAll()
  },
  'edge-router-policy': {
    title: 'Create Edge Router Policy',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-erp">
      <label class="fb-label">Semantic</label>
      <select class="fb-input" id="cf-semantic">
        <option value="AnyOf">AnyOf</option>
        <option value="AllOf">AllOf</option>
      </select>
      <label class="fb-label">Identity Roles (comma-separated)</label>
      <input class="fb-input" id="cf-iroles" placeholder="#role1">
      <label class="fb-label">Edge Router Roles (comma-separated)</label>
      <input class="fb-input" id="cf-rroles" placeholder="#role1">`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      semantic: document.getElementById('cf-semantic').value,
      identityRoles: document.getElementById('cf-iroles').value.split(',').map(s=>s.trim()).filter(Boolean),
      edgeRouterRoles: document.getElementById('cf-rroles').value.split(',').map(s=>s.trim()).filter(Boolean)
    }),
    endpoint: '/api/edge-router-policies',
    onSuccess: () => loadEdgeRouterPolicies()
  },
  'serp': {
    title: 'Create Service Edge Router Policy',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-serp">
      <label class="fb-label">Semantic</label>
      <select class="fb-input" id="cf-semantic">
        <option value="AnyOf">AnyOf</option>
        <option value="AllOf">AllOf</option>
      </select>
      <label class="fb-label">Service Roles (comma-separated)</label>
      <input class="fb-input" id="cf-sroles" placeholder="#role1">
      <label class="fb-label">Edge Router Roles (comma-separated)</label>
      <input class="fb-input" id="cf-rroles" placeholder="#role1">`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      semantic: document.getElementById('cf-semantic').value,
      serviceRoles: document.getElementById('cf-sroles').value.split(',').map(s=>s.trim()).filter(Boolean),
      edgeRouterRoles: document.getElementById('cf-rroles').value.split(',').map(s=>s.trim()).filter(Boolean)
    }),
    endpoint: '/api/service-edge-router-policies',
    onSuccess: () => loadSERP()
  },
  'auth-policy': {
    title: 'Create Auth Policy',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-auth-policy">
      <p style="font-size:0.6875rem;color:#555;margin-top:0.5rem">Creates a basic auth policy. Edit in Ziti CLI for advanced configuration.</p>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      primary: { cert: { allowed: true, allowExpiredCerts: false }, extJwt: { allowed: false, allowedSigners: [] }, updb: { allowed: false, minPasswordLength: 5, requireSpecialChar: false, requireNumberChar: false, requireMixedCase: false, maxAttempts: 5, lockoutDurationMinutes: 0 } },
      secondary: { requireTotp: false, requireExtJwtSigner: null }
    }),
    endpoint: '/api/auth-policies',
    onSuccess: () => loadAuthPolicies()
  },
  'ext-jwt-signer': {
    title: 'Create External JWT Signer',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-signer">
      <label class="fb-label">Issuer *</label>
      <input class="fb-input" id="cf-issuer" placeholder="https://accounts.example.com">
      <label class="fb-label">Audience</label>
      <input class="fb-input" id="cf-audience" placeholder="my-app">
      <label class="fb-label">JWKS Endpoint *</label>
      <input class="fb-input" id="cf-jwks" placeholder="https://accounts.example.com/.well-known/jwks.json">
      <label class="fb-label">Claims Property</label>
      <input class="fb-input" id="cf-claims" placeholder="email">`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      issuer: document.getElementById('cf-issuer').value.trim(),
      audience: document.getElementById('cf-audience').value.trim() || null,
      jwksEndpoint: document.getElementById('cf-jwks').value.trim(),
      claimsProperty: document.getElementById('cf-claims').value.trim() || 'sub',
      enabled: true,
      useExternalId: false
    }),
    endpoint: '/api/ext-jwt-signers',
    onSuccess: () => loadExtJwtSigners()
  },
  'cert-authority': {
    title: 'Create Certificate Authority',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-ca">
      <label class="fb-label">PEM Certificate *</label>
      <textarea class="fb-input" id="cf-pem" style="height:120px;resize:vertical" placeholder="-----BEGIN CERTIFICATE-----"></textarea>
      <label class="fb-label" style="margin-top:0.5rem;display:flex;align-items:center;gap:0.5rem">
        <input type="checkbox" id="cf-autoenroll"> Auto CA enrollment
      </label>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      certPem: document.getElementById('cf-pem').value.trim(),
      isAutoCaEnrollmentEnabled: document.getElementById('cf-autoenroll').checked,
      isOttCaEnrollmentEnabled: true,
      isAuthEnabled: true
    }),
    endpoint: '/api/cert-authorities',
    onSuccess: () => loadCertAuthorities()
  },
  'posture-check': {
    title: 'Create Posture Check',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-posture-check">
      <label class="fb-label">Type</label>
      <select class="fb-input" id="cf-pctype">
        <option value="OS">OS</option>
        <option value="DOMAIN">Domain</option>
        <option value="PROCESS">Process</option>
        <option value="MAC">MAC Address</option>
        <option value="MFA">MFA</option>
      </select>
      <p style="font-size:0.6875rem;color:#555;margin-top:0.5rem">Creates a basic posture check. Edit in Ziti CLI for type-specific config.</p>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      typeId: document.getElementById('cf-pctype').value
    }),
    endpoint: '/api/posture-checks',
    onSuccess: () => loadPostureChecks()
  },
  'transit-router': {
    title: 'Create Transit Router',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-transit-router">
      <label class="fb-label" style="margin-top:0.5rem;display:flex;align-items:center;gap:0.5rem">
        <input type="checkbox" id="cf-notraversal"> No traversal
      </label>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      noTraversal: document.getElementById('cf-notraversal').checked
    }),
    endpoint: '/api/transit-routers',
    onSuccess: () => loadTransitRouters()
  }
};

function showCreateModal(type) {
  const form = createForms[type];
  if (!form) return;
  currentCreateType = type;
  document.getElementById('create-modal-title').textContent = form.title;
  document.getElementById('create-modal-body').innerHTML = form.fields;
  document.getElementById('create-modal').classList.add('open');
}

function closeCreateModal() {
  document.getElementById('create-modal').classList.remove('open');
  currentCreateType = null;
}

async function submitCreateModal() {
  const form = createForms[currentCreateType];
  if (!form) return;
  const btn = document.getElementById('create-modal-submit');
  btn.disabled = true;
  btn.textContent = 'creating...';
  try {
    const payload = form.build();
    const resp = await fetch(form.endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) {
      alert('Create failed: ' + (data.error || JSON.stringify(data).substring(0, 200)));
      return;
    }
    closeCreateModal();
    if (form.onSuccess) await form.onSuccess(data);
  } catch(e) {
    alert('Create failed: ' + e.message);
  } finally {
    btn.disabled = false;
    btn.textContent = 'create';
  }
}

function showJWTModal(jwt) {
  document.getElementById('jwt-display').value = jwt;
  document.getElementById('jwt-modal').classList.add('open');
}

function copyJWT() {
  const ta = document.getElementById('jwt-display');
  navigator.clipboard.writeText(ta.value).then(() => {
    const btn = ta.nextElementSibling;
    btn.textContent = 'copied!';
    setTimeout(() => btn.textContent = 'copy to clipboard', 2000);
  });
}

// ---- Machine Detail Modal ----
let machineModalID = null;
let machineModalSSEHandler = null;

async function openMachineModal(machineID) {
  machineModalID = machineID;
  const modal = document.getElementById('machine-modal');
  if (!modal) return;
  modal.classList.add('open');

  // Reset UI
  document.getElementById('mm-nickname').textContent = 'Loading...';
  document.getElementById('mm-hostname').textContent = '';
  document.getElementById('mm-overview').innerHTML = '';
  document.getElementById('mm-hardware').innerHTML = '';
  document.getElementById('mm-network').innerHTML = '';
  document.getElementById('mm-live-empty').style.display = 'block';
  document.getElementById('mm-live-stats').style.display = 'none';
  document.getElementById('mm-online-dot').className = 'dot dot-yellow';

  // Fetch enrollment snapshot
  try {
    const resp = await fetch(`/api/v1/identities/${machineID}/snapshot`);
    if (resp.ok) {
      const snap = await resp.json();
      renderMachineSnapshot(snap);
    } else {
      document.getElementById('mm-nickname').textContent = machineID;
      document.getElementById('mm-hostname').textContent = 'No snapshot available — enrolled before v2.0';
    }
  } catch(e) {
    document.getElementById('mm-nickname').textContent = 'Error loading snapshot';
  }

  // Fetch latest telemetry snapshot for immediate display
  try {
    const tel = await fetch(`/api/v1/identities/${machineID}/telemetry`).then(r => r.json());
    if (tel && tel.frames && Object.keys(tel.frames).length > 0) {
      updateMachineLive(tel.frames);
    }
  } catch(_) {}

  // Subscribe to SSE stream for live updates while modal is open
  if (machineModalSSEHandler && telemetrySource) {
    telemetrySource.removeEventListener('message', machineModalSSEHandler);
  }
  machineModalSSEHandler = (e) => {
    try {
      const frame = JSON.parse(e.data);
      if (!frame.machine_id || frame.machine_id !== machineID) return;
      updateMachineLive({ [frame.type]: frame.payload });
    } catch(_) {}
  };
  if (telemetrySource) {
    telemetrySource.addEventListener('message', machineModalSSEHandler);
  }
}

function closeMachineModal() {
  const modal = document.getElementById('machine-modal');
  if (modal) modal.classList.remove('open');
  if (machineModalSSEHandler && telemetrySource) {
    telemetrySource.removeEventListener('message', machineModalSSEHandler);
  }
  machineModalSSEHandler = null;
  machineModalID = null;
}

function renderMachineSnapshot(snap) {
  const os = snap.os || {};
  const hw = snap.hardware || {};
  const dec = snap.decision || {};
  const stageNames = ['edge', 'member', 'contributor', 'admin'];
  const stage = dec.stage !== undefined ? dec.stage : '?';

  // Header
  document.getElementById('mm-nickname').textContent = snap.machine_id || os.hostname || 'Unknown';
  document.getElementById('mm-hostname').textContent = os.hostname || '';
  document.getElementById('mm-stage-badge').textContent = stageNames[stage] || `stage-${stage}`;
  document.getElementById('mm-class-badge').textContent = snap.attestation_class || 'unattested';
  document.getElementById('mm-online-dot').className = 'dot dot-yellow';

  // Overview
  const overviewEl = document.getElementById('mm-overview');
  const overviewFields = [
    ['Machine ID', snap.machine_id],
    ['Ziti ID', snap.ziti_identity_id],
    ['Enrolled', snap.enrolled_at ? new Date(snap.enrolled_at).toLocaleString() : null],
    ['Method', snap.enrolled_by],
    ['Node', snap.node],
    ['Stage', dec.approved ? `approved (${stageNames[stage] || stage})` : dec.quarantine ? 'quarantine' : null],
    ['Reason', dec.reason],
    ['Zitadel', snap.zitadel_sub || 'unclaimed'],
  ];
  overviewEl.innerHTML = overviewFields
    .filter(([,v]) => v)
    .map(([k, v]) => `<div class="detail-row"><span class="detail-label">${k}</span><span class="detail-val">${v}</span></div>`)
    .join('');

  // Hardware
  const hwEl = document.getElementById('mm-hardware');
  const hwFields = [
    ['CPU', hw.cpu_model],
    ['Cores', hw.cpu_cores],
    ['Memory', hw.memory_mb ? `${(hw.memory_mb/1024).toFixed(1)} GB` : null],
    ['Product', hw.dmi_product_name],
    ['Serial', hw.dmi_product_serial || hw.serial_number],
    ['Disk Serials', (hw.disk_serials || []).join(', ') || null],
    ['MACs', (hw.macs || []).join(', ') || null],
    ['TPM', hw.tpm_present !== undefined ? (hw.tpm_present ? `v${hw.tpm_version || '2'}` : 'absent') : null],
    ['BIOS', hw.dmi_bios_version],
    ['HW Hash', hw.hardware_hash ? hw.hardware_hash.substring(0, 16) : null],
  ];
  hwEl.innerHTML = hwFields
    .filter(([,v]) => v)
    .map(([k, v]) => `<div class="detail-row"><span class="detail-label">${k}</span><span class="detail-val">${v}</span></div>`)
    .join('');

  // Network
  const net = snap.network || {};
  const conn = snap.connection || {};
  const geo = conn.geolocation || {};
  const netEl = document.getElementById('mm-network');
  const netFields = [
    ['Public IP', conn.public_ip || conn.source_ip],
    ['Location', [geo.city, geo.region, geo.country].filter(Boolean).join(', ') || null],
    ['ASN', geo.asn ? `${geo.asn}${geo.org ? ' — ' + geo.org : ''}` : null],
    ['Gateway', net.gateway],
    ['DNS', (net.dns_servers || []).join(', ') || null],
    ['JA4', conn.ja4],
    ['TLS', conn.tls_version],
  ];
  netEl.innerHTML = netFields
    .filter(([,v]) => v)
    .map(([k, v]) => `<div class="detail-row"><span class="detail-label">${k}</span><span class="detail-val" style="font-family:var(--mono);font-size:0.6875rem">${v}</span></div>`)
    .join('');

  // Interfaces table
  const ifaces = net.interfaces || [];
  if (ifaces.length > 0) {
    netEl.innerHTML += `<div style="grid-column:1/-1;margin-top:0.5rem">
      <table style="width:100%;font-size:0.6875rem">
        <thead><tr><th>Interface</th><th>MAC</th><th>IPs</th></tr></thead>
        <tbody>${ifaces.map(i => `<tr>
          <td>${i.name || ''}</td>
          <td class="id-cell">${i.mac || i.MAC || '-'}</td>
          <td>${(i.ips || i.addrs || i.Addresses || []).map(a => typeof a === 'string' ? a : a.Addr || '').join(', ') || '-'}</td>
        </tr>`).join('')}</tbody>
      </table>
    </div>`;
  }
}

function updateMachineLive(frames) {
  if (!frames || Object.keys(frames).length === 0) return;

  const emptyEl = document.getElementById('mm-live-empty');
  const statsEl = document.getElementById('mm-live-stats');
  if (!emptyEl || !statsEl) return;

  emptyEl.style.display = 'none';
  statsEl.style.display = 'grid';

  const sys = frames['system'];
  if (sys) {
    const cpuPct = sys.cpu_percent || 0;
    const memPct = sys.mem_percent || 0;
    const cpuBar = document.getElementById('mm-cpu-bar');
    const cpuVal = document.getElementById('mm-cpu-val');
    const memBar = document.getElementById('mm-mem-bar');
    const memVal = document.getElementById('mm-mem-val');
    if (cpuBar) cpuBar.style.width = Math.min(100, cpuPct).toFixed(1) + '%';
    if (cpuVal) cpuVal.textContent = `${cpuPct.toFixed(1)}%`;
    if (memBar) memBar.style.width = Math.min(100, memPct).toFixed(1) + '%';
    if (memVal) {
      const usedGB = sys.mem_used ? (sys.mem_used / 1073741824).toFixed(1) : '?';
      const totalGB = sys.mem_total ? (sys.mem_total / 1073741824).toFixed(1) : '?';
      memVal.textContent = `${usedGB} / ${totalGB} GB`;
    }
    const dot = document.getElementById('mm-online-dot');
    if (dot) dot.className = 'dot dot-green';
  }

  const disk = frames['disk'];
  if (disk && disk.mounts) {
    const diskEl = document.getElementById('mm-disk-stats');
    if (diskEl) {
      diskEl.innerHTML = disk.mounts.slice(0, 4).map(m => `
        <div class="stat-card" style="margin-bottom:0.5rem">
          <div class="label">${m.path}</div>
          <div style="background:#161616;border-radius:3px;height:6px;margin-top:0.25rem">
            <div style="height:6px;border-radius:3px;width:${Math.min(100,m.used_percent).toFixed(1)}%;background:${m.used_percent > 85 ? 'var(--red)' : 'var(--yellow)'}"></div>
          </div>
          <div class="sub" style="margin-top:0.25rem">${m.used_percent.toFixed(1)}% of ${(m.total/1073741824).toFixed(0)}GB</div>
        </div>`).join('');
    }
  }

  const ageEl = document.getElementById('mm-live-age');
  if (ageEl) ageEl.textContent = `updated ${new Date().toLocaleTimeString()}`;
}
