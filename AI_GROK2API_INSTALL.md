# 丢家宽给 AI：装成 Grok2API lab 同款

**主路径是本 fork（lij768423-svg/grok2api）+ Quality Guard sidecar。不要装 CPA，不要只开一个节点，不要 pull 官方 latest 镜像。**

用户只需要把全部家宽 sticky 丢给你。你负责：拆 session → Mihomo 每条一个 listener → Grok2API 每条一个 `fixed` 节点 → 打开 Guard。

这个 fork 开箱即用：官方最新 + **缺思考 / 假加密思考拦截默认开**。`docker compose up -d --build` 会带 sidecar。

---

## 0. 用户复制这段发给任何 AI

```text
按这个文档装，不要发挥：
https://github.com/lij768423-svg/grok2api/blob/main/AI_GROK2API_INSTALL.md

主路径：lij768423-svg/grok2api（fork）+ Quality Guard sidecar。不要装 CPA。
不要 pull ghcr.io/chenyme/grok2api:latest，必须 clone 本 fork 再 docker compose up -d --build。

这个 fork 开箱即用：官方最新 + 缺思考拦截默认开。
- hold 30s / minOutput 8 / 6 枪 / fail_closed
- 短 encrypted_content stub 不算思考；floor = max(256B, reasoning_tokens×4)
- hold 到期后的短问候 + 高 reasoning（「你好」）继续扣；floor 达标但 1 秒内吐短回复也扣
- TUI 续聊（previous_response_id）和 hosted tools 仍 hold，扣住后换号
- 缺思考冷却 12h，空流 15m；docker compose up -d 带 sidecar

家宽全部用上，每个 sticky 一个 Mihomo listener + 一个 Grok2API 节点。
禁止只开 1 个交差。禁止把多条合成一个「住宅池」。

机器：Linux + Docker，装到 ~/grok-stack（新目录，别覆盖现网）。
有邮箱再一起装注册机；没有也行，先把出口和 Guard 拉起来。

家宽（一行一条，URL / host:port:user:pass / 带 sid 都行）：

```

用户在后面贴家宽。贴完你就开干，不要再问「要不要先单节点验证」。

---

## 1. 你先读什么

1. 本文（落地顺序）
2. 本仓库 `config.example.yaml`（`requestRetry` 已开）
3. 本仓库 `tools/egress-quality-guard/README.zh-CN.md`
4. 家宽脚本：本仓库 `scripts/from_residential.py`

不要走 CPA 插件。那是旁路，不是交付。

---

## 2. 用户只需要给这些

| 必给 | 说明 |
| --- | --- |
| 一台能跑 Docker 的 Linux | 新目录，默认 `~/grok-stack` |
| **全部家宽** | 每一条 sticky 都贴出来 |

| 有就给，没有就先空着 | 说明 |
| --- | --- |
| 邮箱 | 要注册机才要。只想先把出口和 Guard 拉起来可以后补 |
| 已有 Grok2API | 说路径。没说 = 按本 fork 新建，禁止改别人现网 compose |

家宽一行一条，下面都能认：

```text
http://USER:PASS@HOST:PORT
socks5://USER:PASS@HOST:PORT
USER:PASS@HOST:PORT
HOST:PORT:USER:PASS
节点名 | http://USER:PASS@HOST:PORT
http://ACCOUNT-region-US-sid-XXXX-t-10:PASS@HOST:PORT
```

**同一 host:port、不同 username / sid = 不同 session = 不同节点。**
用户写「我买了 8 条」但只贴 1 条 URL：停下来让他把 sid 列表或 8 行都贴全。不准自行复制成 8 个假节点。

聊天里解析完只回：名称、listener 端口、出口 IP。不要回显完整代理 URL / 密码。

---

## 3. 硬性规则（违反 = 没装完）

1. 用户贴了 N 条家宽，交付必须有 **N 个独立 listener + N 个 Grok2API 节点**。
2. 使用侧节点数 **< 3** 只能叫冒烟，汇报里必须写「不像 lab」。
3. 使用侧 **≥ 3** 才允许说接近 lab：Guard `passive` + `failClosed: true` + `softTPS: 200`。
4. 禁止：只加 `res-01`、把 8 条 sticky 合成一个节点、`proxyPool=true` 套在 sticky 上、阶段 A 停住交差。
5. 禁止：Grok2API 在 bridge 网络里填 `http://127.0.0.1:端口`（那是容器自己）。用 `network_mode: host`，或 `host.docker.internal` / 宿主机网关。
6. 注册口和使用口不要抢同一条 sticky。脚本会在 N≥4 时自动拆。
7. 不要把 CPA `.so`、8317、商店插件写进交付说明。
8. 不要 `docker pull ghcr.io/chenyme/grok2api:latest`。官方镜像 **默认不拦截**，也没有本 fork 的密文 floor / burst / 假加密思考拦截。必须 `--build` 本仓库。

---

## 4. 落地顺序（不可跳）

### A. 解析家宽

把用户原文写到本机文件（0600），不要进 git：

```bash
umask 077
mkdir -p ~/grok-stack/egress-gen
# 用户家宽 → ~/grok-stack/residential.dump   chmod 600

git clone --depth 1 \
  https://github.com/lij768423-svg/grok2api.git \
  ~/grok-stack/grok2api

python3 ~/grok-stack/grok2api/scripts/from_residential.py \
  ~/grok-stack/residential.dump \
  --out-dir ~/grok-stack/egress-gen
```

看 `egress-gen/plan.md`。`lab_like: false` 就告诉用户再补 sticky，但仍把已有的全部开成节点。

### B. 起 Mihomo

用生成的 `egress-gen/mihomo.yaml` 起一个**新的** Mihomo，不要改用户现网 `mihomo-grok-*`。

对每个 listener 测 3 次出口 IP：

```bash
curl -s --max-time 20 --proxy http://127.0.0.1:8301 https://api.ipify.org
```

记到表里（只记端口和 IP）。sticky 三次应相同。两个「节点」出口 IP 相同 = 还是一个故障域，标出来，不要假装拆开了。

### C. 起 Grok2API + Guard

```bash
cd ~/grok-stack/grok2api
./scripts/bootstrap-lab-config.sh
# 或：cp config.example.yaml config.yaml 后自己填 secrets / bootstrapAdmin
```

`config.example.yaml` 已经是 lab 默认，**不要改回官方的 `requestRetry.enabled: false` / `minOutputTokens: 32`**。核对：

```yaml
qualityGuard:
  enabled: true
  model: "grok-4.6"
  mode: passive
  requestRetry:
    enabled: true
    maxAttempts: 6
    holdTimeout: 30s
    minOutputTokens: 8
    onExhausted: fail_closed
    accountCooldown: 12h
    idleAccountCooldown: 15m
    minEncryptedBytes: 256
    encryptedBytesPerReasoningToken: 4
```

使用侧 ≥3 时，把 Guard 调到接近 lab（拦截参数保持上面这组）：

```yaml
qualityGuard:
  softTPS: 200
  failClosed: true
  minimumHealthyNodes: 2   # 脚本 guard.json 为准；≥4 用 3
```

使用侧 <3 时：`failClosed: false`、`softTPS: 500`，汇报写「不像 lab」。

Compose（编本 fork，不要 pull 官方 latest）：

```bash
docker compose up -d --build
```

本 fork **不用** `--profile quality-guard`：sidecar 默认就起。

sidecar 环境（lab 同款，第一天 Rank 先 dry-run）：

```text
RANK_SCHEDULER_ENABLED=true
RANK_DRY_RUN=true
```

有一天真实流量、节点都 healthy 之后，再问用户一句，才把 `RANK_DRY_RUN=false`。

### D. 每个使用侧 listener 建一个节点

管理端 Egress，scope=`grok_build`：

| 字段 | 值 |
| --- | --- |
| 名称 | `plan.md` 里的 use 名 |
| 代理 URL | Mihomo listener（注意 Docker 可达地址） |
| `proxyPool` | **false** |

注册侧 listener 只进注册机代理池，**不要**建成使用侧 Guard 节点。

`QUALITY_GUARD_NODE_IDS` 留空，让 sidecar 自动发现全部已启用 Build 节点。

### E. 注册机（用户给了邮箱才做）

```bash
# 或 grok-fullchain ./deploy/one-click.sh
# 面板代理池只填 8201+ 注册口，不要填 8301+ 使用口
```

号出来后：

```bash
python3 ~/grok-fullchain/deploy/import_to_grok2api.py \
  --auth-dir ~/grok-stack/grok-register-panel/cpa_auth \
  --also-dir ~/grok-stack/grok-register-panel/grok2api_auth \
  --url http://127.0.0.1:8000 \
  --password-file ~/grok-stack/grok2api/admin-password.txt \
  --assign-nodes <全部使用侧 node id>
```

目录名 `cpa_auth` 只是历史文件名。使用端仍是 Grok2API。

---

## 5. 什么叫装完（你必须自己跑）

- [ ] `from_residential.py` 的 session 数 = 用户贴的条数
- [ ] Mihomo listener 数相同；每个口 `curl --proxy` 有公网 IP
- [ ] Grok2API 管理端能登录
- [ ] `/quality-guard` 打得开
- [ ] 使用侧节点数 = 使用侧 listener 数，且 `proxyPool=false`
- [ ] 使用侧 ≥3 时：`failClosed=true`、`softTPS=200`、sidecar 在跑
- [ ] `requestRetry.enabled=true`、`holdTimeout=30s`、`minOutputTokens=8`、`minEncryptedBytes=256`
- [ ] 没有安装 CPA 插件，交付说明里没有 8317
- [ ] 聊天里没有完整代理 URL / admin 密码 / token

缺一条就继续做，不要说「先这样用着」。

---

## 6. 对用户怎么说话

开场只问家宽和机器，不要丢 A/B/C 问卷。

收工汇报用这张表：

| 项 | 值 |
| --- | --- |
| Grok2API | URL（常见 `:8000` / `:8181`） |
| Guard | `/quality-guard`；mode / failClosed / softTPS |
| 拦截 | requestRetry 开；hold 30s；floor 256B / ×4；burst / 假加密 dump / cipher-drool 开 |
| 家宽 | 用户贴了 N 条 → 使用侧 x 个 + 注册侧 y 个 |
| 出口 IP | 端口 → IP；标出重复 IP |
| 像不像 lab | 使用侧 ≥3 且 Guard 已按本文打开 = 接近；否则写原因 |
| Rank | dry-run 中（要关再改 env） |
| 还缺 | 邮箱 / 账号导入 / 重复 IP 的 session |

---

## 7. 不要做

- 不要把官方 `chenyme/grok2api` 当可运行交付：官方 PR 同参数但 **默认不启动拦截**
- 不要把 `RECOMMENDED_DEPLOYMENT` 的「阶段 A：只接一条」当成交付
- 不要问「1024 还是 Kookeey」当分类；问三次 IP 是否相同、yaml 里 `server:` 是什么
- 不要改用户已经在跑的 Grok2API / Mihomo
- 不要把供应商账密写进 grok2api 节点（只写 Mihomo listener）
- 不要在 Issue / 截图 / 聊天回显 `residential.dump` 或 `mihomo.yaml` 里的 username/password
- 不要把 `encrypted_content != ""` 或 `usage.reasoning_tokens` 当成思考证据
- 不要把 holdTimeout 改回 3s（会把 grok-4.6 流末尾密文误判成缺思考）
