#!/usr/bin/env node

/**
 * P3M Critical Path Demo Suite Seeder
 *
 * Seeds a deterministic portfolio/programme/project structure whose linked
 * workspaces contain dated work items and "blocks" dependencies, so the CPM
 * endpoints (GET /api/p3m/projects/{id}/critical-path and
 * GET /api/p3m/programmes/{id}/critical-path) return known critical paths:
 *
 *   Alpha  – linear chain of 4 tasks; every task is critical. Span 12d.
 *   Beta   – parallel branches merging at handover; the long branch (core
 *            platform) is critical, docs carries 3 days of float. Span 9d.
 *   Gamma  – three independent tasks; only "Analysis" reaches the project
 *            end and is critical. Span 7d.
 *   Programme – all three projects as one network plus a cross-project edge
 *            (Beta handover blocks Gamma analysis); the critical path runs
 *            Beta Initiate → Core Platform → Handover → Gamma Analysis.
 *            Span 14d.
 *
 * The script is idempotent: existing entities are matched by title/key and
 * reused, missing pieces are created, and every run finishes by asserting the
 * expected CPM results against the live API (exit code 0 on success).
 *
 * Usage:
 *   node seed-p3m-suite.js [options]
 *
 * Options:
 *   --base-url <url>       Base URL of a running server (default: http://localhost:8080)
 *   --admin-user <user>    Login username (default: admin)
 *   --admin-password <pw>  Login password (default: admin)
 *   --help                 Show this help message
 */

import * as http from 'http';
import * as https from 'https';

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

const args = process.argv.slice(2);

function argValue(flag, fallback) {
  const i = args.indexOf(flag);
  if (i === -1 || i + 1 >= args.length) return fallback;
  return args[i + 1];
}

if (args.includes('--help') || args.includes('-h')) {
  console.log(`Usage: node seed-p3m-suite.js [--base-url <url>] [--admin-user <user>] [--admin-password <pw>]`);
  process.exit(0);
}

const BASE_URL = argValue('--base-url', 'http://localhost:8080').replace(/\/$/, '');
const ADMIN_USER = argValue('--admin-user', 'admin');
const ADMIN_PASSWORD = argValue('--admin-password', 'admin');

// ---------------------------------------------------------------------------
// Suite definition (all dates in December 2026, UTC)
// ---------------------------------------------------------------------------

const PORTFOLIO_TITLE = 'CPM Demo Portfolio';
const PROGRAMME_TITLE = 'Delivery Programme';
const PLAN_NAME = 'CPM Demo Plan';

/** Each project owns one workspace; edges reference task indices. */
const PROJECTS = [
  {
    key: 'alpha',
    title: 'Alpha',
    wsKey: 'CPMA',
    wsName: 'CPM Alpha',
    tasks: [
      { title: 'Alpha – Kickoff', start: '2026-12-01', end: '2026-12-03' },
      { title: 'Alpha – Design', start: '2026-12-04', end: '2026-12-06' },
      { title: 'Alpha – Build', start: '2026-12-07', end: '2026-12-09' },
      { title: 'Alpha – Launch', start: '2026-12-10', end: '2026-12-12' }
    ],
    edges: [
      [0, 1],
      [1, 2],
      [2, 3]
    ]
  },
  {
    key: 'beta',
    title: 'Beta',
    wsKey: 'CPMB',
    wsName: 'CPM Beta',
    tasks: [
      { title: 'Beta – Initiate', start: '2026-12-01', end: '2026-12-02' },
      { title: 'Beta – Core Platform', start: '2026-12-03', end: '2026-12-07' },
      { title: 'Beta – Docs', start: '2026-12-03', end: '2026-12-04' },
      { title: 'Beta – Handover', start: '2026-12-08', end: '2026-12-09' }
    ],
    edges: [
      [0, 1],
      [0, 2],
      [1, 3],
      [2, 3]
    ]
  },
  {
    key: 'gamma',
    title: 'Gamma',
    wsKey: 'CPMD',
    wsName: 'CPM Gamma',
    tasks: [
      { title: 'Gamma – Survey', start: '2026-12-01', end: '2026-12-05' },
      { title: 'Gamma – Analysis', start: '2026-12-03', end: '2026-12-07' },
      { title: 'Gamma – Report', start: '2026-12-02', end: '2026-12-02' }
    ],
    edges: []
  }
];

/** Cross-project "blocks" edges: [sourceProjectKey, sourceTaskIdx] -> [targetProjectKey, targetTaskIdx]. */
const CROSS_EDGES = [
  { from: ['beta', 3], to: ['gamma', 1] } // Beta – Handover blocks Gamma – Analysis
];

/** Expected CPM results used by the verification step. */
const EXPECTED = {
  alpha: {
    item_count: 4,
    dependency_count: 3,
    start: '2026-12-01',
    end: '2026-12-12',
    span: 12,
    critical: ['Alpha – Kickoff', 'Alpha – Design', 'Alpha – Build', 'Alpha – Launch'],
    floats: {},
    registers: {
      'Alpha – Build': { open_risks: 1 },
      'Alpha – Design': { pending_decisions: 1 }
    }
  },
  beta: {
    item_count: 4,
    dependency_count: 4,
    start: '2026-12-01',
    end: '2026-12-09',
    span: 9,
    critical: ['Beta – Initiate', 'Beta – Core Platform', 'Beta – Handover'],
    floats: { 'Beta – Docs': 3 },
    registers: {
      'Beta – Core Platform': { open_risks: 1 },
      'Beta – Handover': { pending_decisions: 1 }
    }
  },
  gamma: {
    item_count: 3,
    dependency_count: 0,
    start: '2026-12-01',
    end: '2026-12-07',
    span: 7,
    critical: ['Gamma – Analysis'],
    floats: { 'Gamma – Survey': 2, 'Gamma – Report': 5 },
    registers: {
      'Gamma – Survey': { open_risks: 1 },
      'Gamma – Report': { pending_decisions: 1 }
    }
  },
  programme: {
    item_count: 11,
    dependency_count: 8,
    start: '2026-12-01',
    end: '2026-12-14',
    span: 14,
    criticalSet: ['Beta – Initiate', 'Beta – Core Platform', 'Beta – Handover', 'Gamma – Analysis']
  }
};

/** RAID register entries per project; task indices the risk/decision link to. */
const REGISTERS = {
  alpha: { riskTaskIdx: 2, decisionTaskIdx: 1 },
  beta: { riskTaskIdx: 1, decisionTaskIdx: 3 },
  gamma: { riskTaskIdx: 0, decisionTaskIdx: 2 }
};

/** Active leave window for the demo user (inclusive on both ends). */
const LEAVE = { start: '2026-12-05', end: '2026-12-11' };
const RESOURCE_NAME = 'CPM Demo Resource';
const ALLOCATION_START = '2026-12-05';
const ALLOCATION_END = '2026-12-12'; // half-open [start, end) → Dec 5–11

/** Expected leave impact on Alpha: 7 overlap days at 40 h/week. */
const EXPECTED_LEAVE_IMPACT = {
  overlap_days: 7,
  lost_hours: 40,
  covered_by_substitute: false,
  taskOverlapDays: { 'Alpha – Build': 3 } // Dec 7–9 inside the leave window
};

/** Architecture review board demo (project Alpha). */
const ARCH_PAGE_TITLE = 'Alpha – Architecture (demo)';
const ARCH_REVIEW_TITLE = 'Alpha – Platform architecture sign-off';

// ---------------------------------------------------------------------------
// HTTP helpers (same conventions as generate-demo.js)
// ---------------------------------------------------------------------------

const colors = { reset: '\x1b[0m', green: '\x1b[32m', red: '\x1b[31m', cyan: '\x1b[36m', dim: '\x1b[2m' };

function log(message) {
  console.log(`${colors.reset}${message}`);
}

function logSection(title) {
  console.log(`\n${colors.cyan}=== ${title} ===${colors.reset}`);
}

function logSuccess(message) {
  log(`${colors.green}✓${colors.reset} ${message}`);
}

function logError(message) {
  log(`${colors.red}✗${colors.reset} ${message}`);
}

function logInfo(message) {
  log(`${colors.dim}${message}${colors.reset}`);
}

function makeRequest(method, url, data = null, headers = {}) {
  return new Promise((resolve, reject) => {
    const urlObj = new URL(url);
    const isHttps = urlObj.protocol === 'https:';
    const options = {
      hostname: urlObj.hostname,
      port: urlObj.port || (isHttps ? 443 : 80),
      path: urlObj.pathname + urlObj.search,
      method,
      headers: { 'Content-Type': 'application/json', ...headers }
    };

    const httpModule = isHttps ? https : http;
    const req = httpModule.request(options, (res) => {
      let body = '';
      res.on('data', (chunk) => (body += chunk));
      res.on('end', () => {
        try {
          resolve({ status: res.statusCode, data: body ? JSON.parse(body) : null, cookies: res.headers['set-cookie'] || [] });
        } catch (_) {
          resolve({ status: res.statusCode, data: body, cookies: res.headers['set-cookie'] || [] });
        }
      });
    });

    req.on('error', reject);
    if (data) req.write(JSON.stringify(data));
    req.end();
  });
}

async function login() {
  const response = await makeRequest('POST', `${BASE_URL}/api/auth/login`, {
    email_or_username: ADMIN_USER,
    password: ADMIN_PASSWORD
  });
  if (response.status !== 200) {
    throw new Error(`Login failed: ${response.status} - ${JSON.stringify(response.data)?.slice(0, 300)}`);
  }
  const cookie = (response.cookies || []).find((c) => c.includes('session'));
  if (!cookie) throw new Error('No session cookie received from login');
  return cookie.split(';')[0];
}

/** Authenticated request against a legacy /api endpoint; returns the JSON body. */
async function api(session, method, endpoint, data) {
  const response = await makeRequest(method, `${BASE_URL}${endpoint}`, data, {
    Cookie: session,
    'Sec-Fetch-Site': 'same-origin'
  });
  if (response.status < 200 || response.status >= 300) {
    const detail =
      typeof response.data === 'object' && response.data !== null
        ? response.data.error || JSON.stringify(response.data).slice(0, 300)
        : String(response.data).slice(0, 300);
    throw new Error(`${method} ${endpoint} failed: ${response.status} - ${detail}`);
  }
  return response.data;
}

/** Authenticated v2 request returning the unwrapped `data` payload. */
async function v2(session, method, endpoint, data, extraHeaders = {}) {
  const response = await makeRequest(method, `${BASE_URL}${endpoint}`, data, {
    Cookie: session,
    'Sec-Fetch-Site': 'same-origin',
    ...extraHeaders
  });
  if (response.status < 200 || response.status >= 300) {
    const err = response.data?.error;
    const detail = err?.message || response.data?.message || JSON.stringify(response.data)?.slice(0, 300);
    throw new Error(`${method} ${endpoint} failed: ${response.status} - ${detail}`);
  }
  const body = response.data;
  return body && typeof body === 'object' && 'data' in body ? body.data : body;
}

const toISODate = (dateStr) => `${dateStr}T00:00:00Z`;
const dayPart = (iso) => String(iso).slice(0, 10);

/** GET /api/p3m/hierarchy returns {portfolios: [...]}; unwrap to the array. */
async function getHierarchy(session) {
  const data = await api(session, 'GET', '/api/p3m/hierarchy');
  return Array.isArray(data?.portfolios) ? data.portfolios : [];
}

// ---------------------------------------------------------------------------
// Seeding steps
// ---------------------------------------------------------------------------

async function ensureWorkspaces(session) {
  logSection('Workspaces');
  const existing = await v2(session, 'GET', '/api/v2/workspaces?page_size=1000').catch(() => []);
  const byKey = new Map((Array.isArray(existing) ? existing : []).map((w) => [w.key, w]));

  for (const project of PROJECTS) {
    const found = byKey.get(project.wsKey);
    if (found) {
      logInfo(`Workspace already exists: ${project.wsName} (${project.wsKey}, id: ${found.id})`);
      project.workspaceId = found.id;
      continue;
    }
    const created = await v2(session, 'POST', '/api/v2/workspaces', {
      name: project.wsName,
      key: project.wsKey,
      description: `Dated work items for the ${project.title} CPM demo project`
    });
    logSuccess(`Created workspace: ${project.wsName} (${project.wsKey}, id: ${created.id})`);
    project.workspaceId = created.id;
  }
}

async function ensureP3MHierarchy(session) {
  logSection('P3M hierarchy');
  const portfolios = await getHierarchy(session);
  let portfolio = portfolios.find((p) => p.title === PORTFOLIO_TITLE);

  if (!portfolio) {
    const created = await api(session, 'POST', '/api/p3m/portfolios', {
      title: PORTFOLIO_TITLE,
      description: 'Demo portfolio for the critical path (CPM) suite',
      strategic_objective: 'Demonstrate CPM schedule analysis across projects',
      budget_allocated: 1000000,
      rag_status: 'green'
    });
    logSuccess(`Created portfolio: ${PORTFOLIO_TITLE} (id: ${created.id})`);
    const refreshed = await getHierarchy(session);
    portfolio = refreshed.find((p) => p.title === PORTFOLIO_TITLE);
  } else {
    logInfo(`Portfolio already exists: ${PORTFOLIO_TITLE} (id: ${portfolio.id})`);
  }

  let programme = (portfolio.programmes || []).find((pg) => pg.title === PROGRAMME_TITLE);
  if (!programme) {
    const created = await api(session, 'POST', '/api/p3m/programmes', {
      portfolio_id: portfolio.id,
      title: PROGRAMME_TITLE,
      description: 'Demo programme containing the Alpha, Beta and Gamma CPM projects',
      budget_allocated: 900000,
      rag_status: 'green'
    });
    logSuccess(`Created programme: ${PROGRAMME_TITLE} (id: ${created.id})`);
    const refreshed = await getHierarchy(session);
    portfolio = refreshed.find((p) => p.title === PORTFOLIO_TITLE);
    programme = (portfolio.programmes || []).find((pg) => pg.title === PROGRAMME_TITLE);
  } else {
    logInfo(`Programme already exists: ${PROGRAMME_TITLE} (id: ${programme.id})`);
  }

  for (const project of PROJECTS) {
    let p3mProject = (programme.projects || []).find((p) => p.title === project.title);
    if (!p3mProject) {
      const created = await api(session, 'POST', '/api/p3m/projects', {
        portfolio_id: portfolio.id,
        programme_id: programme.id,
        title: project.title,
        description: `CPM demo project ${project.key}`,
        current_stage: 'execution',
        budget_approved: 300000,
        start_date: toISODate('2026-12-01'),
        target_completion_date: toISODate('2026-12-31')
      });
      logSuccess(`Created project: ${project.title} (id: ${created.id})`);
      const refreshed = await getHierarchy(session);
      portfolio = refreshed.find((p) => p.title === PORTFOLIO_TITLE);
      programme = (portfolio.programmes || []).find((pg) => pg.title === PROGRAMME_TITLE);
      p3mProject = (programme.projects || []).find((p) => p.title === project.title);
    } else {
      logInfo(`Project already exists: ${project.title} (id: ${p3mProject.id})`);
    }
    project.p3mId = p3mProject.id;

    // Link the workspace to the P3M project (idempotent set).
    await api(session, 'PUT', `/api/p3m/projects/${project.p3mId}/workspaces`, {
      workspace_ids: [project.workspaceId]
    });
  }
}

async function ensureItems(session) {
  logSection('Work items');
  for (const project of PROJECTS) {
    const existing = await v2(
      session,
      'GET',
      `/api/v2/items?workspace_id=${project.workspaceId}&page_size=1000`
    ).catch(() => []);
    const byTitle = new Map((Array.isArray(existing) ? existing : []).map((i) => [i.title, i]));

    for (const task of project.tasks) {
      const found = byTitle.get(task.title);
      if (found) {
        logInfo(`Item already exists: ${task.title} (id: ${found.id})`);
        task.itemId = found.id;
        continue;
      }
      const created = await v2(session, 'POST', '/api/v2/items', {
        workspace_id: project.workspaceId,
        title: task.title,
        description: `CPM demo task in ${project.wsKey}`,
        start_date: toISODate(task.start),
        end_date: toISODate(task.end)
      });
      logSuccess(`Created item: ${task.title} (id: ${created.id})`);
      task.itemId = created.id;
    }
  }
}

async function ensurePlan(session) {
  logSection('Plan');
  const plans = await api(session, 'GET', '/api/plans?page_size=1000').catch(() => []);
  let plan = (Array.isArray(plans) ? plans : []).find((p) => p.name === PLAN_NAME);

  if (!plan) {
    plan = await api(session, 'POST', '/api/plans', { name: PLAN_NAME, description: 'Plan hosting the CPM demo dependencies' });
    logSuccess(`Created plan: ${PLAN_NAME} (id: ${plan.id})`);
  } else {
    logInfo(`Plan already exists: ${PLAN_NAME} (id: ${plan.id})`);
  }

  for (const project of PROJECTS) {
    try {
      await api(session, 'POST', `/api/plans/${plan.id}/workspaces/${project.workspaceId}`);
      logSuccess(`Linked workspace ${project.wsKey} to plan`);
    } catch (error) {
      if (/already|exists/i.test(error.message)) logInfo(`Workspace ${project.wsKey} already linked to plan`);
      else throw error;
    }
  }
  return plan.id;
}

async function ensureDependencies(session, planId) {
  logSection('Dependencies');
  const byKey = Object.fromEntries(PROJECTS.map((p) => [p.key, p]));

  const allEdges = [];
  for (const project of PROJECTS) {
    for (const [s, d] of project.edges) allEdges.push([project.tasks[s], project.tasks[d]]);
  }
  for (const cross of CROSS_EDGES) {
    allEdges.push([byKey[cross.from[0]].tasks[cross.from[1]], byKey[cross.to[0]].tasks[cross.to[1]]]);
  }

  for (const [src, dst] of allEdges) {
    try {
      await api(session, 'POST', `/api/plans/${planId}/items/${src.itemId}/dependencies`, {
        target_item_id: dst.itemId,
        dep_type: 'blocks'
      });
      logSuccess(`Dependency: ${src.title} → ${dst.title}`);
    } catch (error) {
      if (/already exists/i.test(error.message)) logInfo(`Dependency already exists: ${src.title} → ${dst.title}`);
      else throw error;
    }
  }
}

/** Create a RAID item scoped to an entity, idempotent on title + item_type. */
async function ensureRaidItem(session, entityType, entityID, itemType, title, description, itemID, ownerID) {
  const existing = await api(
    session,
    'GET',
    `/api/p3m/raid-items?entity_type=${encodeURIComponent(entityType)}&entity_id=${entityID}`
  );
  const found = (Array.isArray(existing) ? existing : []).find((i) => i.title === title && i.item_type === itemType);
  if (found) {
    logInfo(`RAID ${itemType} already exists: ${title} (id: ${found.id})`);
    return found;
  }
  const created = await api(session, 'POST', '/api/p3m/raid-items', {
    entity_type: entityType,
    entity_id: entityID,
    item_type: itemType,
    title,
    description,
    impact_score: 3,
    probability_score: 2,
    status: 'open',
    owner_id: ownerID,
    item_id: itemID
  });
  logSuccess(`Created RAID ${itemType}: ${title} (id: ${created.id})`);
  return created;
}

async function ensureRegisters(session) {
  logSection('Risk & decision registers');
  const me = await api(session, 'GET', '/api/auth/me');
  const ownerID = me?.user?.id ?? me?.id;
  for (const project of PROJECTS) {
    const reg = REGISTERS[project.key];
    await ensureRaidItem(
      session, 'project', project.p3mId, 'risk',
      `${project.title} – Demo schedule risk`,
      `Seeded by the CPM demo suite; linked to ${project.tasks[reg.riskTaskIdx].title}`,
      project.tasks[reg.riskTaskIdx].itemId, ownerID
    );
    await ensureRaidItem(
      session, 'project', project.p3mId, 'decision',
      `${project.title} – Pending platform decision`,
      `Seeded by the CPM demo suite; linked to ${project.tasks[reg.decisionTaskIdx].title}`,
      project.tasks[reg.decisionTaskIdx].itemId, ownerID
    );
  }
}

async function ensureLeaveImpact(session) {
  logSection('Leave impact');
  const me = await api(session, 'GET', '/api/auth/me');
  const adminId = me?.user?.id ?? me?.id;
  const alpha = PROJECTS.find((p) => p.key === 'alpha');

  // Active leave period for the demo user (idempotent on date range).
  let leaves = await api(session, 'GET', `/api/users/${adminId}/leave`).catch(() => []);
  if (!Array.isArray(leaves)) leaves = leaves?.leaves || [];
  const existingLeave = leaves.find(
    (l) => dayPart(l.start_date) === LEAVE.start && dayPart(l.end_date) === LEAVE.end
  );
  if (!existingLeave) {
    await api(session, 'POST', `/api/users/${adminId}/leave`, {
      start_date: LEAVE.start,
      end_date: LEAVE.end,
      reason: 'CPM demo leave'
    });
    logSuccess(`Created leave ${LEAVE.start} → ${LEAVE.end} for user ${adminId}`);
  } else {
    logInfo(`Leave already exists: ${dayPart(existingLeave.start_date)} → ${dayPart(existingLeave.end_date)}`);
  }

  // User-backed resource (idempotent on name).
  let resources = await api(session, 'GET', '/api/p3m/resources').catch(() => []);
  resources = Array.isArray(resources) ? resources : [];
  let resource = resources.find((r) => r.name === RESOURCE_NAME);
  if (!resource) {
    resource = await api(session, 'POST', '/api/p3m/resources', {
      name: RESOURCE_NAME,
      user_id: adminId,
      capacity_hours_per_week: 40
    });
    logSuccess(`Created resource: ${RESOURCE_NAME} (id: ${resource.id})`);
  } else {
    logInfo(`Resource already exists: ${RESOURCE_NAME} (id: ${resource.id})`);
  }

  // Allocation over Alpha's mid-December window (half-open [start, end)).
  let allocations = await api(session, 'GET', `/api/p3m/resource-allocations?project_id=${alpha.p3mId}`).catch(() => []);
  allocations = Array.isArray(allocations) ? allocations : [];
  const existingAlloc = allocations.find(
    (a) => a.resource_id === resource.id && dayPart(a.start_date) === ALLOCATION_START
  );
  if (!existingAlloc) {
    await api(session, 'POST', '/api/p3m/resource-allocations', {
      resource_id: resource.id,
      project_id: alpha.p3mId,
      start_date: toISODate(ALLOCATION_START),
      end_date: toISODate(ALLOCATION_END),
      hours_per_week: 40
    });
    logSuccess(`Allocated ${RESOURCE_NAME} to Alpha ${ALLOCATION_START} → ${ALLOCATION_END}`);
  } else {
    logInfo('Allocation already exists');
  }

  // Assign the demo user to one critical task so CPM task rows carry leave.
  const buildTask = alpha.tasks[REGISTERS.alpha.riskTaskIdx];
  await v2(session, 'PATCH', `/api/v2/items/${buildTask.itemId}`, { assignee_id: adminId }, {
    'Content-Type': 'application/merge-patch+json'
  });
  logSuccess(`Assigned ${buildTask.title} to user ${adminId}`);
}

async function ensureArchitectureReviews(session) {
  logSection('Architecture review board');
  const alpha = PROJECTS.find((p) => p.key === 'alpha');

  // Architecture page in Alpha's workspace (idempotent on title).
  let pages = await v2(session, 'GET', `/api/v2/workspaces/${alpha.workspaceId}/pages`).catch(() => []);
  if (!Array.isArray(pages)) pages = pages?.items || [];
  let page = pages.find((p) => p.title === ARCH_PAGE_TITLE);
  if (!page) {
    page = await v2(session, 'POST', `/api/v2/workspaces/${alpha.workspaceId}/pages`, {
      title: ARCH_PAGE_TITLE,
      content: '# Demo architecture\n\nSeeded by the CPM demo suite for ARB verification.'
    });
    logSuccess(`Created architecture page: ${ARCH_PAGE_TITLE} (id: ${page.id})`);
  } else {
    logInfo(`Architecture page already exists: id ${page.id}`);
  }

  // Review lifecycle: wip → in_review (with linked page) → approved.
  let reviews = await api(session, 'GET', `/api/p3m/architecture-reviews?project_id=${alpha.p3mId}`).catch(() => []);
  reviews = Array.isArray(reviews) ? reviews : [];
  let review = reviews.find((r) => r.title === ARCH_REVIEW_TITLE);
  if (!review) {
    review = await api(session, 'POST', '/api/p3m/architecture-reviews', {
      project_id: alpha.p3mId,
      title: ARCH_REVIEW_TITLE,
      description: 'Demo ARB sign-off for the Alpha platform architecture'
    });
    logSuccess(`Created architecture review (id: ${review.id}, status: ${review.status})`);
  } else {
    logInfo(`Architecture review already exists (id: ${review.id}, status: ${review.status})`);
  }

  if (review.status === 'wip') {
    review = await api(session, 'PUT', `/api/p3m/architecture-reviews/${review.id}`, {
      architecture_page_id: page.id,
      status: 'in_review'
    });
    logSuccess(`Submitted review for ARB (status: ${review.status})`);
  } else if (review.status === 'in_review') {
    review = await api(session, 'POST', `/api/p3m/architecture-reviews/${review.id}/decision`, {
      approved: true,
      comments: 'Demo approval for the CPM suite'
    });
    logSuccess(`Decided review: ${review.status}`);
  } else {
    logInfo(`Review already decided: ${review.status}`);
  }
}

// ---------------------------------------------------------------------------
// Verification
// ---------------------------------------------------------------------------

function assertClose(label, actual, expected) {
  if (Math.abs(Number(actual) - Number(expected)) > 1e-9) {
    throw new Error(`${label}: expected ${expected}, got ${actual}`);
  }
}

function checkCPMResponse(name, data, expected) {
  const problems = [];

  assertClose(`${name}.item_count`, data.item_count, expected.item_count);
  assertClose(`${name}.dependency_count`, data.dependency_count, expected.dependency_count);
  if (dayPart(data.project_start) !== expected.start) problems.push(`project_start: expected ${expected.start}, got ${data.project_start}`);
  if (dayPart(data.project_end) !== expected.end) problems.push(`project_end: expected ${expected.end}, got ${data.project_end}`);
  assertClose(`${name}.critical_path_days`, data.critical_path_days, expected.span);

  const byId = new Map((data.tasks || []).map((t) => [t.item_id, t]));
  const criticalTitles = (data.critical_task_ids || [])
    .map((id) => byId.get(id)?.title)
    .filter(Boolean);

  if (expected.critical) {
    // Exact ordered chain.
    if (JSON.stringify(criticalTitles) !== JSON.stringify(expected.critical)) {
      problems.push(`critical path: expected [${expected.critical.join(', ')}], got [${criticalTitles.join(', ')}]`);
    }
  } else if (expected.criticalSet) {
    const set = new Set(criticalTitles);
    const want = new Set(expected.criticalSet);
    if (set.size !== want.size || [...want].some((t) => !set.has(t))) {
      problems.push(`critical path: expected set [${[...want].join(', ')}], got [${[...set].join(', ')}]`);
    }
  }

  for (const task of data.tasks || []) {
    if (!task.is_critical && criticalTitles.includes(task.title)) problems.push(`${task.title}: listed as critical but is_critical=false`);
  }
  for (const [title, float] of Object.entries(expected.floats ?? {})) {
    const task = (data.tasks || []).find((t) => t.title === title);
    if (!task) {
      problems.push(`float check: task ${title} not found`);
      continue;
    }
    assertClose(`${name}.float(${title})`, task.total_float_days, float);
  }

  for (const [title, reg] of Object.entries(expected.registers ?? {})) {
    const task = (data.tasks || []).find((t) => t.title === title);
    if (!task) {
      problems.push(`register check: task ${title} not found`);
      continue;
    }
    assertClose(`${name}.open_risks(${title})`, task.open_risks, reg.open_risks ?? 0);
    assertClose(`${name}.pending_decisions(${title})`, task.pending_decisions, reg.pending_decisions ?? 0);
  }

  if (problems.length > 0) throw new Error(problems.join('; '));
  logSuccess(
    `${name}: ${data.item_count} items, ${data.dependency_count} deps, ` +
      `${dayPart(data.project_start)} → ${dayPart(data.project_end)}, span ${data.critical_path_days}d, ` +
      `critical: [${criticalTitles.join(' → ')}]`
  );
}

async function verify(session) {
  logSection('Verifying critical paths');
  for (const project of PROJECTS) {
    const data = await api(session, 'GET', `/api/p3m/projects/${project.p3mId}/critical-path`);
    checkCPMResponse(project.title, data, EXPECTED[project.key]);
  }

  // Re-read the hierarchy to get the programme id.
  const portfolios = await getHierarchy(session);
  const portfolio = portfolios.find((p) => p.title === PORTFOLIO_TITLE);
  const programme = (portfolio?.programmes || []).find((pg) => pg.title === PROGRAMME_TITLE);
  if (!programme) throw new Error(`Programme ${PROGRAMME_TITLE} not found during verification`);

  const data = await api(session, 'GET', `/api/p3m/programmes/${programme.id}/critical-path`);
  checkCPMResponse(PROGRAMME_TITLE, data, EXPECTED.programme);

  // Leave impact on Alpha: one resource row (7 overlap days) and the assigned
  // critical task with its own overlap.
  const alpha = PROJECTS.find((p) => p.key === 'alpha');
  const impact = await api(session, 'GET', `/api/p3m/projects/${alpha.p3mId}/leave-impact`);
  if (impact.resources.length !== 1) {
    throw new Error(`Leave impact: expected 1 resource row, got ${impact.resources.length}`);
  }
  const resRow = impact.resources[0];
  assertClose('leave.overlap_days', resRow.overlap_days, EXPECTED_LEAVE_IMPACT.overlap_days);
  assertClose('leave.lost_hours', resRow.lost_hours, EXPECTED_LEAVE_IMPACT.lost_hours);
  if (resRow.covered_by_substitute !== EXPECTED_LEAVE_IMPACT.covered_by_substitute) {
    throw new Error(`Leave impact: covered_by_substitute expected ${EXPECTED_LEAVE_IMPACT.covered_by_substitute}, got ${resRow.covered_by_substitute}`);
  }
  for (const [title, days] of Object.entries(EXPECTED_LEAVE_IMPACT.taskOverlapDays)) {
    const taskRow = (impact.tasks || []).find((t) => t.title === title);
    if (!taskRow) throw new Error(`Leave impact: expected task row for ${title}`);
    assertClose(`leave.task_overlap(${title})`, taskRow.overlap_days, days);
  }
  logSuccess(
    `Leave impact (Alpha): resource overlap ${resRow.overlap_days}d / ${resRow.lost_hours}h lost; ` +
      `${Object.keys(EXPECTED_LEAVE_IMPACT.taskOverlapDays).length} affected task(s)`
  );

  // ARB final state: approved with a decided_at stamp and the linked page.
  const reviews = await api(session, 'GET', `/api/p3m/architecture-reviews?project_id=${alpha.p3mId}`);
  const review = (Array.isArray(reviews) ? reviews : []).find((r) => r.title === ARCH_REVIEW_TITLE);
  if (!review || review.status !== 'approved') {
    throw new Error(`ARB: expected an approved review, got ${review?.status ?? 'none'}`);
  }
  if (!review.decided_at) throw new Error('ARB: decided_at missing on the approved review');
  if (review.architecture_page_id == null) throw new Error('ARB: architecture_page_id missing on the approved review');
  logSuccess(`ARB: "${ARCH_REVIEW_TITLE}" approved at ${dayPart(review.decided_at)}, page id ${review.architecture_page_id}`);
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main() {
  log(`Seeding P3M CPM demo suite against ${BASE_URL}`);
  const session = await login();
  logSuccess('Logged in');

  await ensureWorkspaces(session);
  await ensureP3MHierarchy(session);
  await ensureItems(session);
  const planId = await ensurePlan(session);
  await ensureDependencies(session, planId);
  await ensureRegisters(session);
  await ensureLeaveImpact(session);
  await ensureArchitectureReviews(session);
  await verify(session);

  logSection('Done');
  logSuccess(`P3M CPM demo suite seeded and verified (portfolio "${PORTFOLIO_TITLE}", programme "${PROGRAMME_TITLE}")`);
}

main().catch((error) => {
  logError(error.message || String(error));
  process.exit(1);
});
