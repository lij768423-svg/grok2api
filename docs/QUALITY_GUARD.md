# Grok2API Quality Guard — Agent Reference

> 管理入口：`<admin-base-url>/quality-guard`（Admin SPA 页面；实际内网地址不写入文档）
> 初次扫描：2026-08-11；实现复核：2026-08-12
> 用途：让 agent 理解「出口质量守护」如何配置、判定、隔离、恢复与账号再平衡，并在排障时知道该读哪份文件、改哪一层配置。

> **时效说明：** 本文中的配置结构和代码行为按当前 `quality_guard.py` 描述；带“生产当前”“实况”“累计统计”的数值只是标注日期的扫描快照。排障时必须重新读取 runtime-config、bootstrap、state 和容器环境，不得把快照当成实时状态。

---

## 1. 一句话是什么

**Quality Guard = 旁路 sidecar**，持续观察 `grok_build` 出口节点上的真实流式请求（passive）/ 主动探针（active），按 **输出 TPS + 生成窗口** 把节点判为 healthy / soft / hard。异常时 **只 disable 节点（不删账号绑定）**，可选 **换 sticky 出口 IP**，恢复后再 enable。本地扩展还按 **TTFT EWMA** 给健康节点打分，**自动把 auto 账号迁到更快的节点**。

它不是请求路径上的中间件：用户流量仍走 `grok2api` 主服务；Guard 通过 **internal API + admin API + session-rotator** 间接改调度拓扑。

---

## 2. 拓扑与进程

```
浏览器 Admin UI  /quality-guard
        │  (配置热更新 → runtime-config.json)
        ▼
┌─────────────────────┐     internal token      ┌──────────────────────┐
│ grok2api            │◄───────────────────────│ egress-quality-guard │
│ qualityGuard block  │   /api/internal/v1/     │ (host network)       │
│ bootstrap.json 写出 │   quality-guard/*       │ quality_guard.py     │
└─────────────────────┘                        └──────────┬───────────┘
        ▲  admin login (rank only)                         │
        │                                                  │ POST rotate
┌───────┴────────┐                              ┌──────────▼──────────┐
│ Admin API      │                              │ session-rotator     │
│ assign accounts│                              │ host-only endpoint  │
└────────────────┘                              └─────────────────────┘
```

| 组件 | 值 |
|------|-----|
| Compose service | `egress-quality-guard` |
| Container | `grok2api-egress-quality-guard` |
| Image | `grok2api-egress-quality-guard:3.1.0-official` |
| 入口脚本（bind-mount） | repo `quality_guard.py` → 容器内 `/usr/local/bin/grok2api-egress-quality-guard` |
| Network | **`network_mode: host`**（用于访问宿主机上的 session-rotator 与仅内网开放的 Grok2API） |
| 状态卷 | docker volume `quality_guard_state` → `/var/lib/grok2api-quality-guard/` |
| 依赖 | `grok2api` healthy + `grok-session-rotator` healthy |
| 主服务配置开关 | `config.yaml` → `qualityGuard.enabled: true` |

**UI 页** `<admin-base-url>/quality-guard` 是 grok2api 前端 SPA，**不是** sidecar 自己的 HTTP 服务。页面通过 admin 会话读写主服务的 qualityGuard 设置，并展示节点/审计；sidecar 本身无对外端口。实际 host、IP 和端口只从运行配置读取，不写入本文。

---

## 3. 配置分层（必读，否则会改错地方）

配置有 **三层**，优先级从高到低：

| 层 | 路径 / 来源 | 作用 |
|----|-------------|------|
| **A. runtime-config** | 容器 `/var/lib/grok2api-quality-guard/runtime-config.json` | Admin UI 热改的 **子集**；sidecar 每 loop 检测 mtime 热加载 |
| **B. bootstrap** | 同目录 `bootstrap.json`（由 grok2api 根据 `config.yaml` 写出） | 完整策略 + **internal_token**；进程启动 `Config.from_bootstrap()` |
| **C. env / compose** | `quality-guard.env` + `docker-compose.yml` environment | `GROK2API_BASE_URL`、admin 账密、**RANK_*** 排序调度、部分历史 knobs |

### 3.1 runtime-config 允许字段（仅这些）

代码常量 `RUNTIME_CONFIG_FIELDS`：

```text
mode
active_interval_seconds
passive_poll_seconds
soft_tps
hard_tps
consecutive_soft
consecutive_errors
quarantine_seconds
min_healthy_nodes
```

**生产当前 runtime（2026-08-11 实测）：**

```json
{
  "version": 1,
  "settings": {
    "mode": "passive",
    "active_interval_seconds": 1800,
    "passive_poll_seconds": 5,
    "soft_tps": 200,
    "hard_tps": 1000,
    "consecutive_soft": 2,
    "consecutive_errors": 2,
    "quarantine_seconds": 30,
    "min_healthy_nodes": 3
  }
}
```

### 3.2 重要差异：soft_tps 以 runtime 为准

| 来源 | soft_tps |
|------|----------|
| `config.yaml` / bootstrap | **500** |
| `quality-guard.env` | **500** |
| **runtime-config（生效）** | **200** |
| `state.json` → `guard.soft_tps` | **200** |

Agent 改阈值时：
- 只改 `config.yaml` **不会**立刻覆盖 runtime 已有的 soft_tps；
- Admin UI 改 quality guard 会写 runtime-config；
- 若要强制与 yaml 对齐，需同步更新 runtime 或删掉 runtime 让 bootstrap 接管（确认 UI 不会立刻写回）。

### 3.3 bootstrap 完整字段（config 段）

| 字段 | 当前值 | 含义 |
|------|--------|------|
| `model` | `grok-4.5` | active quality-test 使用的模型 |
| `mode` | `passive` | 见 §4 |
| `node_ids` | 以 bootstrap 为准 | 守护范围；具体节点 ID 属于部署信息，不写入文档 |
| `rotatable_node_ids` | 以 bootstrap 为准 | 仅允许具备会话换 IP 能力的 sticky 节点调用 session-rotator |
| `active_interval_seconds` | 1800 | active 周期（mode 含 active 时） |
| `passive_poll_seconds` | 5 | 拉 request-audits 间隔 |
| `soft_tps` / `hard_tps` | 500 / 1000（bootstrap）；runtime soft=200 | 见分类 §5 |
| `consecutive_soft` | 2 | 仅在 `fail_closed=false` 时作为 active soft 累计阈值；当前 `fail_closed=true` 时单次 active soft 即可隔离 |
| `consecutive_errors` | 2 | 连续 probe 错误才隔离 |
| `quarantine_seconds` | 30 | 隔离后的恢复截止/重试间隔；不是所有异常都必须等待满 30 秒，部分路径会立即恢复探针 |
| `no_account_backoff_seconds` | 300 | 节点无账号可探针时退避 |
| `min_healthy_nodes` | 3 | fail_closed=false 时保底健康数；fail_closed=true 时不拦隔离 |
| `max_output_tokens` | 384 | active probe 上限 |
| `fail_closed` | **true** | soft 也当硬故障处理（passive 上 soft 直接 quarantine） |
| `min_generation_ms` | 1000 | 防「缓冲爆发」假高速 |
| `rotation_url` | `<session-rotator-url>` | sticky 换 IP；实际内网地址从 bootstrap 读取 |
| `rotation_timeout_seconds` | 45 | |
| `prompt` / `expected` | 16 行分布式 + 结尾 `QUALITY_OK` | active 探针内容与期望 marker |

`bootstrap.json` 还含 `enabled`、`version=1`、`internal_token`（**密钥，勿日志全量打印**）。

### 3.4 环境变量（compose / quality-guard.env）

**对接主服务：**

| Env | 当前 | 说明 |
|-----|------|------|
| `GROK2API_BASE_URL` | `<internal-grok2api-base-url>` | host 网络下访问仅内网开放的 Grok2API |
| `GROK2API_ADMIN_USERNAME` | `<admin-username>` | **仅 rank 账号迁移**；真实用户名不写入文档 |
| `GROK2API_ADMIN_PASSWORD` / `_FILE` | password file bind | rank 用 admin login |

**RANK 调度（仅 env，不进 runtime-config schema）：**

| Env | 当前 | 说明 |
|-----|------|------|
| `RANK_SCHEDULER_ENABLED` | `true` | 开 TTFT 排序 + 目标份额 |
| `RANK_DRY_RUN` | **`false`** | **会真实搬账号** |
| `RANK_INTERVAL_SECONDS` | 120 | 两次 rank 最小间隔 |
| `RANK_EWMA_ALPHA` | 0.3 | TTFT EWMA |
| `RANK_FLOOR_SHARE` | 0.03 | 每节点最低份额 |
| `RANK_MAX_SHARE` | 0.18 | 单节点上限份额 |
| `RANK_MAX_MOVES` | 30 | 单轮最多迁移账号数 |
| `RANK_MAX_MOVE_PCT` | 5 | 单轮最多迁 total_auto 的 5% |
| `RANK_MIN_SAMPLES` | 3 | 样本不足 → under_sampled，score=0 |

历史 env（`QUALITY_GUARD_*`）在官方 bootstrap 路径下 **多数已由 bootstrap/runtime 覆盖**；compose 仍注入是为兼容/文档，**以 bootstrap+runtime 与 state.guard 为准**。

---

## 4. 运行模式

| mode | passive 拉审计 | active 定点探针 |
|------|----------------|-----------------|
| `passive` | ✅ 每 `passive_poll_seconds` | ❌（除恢复/确认路径） |
| `active` | ❌ | ✅ 每 `active_interval` ± jitter |
| `hybrid` | ✅ | ✅ |

**当前生产：`passive`。**

主循环（`main`）：

1. 热加载 runtime-config
2. passive 到期 → `run_passive_cycle()` → 末尾 `run_rank_cycle()`
3. active 到期（若 mode 允许）→ `run_active_cycle()` → `run_rank_cycle()`
4. sleep ≤1s

CLI：

```bash
# 容器内
python3 /usr/local/bin/grok2api-egress-quality-guard --check-config
python3 /usr/local/bin/grok2api-egress-quality-guard --once
```

单实例锁：`guard.lock`（fcntl 排他）。

---

## 5. 质量分类算法

### 5.1 Passive 审计 `classify_audit`（主路径）

只认：

- `provider == grok_build`
- `streaming == true`
- HTTP 2xx 且无 `errorCode`
- 有 `firstTokenMs`
- `output_tokens >= 32` 且 `generation_ms = durationMs - firstTokenMs > 0`

计算：

```text
speed = output_tokens * 1000 / generation_ms   # tokens/s
```

判定顺序：

| 条件 | class | reason |
|------|-------|--------|
| fail_closed **且** generation_ms < min_generation_ms **且** speed ≥ soft_tps | **hard** | `buffered_burst`（短窗假高速） |
| speed ≥ hard_tps | **hard** | `hard_tps` |
| speed ≥ soft_tps | **soft** | `soft_tps` |
| 否则 | **healthy** | `within_threshold` |
| 不满足前置 | **ignored** | `not_build_stream` / `unsuccessful` / … |

**生效阈值（runtime）：** soft=**200** TPS，hard=**1000** TPS，min_generation_ms=**1000**。

### 5.2 Active 探针 `classify_result`

对 internal `POST .../egress-nodes/{id}/quality-test` 返回：

- 无 expected marker → soft `expected_marker_missing`
- output_tokens < 32 → soft
- fail_closed 且 generation 过短 → soft `insufficient_generation_window`
- speed ≥ hard / soft → hard / soft
- 否则 healthy

### 5.3 Passive 异常后动作（`_record_passive_audit`）

| class | 行为 |
|-------|------|
| healthy | 清 `passive_soft_strikes` |
| soft | strikes++；**若 fail_closed → 直接 quarantine**；否则 active 确认探针 |
| hard | **立即 quarantine** |

因当前 `fail_closed=true`，**soft ≈ hard（都会隔离）**。

### 5.4 Active 异常后动作

- probe 抛 `egressQualityProbeNoAccount` → 不隔离，写 `quarantined_until` 退避 `no_account_backoff_seconds`
- 其它错误：`error_strikes++`，scheduled 且 ≥ consecutive_errors → quarantine
- hard → quarantine
- soft 且 `fail_closed=true` → 单次即 quarantine
- soft 且 `fail_closed=false` → `active_soft_strikes >= consecutive_soft` 后 quarantine

---

## 6. 隔离 / 恢复 / 换 IP

### 6.1 Quarantine

1. `_can_quarantine`：
   - `fail_closed=true` → 只要节点当前 enabled 就可隔离（**可把最后几个节点也关掉**）
   - `fail_closed=false` → 隔离后 enabled 数仍 ≥ `min_healthy_nodes`
2. 状态：`disabled_by_guard=true`，`quarantined_until=now+quarantine_seconds`，清 strikes。该时间是恢复调度截止点，不保证节点一定保持禁用到截止时间
3. **先 save state** 再 `PATCH` internal `egress-nodes/batch` `enabled=false`（崩溃可对账）
4. 不删账号绑定

### 6.2 是否 rotate

`_should_rotate` 需同时：

- 配置了 `rotation_url`
- node_id ∈ `rotatable_node_ids`（仅具备 session rotation 能力的 sticky 节点）
- reason ∈ {hard_tps, soft_tps, buffered_burst, expected_marker_missing, insufficient_*, probe_errors, recovery_*, rotation_error}

**非 rotatable 的托管住宅节点不会换 IP**，只执行 disable→probe→enable。

### 6.3 Recovery

- `buffered_burst`：立即 `_recover_quarantined(rotate=False, rotate_on_failure=True)`
- 其它可 rotate：立即 rotate+probe
- 冷静期结束：`_probe_quarantined` 再 recover

因此 `quarantine_seconds=30` 不是统一的最短隔离时长：立即恢复探针成功时可提前 enable；探针或 rotation 失败时则把 `quarantined_until` 向后延长。

恢复成功条件：quality_test → **healthy** → `set_enabled(true)`，清 `disabled_by_guard`。

失败：延长 `quarantined_until`，记 `quarantine_extended`。

### 6.4 受保护节点

`GET .../egress-operations` 里 `fallbacks.*.mode == fixed` 的 nodeId 为 **fixed fallback**，正常不纳入 eligible（除非本 guard 已持有 disabled_by_guard 所有权，避免配置变更导致永久死节点）。

---

## 7. Rank 调度（本地扩展）

在每次 passive/active cycle 末尾调用 `run_rank_cycle`（受 `RANK_INTERVAL_SECONDS` 节流）。

### 7.1 分数

```text
ewma_ft = α * sample + (1-α) * prev     # firstTokenMs
penalty = max(0.4, 1 - 0.15 * soft_strikes)
if last_classification == hard: penalty *= 0.5
if samples < min_samples or ewma<=0:
    score = 0, under_sampled = true
else:
    score = (1 / ewma_ft) * penalty     # 越快越高
```

排序：score 降序 → ewma 升序 → node_id。

### 7.2 目标账号份额

- 每节点至少 `floor_share`（默认 3%，且 ≤ 0.9/n）
- 剩余 residual 按 score 加权
- 单节点 cap `max_share`（18%）
- 整数 target 用最大余额法分配

### 7.3 迁移

- Admin：`list_auto_account_ids_by_node` + `assign_accounts(dest, ids, mode=auto)`
- 从「超标 donor」挪到「不足 receiver」
- 单轮先生成 donor→receiver 账号列表，再按代码截断：`pct_cap = int(total_auto * MAX_MOVE_PCT / 100)`；初始 `cap = RANK_MAX_MOVES`；仅当 `pct_cap > 0` 时才取 `min(cap, max(1, pct_cap))`
- **`RANK_DRY_RUN=false` → 真实迁移**（生产已开）

日志事件：`rank_table`、`rank_move_applied`、`rank_move_failed`。

边界说明：当 `total_auto * MAX_MOVE_PCT < 1` 时，`pct_cap` 向下取整为 0，代码会跳过百分比上限，单轮仅受 `RANK_MAX_MOVES` 和实际 donor/receiver 数量限制。因此不能把上面的实现简写成无条件的 `min(...)`。

`rank_move_applied.assigned` 是 Admin 批量 assign 响应中的整批数量，并会重复写到该批每个账号事件上；它不是“该账号迁移 1 次”的计数，不能对该字段直接求和。当前 API 若返回部分成功但不返回具体成功账号，sidecar 也无法从事件中精确还原成功子集；这些日志适合追踪计划和调用结果，不应作为严格迁移账本。

---

## 8. Internal API（sidecar ↔ grok2api）

前缀：`/api/internal/v1/quality-guard`
鉴权：`Authorization: Bearer <bootstrap.internal_token>`

| 方法 | 路径 | 用途 |
|------|------|------|
| GET | `/egress-nodes?scope=grok_build` | 列节点 |
| GET | `/egress-operations` | fixed fallback 集合 |
| GET | `/request-audits?pagination=cursor&period=24h` | passive 审计流 |
| POST | `/egress-nodes/{id}/quality-test` | active / recovery 质量探针 |
| POST | `/egress-nodes/{id}/test` | 连通性探针 |
| PATCH | `/egress-nodes/batch` | `{ids, enabled}` 隔离/恢复 |

Admin（rank only）：

| 方法 | 用途 |
|------|------|
| POST `/api/admin/v1/auth/login` | 取 accessToken |
| 账号按节点列表 + assign | 见 `AdminApiClient` |

Rotator：

```http
POST <session-rotator-url>
Authorization: Bearer <rotation_token>
{"nodeId":"<id>","oldExitIp":"<ip>"}
```

要求响应 `changed: true`。

---

## 9. 状态文件

目录：`/var/lib/grok2api-quality-guard/`（volume）

| 文件 | 说明 |
|------|------|
| `bootstrap.json` | 主服务写入的完整配置 + internal_token |
| `runtime-config.json` | UI 热配置子集 |
| `state.json` | 运行态：nodes / statistics / ranking / events |
| `guard.lock` | 单实例锁 |

### state.json 关键结构

```text
version
started_at / updated_at
last_passive_poll_at / last_active_cycle_at
passive_initialized
guard { 生效配置快照，含 soft_tps 等 }
nodes {
  "<id>": {
    disabled_by_guard, quarantined_until,
    last_classification, last_reason, last_source,
    last_output_tps, last_first_token_ms, last_duration_ms,
    passive_soft_strikes, active_soft_strikes, error_strikes,
    ewma_first_token_ms, ewma_samples,
    rank_position, rank_score, target_share, target_accounts,
    last_rotation_at, rotation_failures, ...
  }
}
statistics { active{}, passive{}, actions{quarantined,restored,suppressed} }
ranking { last_run_at, dry_run, last_table[], last_moves[] }
recent_events[]
seen_audit_ids  # 去重游标相关
```

**读状态：**

```bash
docker exec grok2api-egress-quality-guard cat /var/lib/grok2api-quality-guard/state.json
docker logs grok2api-egress-quality-guard --since 30m
```

---

## 10. 出口节点角色（与 Guard 相关）

### 10.1 配置来源与角色

| 角色 | 配置来源 | rotatable |
|------|----------|-----------|
| Sticky 住宅分片 | `node_ids` 与 `rotatable_node_ids` 的交集 | **是** |
| 托管住宅出口 | `node_ids` 中未列入 `rotatable_node_ids` 的节点 | 否 |
| Fixed fallback | internal `egress-operations` 中声明为 fixed 的节点 | Guard 正常不纳入 eligible |

历史 env 可能包含已过期或更宽的节点范围；**始终以 bootstrap 与 `state.guard` 的当前值为准**。具体节点 ID、出口名称和内网地址不写入本文。

### 10.2 调度实况的读取原则

不要在长期文档中固化当前 enabled 状态、节点 ID、出口供应商名称、账号分布或累计统计。排障时临时读取 Admin 节点列表和 `state.json`，只在当前任务中给出脱敏摘要。

典型流程可抽象为：passive 发现 `buffered_burst`（TPS 虚高 + 生成窗过短）→ quarantine → probe → restore。具体持续时间与结果必须以当次事件为准。

---

## 11. 与主服务 `config.yaml` 对齐片段

```yaml
qualityGuard:
  enabled: true
  model: "grok-4.5"
  mode: passive
  activeInterval: 30m
  passivePollInterval: 5s
  softTPS: 500          # 注意：runtime 可能覆盖为 200
  hardTPS: 1000
  consecutiveSoft: 2
  consecutiveErrors: 2
  quarantineDuration: 30s
  noAccountBackoff: 5m
  minimumHealthyNodes: 3
  maxOutputTokens: 384
  failClosed: true
  minimumGenerationWindow: 1s
  rotationURL: "<session-rotator-url>"
  rotationToken: "<redacted>"
  rotationTimeout: 45s
  nodeIDs: [<deployment-node-ids>]
  rotatableNodeIDs: [<rotatable-sticky-node-ids>]
```

主服务镜像：`grok2api:3.1.0-precool-480k-20260804`（上游 v3.1.0 + 本地 free quota precool 等，见 `LOCAL_PATCHES.md`）。

---

## 12. Agent 操作手册

### 12.1 只读体检

```bash
# 进程
docker ps --filter name=grok2api-egress-quality-guard
docker logs grok2api-egress-quality-guard --since 1h 2>&1 | tail -100

# 生效 soft_tps / mode
docker exec grok2api-egress-quality-guard \
  python3 -c "import json; s=json.load(open('/var/lib/grok2api-quality-guard/state.json')); print(s['guard']); print(s['statistics'])"

# 节点 enabled
# 用 admin login 后 GET /api/admin/v1/egress-nodes?scope=grok_build
```

### 12.2 改策略（推荐顺序）

1. **临时阈值（soft/hard/mode/间隔）** → Admin UI `/quality-guard`（写 runtime-config）
2. **节点范围 / rotation / model / failClosed** → 改 `config.yaml` `qualityGuard`，重启或触发主服务重写 bootstrap，**再** `docker compose up -d egress-quality-guard`
3. **Rank 开关与幅度** → compose environment `RANK_*`，`docker compose up -d egress-quality-guard`
4. **代码逻辑** → 改 host `./quality_guard.py`（已 bind-mount），**重启 sidecar** 加载

### 12.3 紧急停火

| 目标 | 做法 |
|------|------|
| **立即停止全部 Guard 动作** | `docker compose stop egress-quality-guard`。这是无需等待配置重载的直接停火方式；不会自动恢复已禁用节点 |
| 持久关闭全部 Guard | 把 `qualityGuard.enabled` 改为 `false`，确认 grok2api 已重写 `bootstrap.json`，再重启/重建 sidecar。`enabled` 只在 `Config.from_bootstrap()` 启动阶段读取，单改 YAML 不会让正在运行的 sidecar 立即退出 |
| 只停搬号 | 把 `RANK_DRY_RUN=true` 或 `RANK_SCHEDULER_ENABLED=false` 写入 Compose 环境，然后执行 `docker compose up -d --no-deps egress-quality-guard`。`RANK_*` 只在 `Guard.__init__()` 时读取，不支持 runtime-config 热加载 |
| 手动恢复节点 | 先确认并处理 `state.nodes[id].disabled_by_guard` 所有权，再通过 Admin enable。当前 `fail_closed=true` 时，仅在 Admin 把节点设为 enabled，Guard 会再次 disable 并要求恢复探针；直接改 state 风险较高 |
| 回滚脚本 | 用 `quality_guard.official-3.1.0.py` 替换 bind-mount，或去掉 rank 相关 env（见 `LOCAL_PATCHES.md`） |

`mode` 的 runtime 热更新只能在 active / passive / hybrid 之间切换检测来源，不能表达“全部停用”。关闭 Rank 也只停止账号再平衡，不会停止节点质量判定、隔离和恢复。

### 12.4 排障速查

| 现象 | 方向 |
|------|------|
| 节点频繁 quarantine | 看 `reason`：`buffered_burst`→代理缓冲/假流式；`hard_tps`→真加速异常；对照 soft_tps=200 是否过严 |
| quarantine 不生效 | fail_closed=false 且已触 min_healthy；或 fixed fallback 保护 |
| 从不 rotate | 节点不在 `rotatable_node_ids`；或该出口类型设计为不可换 IP |
| rank 不搬号 | dry_run / 无 admin 密码 / interval 未到 / 无 eligible；修改 `RANK_*` 后未重建 sidecar也会继续使用旧值 |
| probe no account | 节点上无可用账号；backoff 300s |
| bootstrap missing | 主服务 qualityGuard 未启用或 volume 权限；重启 grok2api |
| 改 yaml soft 不生效 | runtime-config 仍锁 soft_tps=200 |

### 12.5 密钥红线

- **不要**把 `internal_token`、`rotation_token`、admin password 写入对外文档或 commit
- 本文件已脱敏；真实值可能存在于 host `config.yaml`、`quality-guard.env`、`admin-password.txt`、volume `bootstrap.json`，以及容器环境中
- 当前部署的 `quality-guard.env` 与 `admin-password.txt` 权限可能允许同机其他用户读取；排障时只检查变量是否存在，不要打印完整容器环境或文件内容。需要收紧权限时应作为单独运维变更评估，不在只读体检中顺手修改

---

## 13. 关键源码与文档路径

| 路径 | 内容 |
|------|------|
| `quality_guard.py` | 生产 sidecar（含 rank 扩展） |
| `quality_guard.official-3.1.0.py` | 上游基线备份 |
| `quality-guard.env` | 历史/辅助 env |
| `config.yaml` | `qualityGuard:` |
| `docker-compose.yml` | service `egress-quality-guard` |
| `EGRESS.md` | 出口链路角色与运维边界 |
| `LOCAL_PATCHES.md` | 镜像与 guard 补丁说明 |
| `rank-eval/` | rank 评估产物 |
| `QUALITY_GUARD.md` | 本文 |

核心函数索引（quality_guard.py）：

| 符号 | 职责 |
|------|------|
| `Config.from_bootstrap` | 启动配置 |
| `load_runtime_config` / `RuntimeConfigReloader` | 热更新 |
| `classify_audit` / `classify_result` | 质量判定 |
| `ApiClient.*` | internal + rotate |
| `AdminApiClient.*` | rank 搬号 |
| `Guard._quarantine` / `_recover_quarantined` | 隔离恢复 |
| `Guard._record_passive_audit` | passive 主路径 |
| `Guard._probe_active` | active 探针 |
| `Guard.run_rank_cycle` | TTFT 再平衡 |
| `main` | 事件循环 |

---

## 14. 设计原则（给 agent 的决策约束）

1. **只 disable 节点，不删绑定** — 恢复后账号仍在。
2. **Guard 只管理自己 disable 的节点** — `disabled_by_guard` 所有权。
3. **fail_closed 优先可用性风险** — 宁可少节点，也不放行疑似假高速。
4. **Rotate 仅 sticky** — 住宅会话可换 IP；kookeey 只靠 disable/enable。
5. **Rank 是增强，不是安全边界** — 可关 dry-run；质量判定不依赖 rank。
6. **改配置先分清 runtime vs bootstrap vs RANK env** — 否则「改了不生效」。

---

## 15. 变更记录（运维视角）

| 日期 | 事项 |
|------|------|
| 2026-07 | 上游 PR #837 quality guard 进入 v3.1.0 |
| 2026-08-02+ | host-network sidecar + session-rotator |
| 2026-08-05 | Kookeey 节点入 watch；TTFT rank 上线，`RANK_DRY_RUN=false` |
| 2026-08-05 | 镜像切 `3.1.0-official`，脚本 bind-mount 保留 rank |
| 2026-08-11 | 文档化扫描；runtime soft_tps=200（严于 yaml 500）；mode=passive；rank 活跃 |
| 2026-08-12 | 复核文档语义：区分快照与实时状态，澄清 soft 隔离、恢复时序、Rank cap、日志计数和停火生效条件 |

---

*本文根据部署配置、sidecar 源码、状态结构和管理接口整理；主机地址、凭据、节点清单与运行日志均已脱敏。*
