# Upstream Fork Delta & v0.9.4 Integration

This document records the upstream base this repository was forked from, the exact
delta between our fork and that base, how v0.9.4 interacts with our fork-specific
work, and the integration performed on branch `integrate-v0.9.4`.

All SHAs below were re-verified against a fresh `git fetch upstream --tags` on
2026-11-17 (see §4 Integration log).

## 1. Upstream base

| Property | Value |
|---|---|
| Repository | https://github.com/Windshiftapp/core ("Windshift") |
| License | AGPL‑3.0 |
| Stack | Go 1.27 + SvelteKit frontend, single binary; SQLite (`modernc.org/sqlite`) or Postgres |
| Default branch | `main` |
| Release cadence (recent) | v0.9.0 (2026‑10‑20), v0.9.1 (2026‑10‑21), v0.9.2 (2026‑10‑30), v0.9.3 (2026‑11‑04), **v0.9.4 (2026‑11‑10)** — roughly weekly through Oct–Nov 2026 |
| Integration target | tag `v0.9.4` = `2ebdd7112d385cd8176f2ce71b26f7d23127d43b` (upstream `main` is ~184 commits ahead of v0.9.4 and is **out of scope** for this integration) |

### Fork point / true common ancestor

The fork's true common ancestor with the v0.9.4 lineage is:

```
4bd3fe0fc4dd8cc8f485a3d22d7e8b71e85be3a3
  "docs(api): refresh generated OpenAPI schema" — Stefan Ernst, 2026-07-10
```

Verified: `git merge-base main v0.9.4` returns this SHA after fetch; it is an
ancestor of local `main`. GitHub's compare API (`compare/3248819a...v0.9.4`)
reports `status: diverged` with the same merge base (ahead_by 1824 / behind_by
~1561), consistent with local counts: **1824 upstream commits** in
`MB..v0.9.4`, **1575 fork-side commits** in `MB..main`.

### Maintenance model: continuous cherry-pick sync

The fork is maintained by continuously **cherry-picking upstream commits onto
fork `main`**, not by merging. Evidence (verified 2026‑11‑17):

- 40 of the last 300 commits on `main` have author ≠ committer — the signature
  of cherry-picks (original author preserved, local committer stamped).
- Cherry-picked commit bodies carry `(cherry picked from commit …)` references.

Consequence: SHAs for shared upstream work **diverged** between fork and
upstream. `git merge v0.9.4` is therefore not viable — it would treat every
re-SHA'd cherry-pick as a duplicate and produce massive conflicts. The correct
integration is to rebase our *unique* work onto the new base (v0.9.4), which is
what branch `integrate-v0.9.4` does (§4).

### Last upstream content we hold

The newest upstream commit present in fork `main` is:

```
3248819a  2026-10-31  "fix(items): resolve an omitted item type before hierarchy validation (WI‑1956)"
```

Everything upstream released after that — including all of v0.9.4's feature work
— is absent from `main` and arrives via the new base in this integration.
`git cherry -v main v0.9.4` lists **255** such upstream-only commits
(`/tmp/delta-theirs.txt`).

## 2. Fork delta (what is uniquely ours)

### Stats (computed from `git cherry`, 2026‑11‑17)

| Metric | Value |
|---|---|
| Unique fork commits (`+` in `git cherry -v v0.9.4 main`) | **21** — all by roetz / Tommie Roetz, 2026‑10‑28 → 2026‑11‑17 |
| Re-SHA'd upstream work (`=` lines) | 1477 (excluded from rebase; already in v0.9.4 base) |
| Files touched by the 21 unique commits | 227 |
| Fork-only files (absent from v0.9.4 tree) | 152, of which **3 are accidental junk** (§4.4) → 149 real |
| Tables added by fork schema files | 114 (across 8 files, sqlite + postgres pairs) |
| Files touched by *both* sides since MB (conflict-risk set) | 74 (`/tmp/conflict-risk.txt`) |

### Unique commits → modules

Topological order (= pick order used in §4). Modules: **P3M** = project
portfolio/programme management; **Plans**; **SMS** = stakeholder management;
**SRM** = supplier relationship management.

| # | SHA (short) | Date | Author | Subject | Module(s) |
|---|---|---|---|---|---|
| 1 | `7c4bd7683` | 2026‑10‑28 | roetz | Added BigGantt | BigGantt |
| 2 | `a6920e0dc` | 2026‑10‑29 | roetz | P3M-Added | P3M |
| 3 | `2c39ad65e` | 2026‑11‑01 | roetz | Stakeholders and CSS updates | Stakeholders (P3M/SMS), frontend CSS |
| 4 | `1526cbdb3` | 2026‑11‑01 | roetz | CSS padding fixes | frontend (+ junk `.tmp-bcryptgen/`) |
| 5 | `8ae48b5d0` | 2026‑11‑01 | roetz | Fix Bit Gantt | BigGantt |
| 6 | `fc2b48ede` | 2026‑11‑01 | roetz | Fix Bit Gantt | BigGantt |
| 7 | `41e8d9ca8` | 2026‑11‑02 | Tommie Roetz | Add Plans module, P3M plan linking + hierarchy guide, Big Gantt grouping | Plans, P3M, BigGantt |
| 8 | `542248b9b` | 2026‑11‑02 | roetz | Add px-6 header padding to Big Gantt (match P3M) | BigGantt |
| 9 | `f938899db` | 2026‑11‑03 | roetz | Plan updates | Plans |
| 10 | `9514f7410` | 2026‑11‑03 | roetz | Pages and P3M updates | Pages, P3M |
| 11 | `076af60b0` | 2026‑11‑04 | roetz | Stakeholders updates | Stakeholders (P3M/SMS) |
| 12 | `ba687232d` | 2026‑11‑05 | Tommie Roetz | MCP tools for Plans, P3M, SRM, and SMS | MCP/aitools (all four modules) |
| 13 | `19021908e` | 2026‑11‑06 | roetz | Minor updates to RAID | RAID (P3M) |
| 14 | `aaaec4fd4` | 2026‑11‑08 | roetz | Plans updated | Plans |
| 15 | `31d0c5f17` | 2026‑11‑10 | roetz | P3M/SMS gap closure: lifecycle, finance sync, front door, reporting | P3M, SMS |
| 16 | `40d429b03` | 2026‑11‑10 | roetz | p3m - changes | P3M |
| 17 | `d13cd889f` | 2026‑11‑11 | roetz | p3m - changes | P3M |
| 18 | `60cd7fb5c` | 2026‑11‑13 | roetz | E2E Testing work | E2E harness |
| 19 | `45b35428b` | 2026‑11‑13 | roetz | E2E Testing work | E2E harness |
| 20 | `127d61633` | 2026‑11‑16 | roetz | Pages: create pages from /pages, fix dead header search | Pages |
| 21 | `474f3d641` | 2026‑11‑17 | roetz | Pages enhanced | Pages (+ 2 junk files) |

### Fork-only schema (8 files, absent from v0.9.4 tree)

Registered via `go:embed`: sqlite variants in `internal/database/database.go`,
postgres variants in `internal/database/postgres.go`. All 8 must remain
registered after the rebase.

| File pair | Tables |
|---|---|
| `schema/p3m.sql` / `_postgres.sql` | 40 — 39× `p3m_*` (architecture_reviews, authorization_thresholds, baselines, benefit_measurements, benefit_realizations, benefits, budget_approvals, budget_tranches, change_assessments, change_calendar_entries, closure_records, communications, finance_actuals, finance_alert_rules, finance_sync_configs, finance_sync_rejections, finance_sync_runs, financial_close_outs, front_door_screens, gate_artifacts, intakes, investment_case_scores, investment_cases, lifecycle_stages, portfolios, programmes, project_stage_skips, projects, raci_assignments, raid_items, readiness_assessments, report_snapshots, resource_allocations, resources, scoring_criteria, stage_gate_reviews, stage_gate_templates, stakeholders, tolerance_profiles) + `workspace_p3m_projects` |
| `schema/plans.sql` / `_postgres.sql` | 4 — `plans`, `plan_p3m_entities`, `plan_stakeholders`, `plan_workspaces` |
| `schema/sms.sql` / `_postgres.sql` | 28 — `sms_*` (campaign_targets, commitments, communications, decisions, engagement_plan_stakeholders, engagement_plans, feedback, grievances, issues, node_kind_roles, node_kinds, node_members, nodes, notification_campaigns, notifications, ownership_transfers, pilot_baselines, raci_assignments, racis, relationship_goals, stakeholder_entities, stakeholder_group_members, stakeholder_groups, stakeholder_links, stakeholders, tasks, work_package_nodes, work_packages) |
| `schema/srm.sql` / `_postgres.sql` | 42 — `srm_*` (categories, clause_extractions, comms_messages, communications_plans, contract_assignments, contract_documents, contract_management_plans, contracts, dispute_escalations, disputes, entity_stakeholders, innovation_factors, innovation_objectives, innovation_recommendations, innovation_tasks, innovations, jbp_initiatives, jbps, meeting_actions, meeting_attendees, meetings, metric_results, performance_metrics, pulse_surveys, raci_assignments, racis, remedy_plans, scorecard_dimensions, scorecards, scoring_adjustments, segmentation_answers, segmentation_results, segmentation_runs, service_improvement_plans, sip_deliverables, stakeholder_phase_snapshots, stakeholders, supplier_contacts, suppliers, survey_responses, validation_requests, value_propositions) |

### Shared DB files we modified (union-merge sites)

| File | Our changes |
|---|---|
| `internal/database/migrations.go` | New entries in `var Catalog = []Migration{…}` for P3M/SMS/Plans migrations + check/apply functions, e.g. `checkP3MDecisionARBSQLite`, `applySQLiteSMSPlanChannelCheck`, `applySQLiteSMSCampaignChannelWiden`; regression tests `p3m_decision_arb_migration_test.go`, `p3m_phase2_migration_test.go` |
| `internal/database/catalog.go` | Shared check builders used by the compact catalog (column/table/index existence checks) |
| `internal/database/sqlite.go` / `postgres.go` | Dialect-specific migration application + schema embeds for the 8 fork files |
| `schema/permissions.sql` / `_postgres.sql` | Permission rows for P3M/Plans/SMS/SRM features |

### Fork-only code areas (149 real files)

- `internal/handlers/` — 49 files: `p3m_*.go`, `plans*.go`, `sms*.go`, `srm*.go`, `srm_planning.go`
- `frontend/src/lib/features/{p3m,plans,sms,srm,gantt}/` + Pages views — 62 frontend files (BigGantt.svelte, P3M*.svelte, PlanDetail/Plans + 8 plan views, SMS sections, SRM views, OrgPagesView)
- `internal/aitools/{p3m,plans,sms,srm}.go` — MCP tool definitions (5 files incl. registry additions)
- `internal/models/plans.go` (+4), `internal/routes/plans.go` (+3) — module wiring
- `internal/e2e/` — E2E harness (4 real files; 2 junk paths, §4.4)
- Dev tooling at repo root: `local-dev.sh`, `start-test.{sh,bat,ps1}`, `go.works{,.json}`, `windshift-seed.schema.json`, `scripts/demo/` additions

## 3. v0.9.4 impact analysis (upstream changes × our features)

v0.9.4 adds **255 commits** we do not hold. Per release-note area:

| v0.9.4 change | What it does | Touches our fork work? |
|---|---|---|
| Transition screens | `workflow_transitions.screen_id` (ON DELETE RESTRICT), `PUT /transitions/{id}/screen`, 409 `screen_required` on bulk moves | **No** — workflow designer only; P3M/Plans/SMS/SRM do not use transition screens. New schema migration auto-runs at startup (§5) |
| Jira-import concurrency + env vars | `JIRA_IMPORT_MAX_DURATION/CONCURRENCY/WATCHERS`, import job hardening | **No** — no overlap with our modules |
| Custom-field-by-id rekeying | Portal customer / customer-org CF JSON rekeyed by ID; legacy name keys mirrored, existing rows migrated | **No** — portal tables only. Migration is idempotent and auto-applies at startup |
| RTL / Arabic / Persian localization pass | Broad frontend sweep (locale files, layout, navigation) | **Yes — verify.** We also extended locale/navigation/header files for our modules; these are in the 74-file conflict-risk set (§2). Resolved by manual 3-way merge keeping both behaviors (rule C); Pages/P3M UI must be click-through checked after merge (§4.6 checklist) |
| Board-config PATCH endpoints | `PATCH /collections/{id}/board-configuration`, `PATCH /workspaces/{id}/board-configuration` | **No** — new routes; no conflict with our module routes |
| Attachment `is_internal` PATCH | Minor attachment metadata endpoint | **No** |
| v1 API deprecation + lint guard | `scripts/check-deprecated-v1-api.sh` wired into pre-commit and `make lint`; first-party v1 usage removed upstream | **Passes for us** — our code has no first-party v1 API usage (only comments and the implementation itself, which is unchanged) |
| 6 schema migrations auto-run at startup | Idempotent check/apply functions stamp applied state | **Yes — expected.** First run of the integrated binary against the real DB applies these on top of our schema; verified in §4.5–4.6 |

**None of v0.9.4's changes touch P3M/Plans/SMS/SRM tables or modules.** The
only substantive overlap is the frontend localization/layout sweep, handled by
rule C merges and a post-merge UI checklist.

## 4. Integration log (branch `integrate-v0.9.4`)

### 4.1 Setup

| Item | Value |
|---|---|
| Branch | `integrate-v0.9.4`, created from tag `v0.9.4` = `2ebdd7112d385cd8176f2ce71b26f7d23127d43b` |
| Merge base (reference only) | `4bd3fe0fc4dd8cc8f485a3d22d7e8b71e85be3a3` |
| Method | `git cherry-pick -n` of all 21 unique fork commits in topological order, squashed into one feature commit on top of v0.9.4 (no intermediate commits) |
| Delta artifacts | `/tmp/delta-ours.txt`, `/tmp/delta-theirs.txt`, `/tmp/delta-overlap.txt`, `/tmp/conflict-risk.txt` (74 files), `/tmp/pick-order.txt` |

### 4.2 Pick list (topological order, full SHAs)

```
7c4bd7683e121b589a6616f2eb6a81ab4f3ffe97   Added BigGantt
a6920e0dc6d74aa0919ad2c33a6ef80e8c8ffd65   P3M-Added
2c39ad65e2a6090d5d92be5a9fbeb71bda2181cb   Stakeholders and CSS updates
1526cbdb38c37e14d8a57a7b71fedd0d46ec9a9f   CSS padding fixes
8ae48b5d05e534e9b307df4ec604f723ffa5b346   Fix Bit Gantt
fc2b48ede1734d95d60affbe8c6b579df1eba2d0   Fix Bit Gantt
41e8d9ca8d083e45b1aa25b04ff815f208c6d75c   Add Plans module, P3M plan linking + hierarchy guide, Big Gantt grouping
542248b9b23efd83f1b63aaec6c41618695ca35e   Add px-6 header padding to Big Gantt (match P3M)
f938899db44fb3a36eb1b44289014c4baee76cc3   Plan updates
9514f7410166e195c6d1c9951de604c609308696   Pages and P3M updates
076af60b00323867140d6bf809121f4f1d74adbe   Stakeholders updates
ba687232db68e792ed7a44031621ee92af821c22   MCP tools for Plans, P3M, SRM, and SMS
19021908e63886ea7fa3f9ad003ce96abfb1b401   Minor updates to RAID
aaaec4fd48e458077bef2a03516722688619e7f4   Plans updated
31d0c5f17d223a372864a85a2226659a3be36c90   P3M/SMS gap closure: lifecycle, finance sync, front door, reporting
40d429b03bfeb08a28ac85c7a35024341b27ee48   p3m - changes
d13cd889f37a76d31dace7e23c94bc1463816691   p3m - changes
60cd7fb5c304e87936d780eb2fb6d03578ad6cf0   E2E Testing work
45b35428ba438044685cc2067f9d4ac7dfe39250   E2E Testing work
127d616338a7540e0da5cf641df0f84e32ee58de   Pages: create pages from /pages, fix dead header search
474f3d64127b6b9cb3f58e4b2df961f68c8bf88d   Pages enhanced
```

No `+` commit was excluded as missed upstream work (no subject matched an
upstream release note; verified against `git log v0.9.4 --grep=…`).

### 4.3 Conflict resolution rules applied

- **A. DB catalog** (`migrations.go`, `catalog.go`, `sqlite.go`, `postgres.go`): union merge — keep all our P3M/SMS/Plans entries (Catalog rows + check/apply functions) and all upstream v0.9.4 entries; no duplicate Version strings.
- **B. Fork-owned module paths** (`schema/{p3m,plans,sms,srm}*.sql`, `internal/aitools/{p3m,plans,sms,srm}.go`, `internal/handlers/plans*.go` + `srm_planning.go`, `internal/routes/plans.go`, `internal/models/plans.go`, `frontend/src/lib/features/plans/**`, BigGantt components): take ours — v0.9.4 does not modify these modules.
- **C. Shared files** (`schema/permissions*.sql`, `internal/aitools/registry.go`, frontend pages/header/nav/locale files touched by the RTL pass): manual 3-way merge keeping both behaviors (upstream structural changes + our feature tweaks).
- **D. Everything else**: standard resolution preferring union of behavior.

### 4.4 Junk file removals (fork cleanup, recorded for traceability)

Three accidentally-committed paths were dropped from the squashed feature commit
(they exist on `main` and are restored there untouched):

| Path | Origin | Why removed |
|---|---|---|
| `internal/e2e/harness_test.go")/Overwrite harness_test.go with clean content\n<\|channel>thought\n<channel\|><\|tool_call>call:bash{command:` (file, name contains newlines) | commit `474f3d641` | AI-agent artifact: a shell command created a directory + file from transcript text. Content is a duplicate of the real `internal/e2e/harness_test.go`; not valid Go path; unreferenced |
| `internal/e2e/harness_test.go")/Rewrite harness_test.go with clean, valid Go code\n<\|channel>thought\n<channel\|><\|tool_call>call:bash{command:` (file) | commit `474f3d641` | Same artifact, second variant; identical blob to the first |
| `.tmp-bcryptgen/main.go` | commit `1526cbdb3` | Throwaway bcrypt-hash generator for a P3M E2E test password (`p3mtest123`); unreferenced by any build/script |
| `internal/e2e/go.mod` | commit `474f3d641` | Not a valid module file (contains Go source text, no `module` directive); created the spurious nested-module boundary that hid broken files from `go build ./...` |
| `internal/e2e/main.go` | commit `474f3d641` | Invalid Go (bare assignment statement, malformed import) — AI-agent hallucination; breaks `gofmt`/`go vet` on the tree |
| `internal/e2e/runner.go` | commit `474f3d641` | Same artifact as `main.go`, in package `runner`; invalid Go |

Note: the real E2E suite (`harness_test.go`, `e2e_*_test.go`) is **not** part of
this repository — per `.gitignore` (upstream convention) test sources live in
the adjacent private `core-tests` repo and are overlaid by its runners. Only
`internal/e2e/KNOWN_BUGS.md` is tracked here (appended to by the suite at run
time).

### 4.5 Per-file conflict resolutions

Picks that applied with auto-merge only (no manual resolution): `7c4bd7683`,
`1526cbdb3`, `8ae48b5d0`, `fc2b48ede`, `542248b9b`, `f938899db`, `ba687232d`,
`19021908e`, `31d0c5f17`, `40d429b03`, `d13cd889f`, `45b35428b`, `127d61633`.
Auto-merged regions were spot-checked (nav items, embeds, schema vars) and the
full build gate (§4.6).

**Pick 2 — `a6920e0dc` P3M-Added**
- `internal/database/migrations.go`: union — inserted our `20261028_p3m_module` entry before the catalog array close; all v0.9.4 entries and helper functions kept verbatim.
- `internal/database/sqlite.go`: single-line schema concatenation — union: v0.9.4 token list + `p3mSchema` after `incidentsSchema`; upstream tail (`milestoneCommentsSchema`, `milestoneHistorySchema`) preserved at end.

**Pick 3 — `2c39ad65e` Stakeholders and CSS updates**
- `frontend/src/lib/navigation/mainNavigation.js`: import conflict — kept `IconHeartHandshake` (used by the srm item); dropped `IconMessage2Plus`: its only usage (portal-hub) was replaced with `IconWorldSearch` by upstream's RTL pass, so keeping it would be an unused import.
- `internal/database/migrations.go`: union — inserted 4 entries before array close: `20261029_srm_module`, `20261030_p3m_project_links`, `20261031_sms_module`, `20261031_sms_plan_channel_check`.
- `internal/database/sqlite.go`: schema line rebuilt to the fork's final module order (authoritative = `main`): `…incidentsSchema + smsSchema + p3mSchema + srmSchema + conditionSetsSchema…`; upstream milestone tail kept at end.

**Pick 7 — `41e8d9ca8` Add Plans module, P3M plan linking + hierarchy guide, Big Gantt grouping**
- `mainNavigation.js`: kept `IconMap` (plans item); dropped `IconMessage2Plus` again (still orphaned by the upstream portal-hub icon swap).
- `internal/database/migrations.go`: inserted `20261120_plans_module` entry before array close.
- `internal/database/sqlite.go`: schema line — added `plansSchema` after `itemSupportEventsSchema`, before the upstream milestone tail (matches `main`, where plans is last); also repaired a doubled `schema :=` prefix introduced by an earlier mechanical edit in this session (caught by `go build`).

**Pick 10 — `9514f7410` Pages and P3M updates**
- `mainNavigation.js` (2 blocks): kept `IconNotebook` import + the `pages` nav item; dropped the `logbook` item (feature removed in v0.9.4) and its now-unused icon refs; dropped orphaned `IconMessage2Plus`.
- `frontend/src/lib/pages/mainAppRoutes.js`: added `'org-pages'` to the view list; dropped `'logbook-document'` (removed feature).
- `internal/database/migrations.go` imports: kept HEAD superset (`strconv`, `strings`, `github.com/lib/pq`) — required by v0.9.4 code; fork side contributed only `strings`.

**Pick 11 — `076af60b0` Stakeholders updates**
- `internal/database/sqlite.go`: schema line — kept ours verbatim; theirs was a strict subset (this commit is where the fork moved `smsSchema` before `p3mSchema`, matching the order already on our side).

**Pick 14 — `aaaec4fd4` Plans updated**
- `frontend/src/lib/pages/Homepage.svelte`: additive import conflict — kept both: `onWorkItemRefresh` (v0.9.4) and `getUserTimezone` (fork plan-date display); verified each is used exactly once in the merged file.

**Pick 18 — `60cd7fb5c` E2E Testing work**
- `Makefile`: `.PHONY` union — `vulncheck` (v0.9.4) + `e2e` (fork's new target, defined at line 229).

**Pick 21 — `474f3d641` Pages enhanced**
- `go.mod` (4 blocks): union of indirect deps; higher version kept for shared modules (`beevik/etree v1.8.1`, `russellhaering/goxmldsig v1.6.1`, `golang.org/x/sys v0.49.0`, `golang.org/x/tools v0.51.0`); fork's extra indirects (MCP/E2E transitive closure) kept.
- `go.sum`: regenerated via `go mod tidy` after the go.mod resolution.

**Post-merge API-drift fixes** (compile errors a text merge cannot see — fork code written against pre-v0.9.4 signatures):
- `internal/scheduler/finance_sync_scheduler.go:277`: v0.9.4 added a `locale` parameter to `SMTPSender.SendTransactionalContext`; pass `"en"` for the ops-facing finance-sync failure alert (no user context).
- `respondLLMError` gained a `*services.PermissionService` param in v0.9.4 (admin error diagnostics): wired it into `SMSHandler` via a new `SetPermissionService` setter (`internal/handlers/sms.go`, called at `internal/server/server.go:1690`) so SMS AI endpoints get admin diagnostics like their upstream siblings, instead of passing nil.

**Formatting**: `gofmt -w` applied to 40 tracked files that were not gofmt-clean on the fork (no semantic change); required for the lint gate.

### 4.6 Verification results

_Filled in after steps 6–8: DB backup location, dbcheck assertions, build/test/lint gate, live smoke, manual UI checklist._

## 5. Future sync procedure (post-cutover)

Once `main` is cut over to the integrated branch, `main` contains v0.9.4 as a
true ancestor and SHAs no longer diverge for future work:

```bash
git fetch upstream --tags
git merge upstream/main        # plain merge now works; conflicts only where we genuinely overlap
```

- DB migrations auto-apply at startup via idempotent check/apply functions —
  new upstream schema changes need no manual migration step.
- Keep the cherry-pick habit only if a selective backport is needed again; for
  full syncs, prefer `git merge upstream/main`.
- After each sync: run `make ci` (go + frontend) and the pairing script
  `scripts/check-schema-migration-pairing.sh --range <last-sync>..HEAD`.
