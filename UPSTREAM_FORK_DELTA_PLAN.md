# Upstream fork delta documentation + v0.9.4 integration (branch)

## Context

User request (verbatim): "document in detail the upstream base we forked from to determine the delta between our fork and the base. Make sure we do not break our fork in implementing the fixes or changes they have made." Scope confirmed with user: produce `docs/upstream-fork.md` **and** execute the v0.9.4 integration on branch `integrate-v0.9.4` with DB backup/verification. **`main` stays untouched; nothing is pushed.** Cutover of main is a separate later decision.

## Verified facts (basis for the doc — re-verify SHAs in step 1)

- **Upstream**: https://github.com/Windshiftapp/core — Windshift, AGPL‑3.0, Go 1.27 + SvelteKit frontend, single binary, SQLite (`modernc.org/sqlite`) or Postgres. Default branch `main`; latest release **v0.9.4** = tag `2ebdd7112d385cd8176f2ce71b26f7d23127d43b` (Nov 10, 2026). Upstream main is ~184 commits ahead of v0.9.4 — out of scope per request.
- **Fork**: TRoetz/core (`origin`), local `/media/dragonzine/Data/Source/core`, `main` @ `474f3d64` (Nov 17, "Pages enhanced"), 3618 commits since 2026‑01‑13.
- **Fork point / true common ancestor** with the v0.9.4 lineage: **`4bd3fe0fc4dd8cc8f485a3d22d7e8b71e85be3a3`** — "docs(api): refresh generated OpenAPI schema", Stefan Ernst, 2026‑07‑10. Verified: ancestor of local main; GitHub compare API (`compare/3248819a...v0.9.4`) → `status: diverged`, merge_base = this SHA (ahead_by 1824 / behind_by 1561).
- **Maintenance model**: continuous **cherry-pick sync** of upstream commits onto fork main (40 of the last 300 commits have author≠committer; messages carry "(cherry picked from commit …)"). Consequence: SHAs diverged → `git merge v0.9.4` is not viable; integration must rebase our unique work onto v0.9.4.
- **Last upstream content we hold** ≈ `3248819a` (Oct 31, "fix(items): resolve an omitted item type before hierarchy validation (WI‑1956)").
- **Fork-specific commits**: 21 by roetzt / Tommie Roetz, Oct 28–Nov 17 — BigGantt (`7c4bd768`), P3M module (`a6920e0d`, `d13cd889`, `40d429b0`, …), Plans module (`41e8d9ca`, `aaaec4fd`), SMS/SRM gap closure (`31d0c5f1`), MCP tools for Plans/P3M/SRM/SMS (`ba687232`), RAID (`19021908`), Stakeholders (`076af60b`, `2c39ad65`), Pages enhancements (`474f3d64`, `127d6163`, `9514f741`), E2E tests (`45b35428`, `60cd7fb5`). Authoritative list = step 2 output.
- **Fork-only schema files** (absent from v0.9.4 tree; local has 131 vs upstream's 123): `internal/database/schema/{p3m,plans,sms,srm}.sql` + `_postgres.sql`. Tables include `plans`, `plan_p3m_entities`, `plan_stakeholders`, `plan_workspaces`, 41× `p3m_*`, `workspace_p3m_projects`, plus sms/srm tables.
- **Shared DB files we modified**: `internal/database/{catalog.go, migrations.go, sqlite.go, postgres.go}`, `schema/permissions.sql(+_postgres)`; our migration entries + check/apply functions inside `migrations.go` (e.g. `checkP3MDecisionARBSQLite`, `applySQLiteSMSPlanChannelCheck`, `applySQLiteSMSCampaignChannelWiden`) and regression tests `p3m_decision_arb_migration_test.go`, `p3m_phase2_migration_test.go`.
- **v0.9.4 changes relevant to us** (release notes): transition screens (`workflow_transitions.screen_id` ON DELETE RESTRICT; PUT /transitions/{id}/screen; 409 screen_required on bulk moves), Jira-import concurrency + `JIRA_IMPORT_MAX_DURATION/CONCURRENCY/WATCHERS`, custom-field-by-id rekeying of portal customer/customer-org CF JSON (legacy name keys mirrored, rows migrated), RTL/Arabic/Persian localization pass (broad frontend sweep — touches files we also extended), board-config PATCH endpoints (`PATCH /collections/{id}/board-configuration`, `PATCH /workspaces/{id}/board-configuration`), attachment is_internal PATCH, v1 API deprecation + `scripts/check-deprecated-v1-api.sh` lint guard (pre-commit + make lint; no first-party v1 usage in our code — only comments and the implementation itself), 6 schema migrations auto-run at startup. **None touch P3M/Plans/SMS/SRM tables or modules.**
- **Toolchain**: go1.27.3 ✓ matches go.mod; Makefile targets `build`(48) / `ci`(133 = ci-go + ci-frontend) / `lint`(193); frontend node_modules present; DB path via `-db` flag or `DB_PATH` env (internal/config/load.go:30,84). Build/run pattern from local-dev.sh: `go build -o .dev-windshift .` then `.dev-windshift -port <PORT> -db <path> -attachment-path <path> -no-csrf`. A stray `/tmp/windshift-smoke` process (PID 505598) is running — stop before backup.

## Approach

### Step 1 — Fetch upstream, verify refs
```bash
git remote add upstream https://github.com/Windshiftapp/core.git   # if absent
git fetch upstream --tags --prune
git rev-parse 'v0.9.4^{commit}'    # expect 2ebdd7112d385cd8176f2ce71b26f7d23127d43b
MB=$(git merge-base main v0.9.4)   # expect 4bd3fe0fc4dd8cc8f485a3d22d7e8b71e85be3a3
```
Use `$MB` symbolically in all later steps; never hardcode the SHA downstream.

### Step 2 — Compute delta sets → /tmp/delta-*.txt
```bash
git cherry -v v0.9.4 main > /tmp/delta-ours.txt    # '+' = fork-specific commits to rebase; '=' = re-SHA'd upstream work (exclude)
git cherry -v main  v0.9.4 > /tmp/delta-theirs.txt # '+' = upstream content we lack (arrives via the new base — no action needed)
comm -12 <(git diff --name-only $MB..main | sort) \
         <(git diff --name-only $MB..v0.9.4 | sort) > /tmp/delta-overlap.txt   # conflict-risk files
```
Expect ~21–40 `+` lines in delta-ours (the 21 known roetz/Tommie commits; more if other authors did fork work).

### Step 3 — Write docs/upstream-fork.md
Sections, content from "Verified facts" above + step 2 outputs:
1. **Upstream base** — repo, license, stack, default branch, release cadence; fork point `$MB` (SHA/date/subject); maintenance model = cherry-pick sync with evidence; last synced content `3248819a`.
2. **Fork delta** — table of unique commits from /tmp/delta-ours.txt (SHA, date, subject) mapped to modules (P3M, Plans, SMS, SRM, BigGantt, RAID, Stakeholders, Pages, MCP tools, E2E); the 8 fork-only schema files + their tables; shared DB files we modified.
3. **v0.9.4 impact analysis** — per area: what changed × whether it touches our features (transition screens → workflow designer only; CF rekeying → portal tables only; RTL pass → broad frontend, verify Pages/P3M UI after merge; v1 lint guard → passes, no first-party usage).
4. **Integration log** — filled during steps 4–5: branch name, pick list, per-file conflict resolutions, verification results.
5. **Future sync procedure** — post-cutover, main contains v0.9.4 as an ancestor → `git fetch upstream && git merge upstream/main` works directly; DB migrations auto-apply at startup (idempotent check functions).

### Step 4 — Create integration branch
```bash
git checkout -b integrate-v0.9.4 v0.9.4
# commit docs/upstream-fork.md as the first commit on this branch
```

### Step 5 — Rebase our unique work onto v0.9.4 (squash)
Apply all `+` commits from /tmp/delta-ours.txt in topological order without intermediate commits, then one feature commit:
```bash
git cherry-pick -n <sha1> <sha2> ... <shaN>   # pauses on conflict; resolve per rules below and continue
# after the last pick:
git commit -m "Fork features (P3M/Plans/SMS/SRM/Pages/Gantt/MCP/E2E) rebased onto v0.9.4" \
           -b "Source commits: <sha1> … <shaN>"   # full SHA list in body for traceability
```
Conflict resolution rules by file class:
- **A. DB catalog** (`internal/database/migrations.go`, `catalog.go`, `sqlite.go`, `postgres.go`): union merge — keep all our entries (P3M/SMS/Plans migrations + check/apply functions) and all upstream v0.9.4 entries; no duplicate Version strings.
- **B. Fork-owned module paths** (`schema/{p3m,plans,sms,srm}*.sql`, `internal/aitools/{p3m,plans,sms,srm}.go`, `internal/handlers/plans*.go` + `srm_planning.go`, `internal/routes/plans.go`, `internal/models/plans.go`, `frontend/src/lib/features/plans/**`, BigGantt components): take ours — upstream v0.9.4 does not modify these modules.
- **C. Shared files** (`schema/permissions*.sql`, `internal/aitools/registry.go`, frontend pages/header/nav/locale files touched by the RTL pass): manual 3-way merge keeping both behaviors (upstream structural changes + our feature tweaks).
- **D. Everything else**: standard resolution preferring union of behavior.

Escape hatch: if >15 files conflict, or any file cannot be resolved confidently under these rules → stop and report with per-file analysis (`git diff v0.9.4 main -- <file>`); do not guess.

### Step 6 — DB backup + migration dry-run on a copy
```bash
pkill -f windshift    # stops stray /tmp/windshift-smoke (PID 505598) and any dev server; note in report
TS=$(date +%Y%m%d-%H%M%S); mkdir -p /tmp/windshift-backup-$TS
cp windshift.db windshift.db-wal windshift.db-shm /tmp/windshift-backup-$TS/   # consistent: no writer active
go build -o .dev-windshift-integrated .
cp /tmp/windshift-backup-$TS/windshift.db /tmp/upgrade-test.db
./.dev-windshift-integrated -port 19473 -db /tmp/upgrade-test.db -attachment-path /tmp/ws-attach -no-csrf &   # wait for "listening" in output, then kill
cd <repo> && go run /tmp/dbcheck.go /tmp/upgrade-test.db
```
`/tmp/dbcheck.go` (throwaway; driver `modernc.org/sqlite` resolves from the repo's go.mod when run with cwd = repo root): opens the DB read-only and asserts, exiting non-zero on any failure:
- Fork tables intact via sqlite_master: `plans`, `plan_p3m_entities`, `plan_stakeholders`, `plan_workspaces`, ≥40 `p3m_%` tables, `workspace_p3m_projects`, sms/srm tables (names from the schema files).
- v0.9.4 migrations applied: column `screen_id` on `workflow_transitions`; table `notification_template_translations`; columns `language`,`timezone` on `portal_customers`; column `delivered` on `item_due_reminders`.

### Step 7 — Build/test/lint gate (on integrate-v0.9.4)
```bash
go build ./...
go test ./internal/database/...        # includes p3m_decision_arb_migration_test.go, p3m_phase2_migration_test.go + upstream migration tests
bash scripts/check-schema-migration-pairing.sh --range v0.9.4..integrate-v0.9.4
make lint                              # includes new check-deprecated-v1-api.sh guard
make ci-frontend                       # npm ci/audit/check/typecheck (node_modules present)
```

### Step 8 — Live smoke against the real DB
Start `./.dev-windshift-integrated -port <free> -db windshift.db -attachment-path <existing> -no-csrf`; verify:
- Health endpoint → 200 (`curl -s localhost:<port>/health`).
- v0.9.4 surface exists: `PUT /transitions/1/screen` → 401 (auth required), not 404; `PATCH /collections/1/board-configuration` → 401, not 404.
- Our modules respond: `/plans` route and P3M pages return 200 for an authenticated session — if no easy auth path via curl, record as manual UI checklist (pages to click through) instead of weakening steps 2–6.
Then stop the server. The real DB now has v0.9.4 migrations applied on top of our schema; backup remains at `/tmp/windshift-backup-$TS/`.

### Step 9 — Final state
- Branch `integrate-v0.9.4` ready for review: [doc commit] + [feature commit]. main untouched; nothing pushed.
- Report: delta stats, conflict resolutions applied, DB backup location, verification results, manual UI checklist (if any).

## Critical files & anchors

1. `internal/database/migrations.go` — `var Catalog = []Migration{...}` (~line 213) + our P3M/SMS check/apply functions; primary union-merge site (rule A).
2. `internal/database/catalog.go` — schema file registration; all 8 fork files must remain registered after the merge.
3. `docs/upstream-fork.md` — new deliverable, committed first on integrate-v0.9.4.
4. `.git/config` — add `upstream` remote (step 1).
5. `windshift.db` (+ `-wal`, `-shm`) at repo root — live DB; back up before any binary run (step 6).

## Verification (end-to-end)

1. `git rev-parse 'v0.9.4^{commit}'` → `2ebdd71…d43b`; `git merge-base main v0.9.4` → `4bd3fe0f…a3`.
2. On integrate-v0.9.4: `git merge-base --is-ancestor v0.9.4 HEAD && echo OK` → proves all upstream fixes are in the branch.
3. `git cherry -v v0.9.4 integrate-v0.9.4 | grep '^+'` → exactly 2 lines (doc commit + feature commit) — no lost fork work; cross-check that each of the 21 known SHAs appears in the feature commit body.
4. Step 6 assertions all pass on the upgraded copy (fork tables intact + v0.9.4 columns present).
5. `go build ./...` exit 0; `go test ./internal/database/...` PASS; pairing script exit 0; `make lint` PASS; `make ci-frontend` PASS.
6. Step 8: health 200; new endpoints return 401 not 404; server log shows no migration errors on the real DB.

## Assumptions & contingencies

- Scope (user-confirmed): branch only — main untouched, no push, cutover is a separate later decision. Target = tag v0.9.4, not upstream main (+184 commits).
- If `git merge-base` after fetch ≠ 4bd3fe0f (upstream rewrote history again) → use the computed value as `$MB`; record actual in doc; steps unchanged.
- If delta-ours contains a `+` commit that looks like upstream work we missed (subject matches an upstream release note) → verify with `git log v0.9.4 --grep=<subject>` and exclude it from the pick list, recording why in the integration log.
- sqlite3 CLI is not installed → DB assertions use the throwaway Go program with modernc.org/sqlite (step 6); delete /tmp/dbcheck.go afterward.
- If step 8 auth makes curl checks of our modules impractical → record as manual UI checklist instead; do not weaken steps 2–6.
