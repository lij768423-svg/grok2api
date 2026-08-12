# Local Grok2API patches

## Current production image

`grok2api:3.1.0-precool-480k-20260804`

Base: upstream `v3.1.0` (includes PR #837 egress quality guard and probe-wait recovery).

Local-only behavior still carried forward:

- Free Build quota precool at rolling 24h total_tokens >= 480000
  (freeQuotaPreCoolTokens + maybePrecoolFreeQuotaAfterSuccess).

## Quality guard

- Upstream v3.1.0 ships admin Quality Guard UI + internal bootstrap sidecar API.
- Production still runs host-network sidecar
  `grok2api-egress-quality-guard:20260802-account-fallback` with bind-mounted
  `./quality_guard.py` and `./quality-guard.env` (admin API + host-only session
  rotator) so sticky/rotation topology keeps working.
- `config.yaml` has `qualityGuard.enabled: true` matching the env policy.

## Rollback

Use the newest directory under `backups/upgrade-3.1.0-*`.
Set GROK2API_IMAGE back to `grok2api:3.0.11-precool-480k-20260804` and
`docker compose up -d grok2api`.

## Quality guard official bootstrap (current)

- Image: `grok2api-egress-quality-guard:3.1.0-official` from upstream v3.1.0
  `tools/egress-quality-guard` (+ optional `GROK2API_BASE_URL` env).
- Auth: bootstrap internal token at `/api/internal/v1/quality-guard` (not admin login).
- Policy: only `config.yaml` → `qualityGuard` (admin UI hot-reloads runtime config).
- Network: `network_mode: host` + an internal `GROK2API_BASE_URL` so the
  sidecar can reach the host-only session-rotator and private Grok2API endpoint.
  Exact internal addresses are intentionally omitted from documentation.

## Quality guard TTFT rank scheduler (2026-08-05)

- Baseline frozen as quality_guard.official-3.1.0.py (docker cp from image grok2api-egress-quality-guard:3.1.0-official).
- Runtime bind-mount: ./quality_guard.py -> /usr/local/bin/grok2api-egress-quality-guard.
- Behavior: EWMA first-token ranking on healthy nodes; weighted auto-account target shares.
- Enabled apply: RANK_DRY_RUN=false (2026-08-05). Eval under rank-eval/ with 5m collect + 24h report cron.
- Admin credentials for inventory/apply come from
  `GROK2API_ADMIN_USERNAME` plus a private password file with restrictive
  permissions (for example, mode `0600`).
- Ranking knobs are env-only (not in runtime-config schema).
- Rollback: remove rank bind mounts + RANK_* env from compose, then docker compose up -d egress-quality-guard.
  Or set RANK_SCHEDULER_ENABLED=false.
