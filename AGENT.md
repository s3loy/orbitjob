# Agent Workflow Guidelines

**Purpose**: Prevent documentation-code divergence, inconsistent information across files, and stale references.

## 1. Single Source of Truth (SSOT) Principle

Each type of information should be defined in exactly one place. All other locations must reference or stay consistent with that single source.

| Information Type | Authoritative Source | Forbidden Duplication |
|-----------------|---------------------|----------------------|
| Tech stack versions | `go.mod`, `Dockerfile`, `charts/*/Chart.yaml` | README, CONTRIBUTING, CLAUDE.md must reference, not redefine |
| API schema | `api/openapi.yaml` | USAGE.md examples must match schema |
| Environment variables | `deploy/env/*.example`, `charts/*/values.yaml` | Documentation references only, never redefines defaults |
| Database schema | `db/migrations/*.up.sql` | Documentation descriptions must match latest migration |
| Deployment methods | `README.md` quick start + `docs/local-deployment.md` full guide | USAGE.md must not detail deprecated methods |
| Branch strategy | `CONTRIBUTING.md` | CLAUDE.md references only, never re-describes |
| Commit conventions | `CONTRIBUTING.md` | CLAUDE.md references only, never duplicates type/scope lists |

## 2. Code Change → Documentation Update Rules

**Trigger → Required Documentation Updates**:

| Code Change | Must Update | Verification Method |
|-------------|------------|---------------------|
| Add/remove HTTP endpoint | `api/openapi.yaml` + `USAGE.md` examples | `make openapi-check` |
| Modify env var name/default | `deploy/env/*.example` + `charts/*/values.yaml` + `README.md` quick start + `USAGE.md` config section | grep all docs for old var name |
| Add/modify handler type | `USAGE.md` handler section + `api/openapi.yaml` JobDefinition schema | OpenAPI validator |
| Change deployment flow (add/remove/replace method) | `README.md` quick start + `docs/local-deployment.md` + `USAGE.md` deployment choice | All three files must consistently describe current recommended method |
| Add/modify Makefile target | `docs/local-deployment.md` Makefile reference (if exists) | `grep -E '^[a-z][a-z-]*:' Makefile` output matches docs |
| Modify database migration | `docs/database-setup.md` (if involves role/RLS) | Migration comments match doc descriptions |
| Modify CRD schema | `charts/orbitjob/crds/*.yaml` + `USAGE.md` CRD usage section | kubectl explain output matches docs |
| Deprecate feature/config/API | Mark "deprecated" in all docs mentioning it + migration path | grep -r search for deprecated term |

## 3. Deprecation and Evolution Strategy

**Deprecation ≠ immediate removal**. Evolution path for old methods/configs:

### Phase 1: Deprecation Period
- Code retains compatibility
- Documentation marks: `> **Deprecated**: This method will be removed in the next release. Please migrate to [new method](link).`
- CHANGELOG records deprecation

### Phase 2: Removal
- Remove compatibility layer from code
- Remove or move documentation to "historical versions" section
- Archive the migration guide under `docs/migrations/`

**Current Examples**:
- Docker Compose method: **removed** together with the legacy execution path — docs now describe it only as history; do not present `docker compose` / `make docker-up` as current behavior
- `DATABASE_DSN`/`SCHEDULER_DSN` old variables: Now use `RUNTIME_DSN`, but retain fallback → docs must clarify "backward compatible, forbidden for new deployments"
- `make dev` target: **removed** with the devserver binary → docs must not reference it except as history

## 4. Documentation Consistency Checklist

**Must run after every change**:

```bash
# 1. OpenAPI sync
make openapi-check

# 2. Environment variable consistency (automation TODO)
# Check variable names match across deploy/env/*.example, charts/*/values.yaml, docs

# 3. Version number consistency
grep -r "Go 1\." README*.md CONTRIBUTING.md docs/ CLAUDE.md
# Expected: all occurrences must be same version

# 4. Deployment method description consistency
grep -i "docker compose\|kind.*helm\|quick start" README*.md docs/local-deployment.md USAGE.md
# Expected: recommended method must be consistent

# 5. Deprecation marking completeness
git diff HEAD~1 --name-only | while read f; do
  # If f is code file and removes feature, check docs marked deprecated
done
```

**Manual checks before merge**:
- [ ] Changed env vars → grep all docs for old name, confirm updated
- [ ] Changed API → OpenAPI + USAGE examples consistent
- [ ] Changed deployment flow → README/local-deployment/USAGE all three consistent
- [ ] Added new concept (e.g. Operator) → CLAUDE.md architecture diagram, README feature list, USAGE usage section all synced
- [ ] Modified dependency version → README badge, CONTRIBUTING dev environment, CLAUDE.md tech stack table all synced

## 5. Multi-language Documentation Sync

`README.md` (English, canonical) and `README.zh.md` (Chinese variant) must have equivalent content (not word-for-word translation, but same information):

- Modify the English README → sync `README.zh.md`
- Add new section → sync both files
- Version numbers, links, command examples → exactly identical

**Verification**:

```bash
# Section count consistency
grep '^##' README.md | wc -l
grep '^##' README.zh.md | wc -l

# Key command consistency
diff <(grep '```bash' README.md) <(grep '```bash' README.zh.md)
```

## 6. CLAUDE.md Maintenance Principles

`CLAUDE.md` is AI project instructions, **not user documentation**, therefore:

- **Role**: Define tech stack, architecture dependencies, dev constraints, quality gates, current status
- **Should not include**: Detailed API usage, deployment steps, troubleshooting (these belong in README/USAGE/CONTRIBUTING)
- **Reference, not repeat**: Branch strategy, commit conventions, test commands → detailed in CONTRIBUTING, CLAUDE.md only references
- **Sync timing**: Architecture changes, tech stack upgrades, new constraints, gate adjustments → update CLAUDE.md immediately

**CLAUDE.md Self-check**:
- [ ] Tech stack table versions match go.mod/Dockerfile
- [ ] Architecture diagram dependency directions match actual imports
- [ ] Key path directories match actual directory structure
- [ ] Environment variable names match deploy/env/*.example
- [ ] "Current Status" section reflects latest git log + memory
- [ ] Deprecated methods marked or removed

## 7. Agent Execution Commitments

As an AI agent, I commit to:

1. **Sync docs immediately with code changes**: No "TODO: update docs", complete in same PR
2. **grep before changing**: Before modifying terms/configs, grep entire repo to find all mentions
3. **Explicitly mark deprecations**: When removing old method, mark `> **Deprecated**` in docs + migration path
4. **No compromise on OpenAPI**: API changes must pass `make openapi-gen && make openapi-check`
5. **SSOT, no duplication**: When finding duplicate definitions → alert s3 and merge to single source
6. **Proactively clean stale info**: When finding docs not matching code → fix immediately, don't wait for s3 to discover
7. **Multi-language sync**: Modify `README.md` → automatically sync `README.zh.md`

**Penalty for violations**: s3 has the right to reject PR, requiring documentation re-sync.

## 8. Automation Improvement Roadmap (TODO)

Currently manual checks, future automation:

- [ ] pre-commit hook: Check env var names consistent across deploy/env, charts, docs
- [ ] CI check: README.md and README.zh.md section count, command examples consistent
- [ ] CI check: Version numbers in CLAUDE.md match go.mod
- [ ] CI check: Directory paths in docs exist in repo
- [ ] CI check: API example payloads match OpenAPI schema (JSON schema validator)

---

**Last Updated**: 2026-09-18 (Kubernetes-only architecture convergence: compose stack, devserver, dispatcher and worker removed from docs; language unified — English docs everywhere, Chinese only in `README.zh.md`)
