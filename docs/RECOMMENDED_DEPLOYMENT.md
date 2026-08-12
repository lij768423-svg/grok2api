# 推荐出口部署方式

本文给 Grok2API 部署者和 AI 工具一条可复用的落地路径：

```text
住宅/家宽或 Resin
        │
        │  可选：加密中转/Relay，只负责把链路送到出口服务
        ▼
Mihomo 出口编排层
        │
        ├─ 固定 sticky 会话 -> 独立监听器 -> 固定出口节点
        └─ Resin 动态池     -> 代理池监听器 -> 每连接/每隧道轮换
        ▼
Grok2API 或 CPA 出口节点
        │
        ├─ 账号按节点绑定
        └─ Quality Guard / CPA Guard 观察、摘流、迁号、轮换、复测
```

核心原则是：**Mihomo 负责把上游代理整理成可管理的出口，应用负责账号到出口的绑定，Guard 负责质量判定和故障动作。** 不要让应用直接混用原始代理用户名，也不要把固定 sticky 会话和动态 Resin 池塞进同一个固定节点。

## 1. 先区分两类上游

| 上游 | 正确语义 | 应用中的节点类型 | IP 变化时机 |
| --- | --- | --- | --- |
| 家宽/住宅 sticky | 一个会话在一段时间内保持同一出口 | `fixed`，Grok2API 中 `proxyPool=false` | 会话失效、显式换会话或受信任轮换接口成功后 |
| Resin 动态池 | 新连接或新隧道可能获得不同出口 | `pool`，Grok2API 中 `proxyPool=true` | 建立新连接/新隧道时由池策略决定 |

判断标准不是供应商名字，而是**同一条代理 URL 是否保证同一出口**。测试时至少记录：连接是否成功、出口 IP、首字延迟、流式持续时间和连续多次连接的 IP 是否变化。

不要把“能访问外网”当成“适合长期承载 Grok 流式请求”。住宅出口还需要单独做真实模型质量测试。

## 2. 推荐的分层部署

### 2.1 上游接入层

把家宽和 Resin 的真实凭据只放在 Mihomo 的私密配置或 secret 管理中。仓库、README、截图和 AI 对话只使用占位符。

- 家宽 sticky：每个 session 作为一个独立 upstream。不要把多个 session 写成一个共享用户名，也不要先做随机负载均衡。
- Resin 动态池：把同一供应商、同一地区、相同风控属性的入口整理成一个池；若供应商已经按每连接轮换，不要再在应用层伪造 sticky。
- 不同供应商、不同地区或不同认证策略不要放进同一个池。这样出问题时无法判断是供应商、地区还是账号策略导致的。

### 2.2 可选中转层

当本地到住宅/Resin 的直连不稳定，或供应商只允许特定区域访问，可在上游和 Mihomo 之间增加加密 relay：

```text
应用 -> Mihomo -> 加密 relay -> 住宅/Resin -> 最终 residential exit IP
```

relay 只改善传输路径，**不应被当作最终住宅出口**。验证必须以最终 `exit_ip` 为准，而不是 relay 的地址。

中转层的边界：

- 只承载到供应商的连接，不在 relay 上再次做账号级轮换；
- 不要让所有 sticky session 依赖单个 relay，至少保留一个独立故障域或明确的回退路径；
- relay 变更不应覆盖 Mihomo 中其他 provider 的配置；
- 修改 relay、Mihomo 或供应商配置后，先做连通检测，再做真实模型质量检测。

如果直连已经稳定，不要为了“多一跳可能更快”盲目增加 relay。额外一跳通常会增加首字延迟；只有在直连丢包、握手失败或区域访问受限时才值得采用。

### 2.3 Mihomo 编排层

推荐把 Mihomo 按故障域拆成多个逻辑路径。可以是多个实例，也可以是一个实例中的多个独立 listener，但必须满足：

1. 每个 sticky session 都有自己的 listener 和 proxy group；
2. Resin 动态池有单独的 listener 和 `load-balance`/provider 组；
3. 固定路径和动态池不共用一个应用节点；
4. listener 的上游变更可以单独重载，不能一次重载把所有健康节点一起中断；
5. Mihomo 的 controller、配置文件和 provider 文件只允许受控的本机/私网调用。

逻辑结构应接近下面这样（这是结构示意，不是可直接提交的生产配置）：

```yaml
fixed_sessions:
  - name: residential-session-a
    upstream: <sticky-session-a>
    listener: <fixed-listener-a>
  - name: residential-session-b
    upstream: <sticky-session-b>
    listener: <fixed-listener-b>

dynamic_pools:
  - name: resin-pool-us
    upstreams: [<resin-entry-a>, <resin-entry-b>]
    strategy: load-balance-or-provider-defined-rotation
    listener: <pool-listener-us>
```

Mihomo 的池策略只解决“下一条连接走哪里”，不负责判断 Grok 模型质量，也不负责迁移账号。质量判定和账号迁移留给后方 Guard。

## 3. 接入 Grok2API 或 CPA

### 3.1 Grok2API

在管理端把 Mihomo listener 当作出口节点添加：

| 路径 | `proxyPool` | 账号绑定 | 说明 |
| --- | --- | --- | --- |
| sticky session listener | `false` | 一个节点一组 auto 账号 | 账号在会话周期内保持同一出口 |
| Resin pool listener | `true` | 只放可承受 IP 变化的 auto 账号 | 单次连接失败不应冷却整个动态池 |

建议所有节点使用同一个业务 scope（例如 `grok_build`），但每个物理故障域使用不同 node。不要把八条 sticky listener 合并为一个名为“住宅池”的节点，否则 Guard 只能整体摘流，无法定位坏 session。

账号分配：

- 先为每个固定节点分配少量 auto 账号，观察一轮完整流式请求；
- 确认出口 IP、质量和并发稳定后再扩容；
- 手工 sticky 绑定和自动 rebalance 分开管理；
- Guard 只迁移明确标记为 auto 的账号，不要把人工绑定当成可随意迁移的库存。

### 3.2 CPA 原生插件

CPA 使用相同的节点语义：

- 固定住宅：节点类型 `fixed`，每个 session 单独建节点；
- Resin 动态池：节点类型 `pool`，由 Mihomo 或供应商决定每连接轮换；
- `account_capacity` 只表示调度上限，不等于供应商承诺的并发能力；
- 添加节点后先“连通”，再“质量”，最后才执行重平衡。

CPA 插件不会自动猜测供应商换 IP API，也不会因为打开 `pool` 标记就修改 session。需要自动换 IP 时，必须配置**按节点 allowlist 限制**的受信任 rotation webhook。

## 4. 后方 IP 轮换应该怎么做

### 4.1 动态 Resin 池

动态 Resin 的推荐路径是“新连接轮换”，不是“中途换 IP”：

1. Mihomo 为池 listener 创建新隧道或选择下一个 provider 出口；
2. 应用把该节点标记为 `proxyPool=true`；
3. 单条连接失败只影响当前请求或当前隧道，不要把整个池置为坏节点；
4. 连续失败达到应用侧阈值时，再隔离具体 provider/listener 故障域；
5. 恢复时建立新隧道并验证最终出口 IP 和真实模型质量。

流式请求已经开始输出后不要强行切换 IP。正确处理是结束当前请求并让客户端重试，后续请求使用新隧道。

### 4.2 固定家宽 sticky

固定家宽需要“隔离 -> 轮换 -> 验证 -> 恢复”四步：

```text
异常样本
  -> Guard disable 目标节点
  -> 迁出 auto 账号
  -> 只调用该节点的 rotation webhook / session 更新
  -> 验证 new_exit_ip != old_exit_ip
  -> 真实模型质量 probe
  -> healthy 才 enable
```

轮换接口必须满足：

- 传入 node ID 和旧出口 IP，只作用于一个明确节点；
- 返回新出口 IP，且必须和旧值不同；
- 失败、超时、返回相同 IP 或返回无法解析的数据时，节点继续隔离；
- 不得重写其他 provider、其他 session 或整个 Mihomo 配置；
- rotation token 只能从 secret/env 注入，不能进入节点 URL、状态文件或日志。

### 4.3 轮换后不能只做连通检测

连通检测只能证明代理可以建立连接。恢复前必须按顺序验证：

1. 新旧出口 IP 确实不同；
2. 真实模型请求能返回完整流式结果；
3. 首字耗时和生成窗口没有明显异常；
4. 没有 401/403/429、额度或认证错误混入出口故障统计；
5. 小流量观察通过后再重平衡账号。

## 5. Quality Guard 的职责边界

Quality Guard 不是 Mihomo controller，也不是代理商 API 的替代品：

| 组件 | 负责 | 不负责 |
| --- | --- | --- |
| 供应商 | 提供家宽/Resin 出口和可选换 IP能力 | 应用账号迁移、模型质量判定 |
| Mihomo | upstream、分片、listener、池策略、热重载 | Grok 质量、账号 rebalance |
| Grok2API/CPA | 节点登记、账号绑定、请求调度 | 猜测供应商 session 语义 |
| Quality Guard | 被动/主动质量检测、摘流、恢复、可选 rotation webhook | 直接修改未 allowlist 的代理 |

推荐的 Guard 策略：

- sticky 节点：`fail_closed` 可开；hard 或确认后的 soft 异常先摘流；
- Resin 池：优先使用请求级新隧道语义，避免一次出口故障导致整池冷却；
- 所有节点：恢复必须经过真实模型质量 probe；
- rotation 只配置在明确支持换 IP 的 sticky 节点；
- `min_healthy_nodes` 保留业务冗余，不要为了“检测完整”把最后一个健康出口也摘掉；
- rank/rebalance 只操作 auto 账号，不作为质量判定的安全边界。

## 6. 推荐上线顺序

### 阶段 A：单节点验证

- 只接入一条 sticky 家宽或一个 Resin pool listener；
- 记录多次连通检测和真实模型质量结果；
- 确认应用容器能访问 listener，且没有把容器内回环地址误填给另一容器；
- 先不启用自动换 IP 和自动 rebalance。

### 阶段 B：拆分故障域

- 每个 sticky session 建一个独立 listener/node；
- Resin 按 provider/地区/认证策略拆成独立池；
- 为每个节点确认不同的最终出口 IP 或明确的池语义；
- 小批量绑定 auto 账号，观察首字、流式断流、401/403/429 和 Token/s。

### 阶段 C：启用 Guard

- 先 `passive` 或 `dry-run` 观察阈值分布；
- 再启用 active probe，并设置足够长的间隔；
- 仅对已验证的 sticky 节点配置 rotation allowlist；
- 验证异常节点会先 disable、迁出 auto 账号，再调用单节点轮换；
- 验证新 IP 和真实模型质量都通过后才自动恢复。

### 阶段 D：扩容和故障演练

- 逐步增加每个出口的账号数和并发；
- 人工演练一条 sticky session 失败、一个 Resin 隧道失败和一次 rotation webhook 超时；
- 确认其他节点不被重载、不被换 IP、不被迁号；
- 确认所有秘密只存在于运行环境和权限受限的状态目录；
- 保留一个不参与自动轮换的回退节点或回退 provider。

## 7. 故障判断速查

| 现象 | 优先检查 | 不要直接做 |
| --- | --- | --- |
| 所有账号同时断流 | relay/Mihomo 共享故障域、监听器和容器网络 | 逐个旋转所有住宅 session |
| 只有一条 sticky 路径异常 | 该 node 的最终 IP、session 和 rotation 结果 | 重启整个 Mihomo 集群 |
| Resin 单次失败 | 当前隧道是否已结束、池是否能建立新连接 | 把整个 pool 标成固定坏节点 |
| IP 变了但模型仍异常 | 真实模型 probe、流式窗口和认证错误 | 仅凭 `curl` 成功恢复 |
| Guard 一直不恢复 | `disabled_by_guard` 所有权、rotation allowlist、probe 结果 | 手工改 `state.json` 强行 enable |
| 轮换后其他节点也变 IP | listener/provider 共享配置或全量重载 | 继续扩大 rotation 范围 |

## 8. AI/运维执行前的检查清单

- [ ] 先识别每个 upstream 是 sticky 还是动态池；
- [ ] 每个 sticky session 都有独立 listener 和应用 node；
- [ ] Resin 动态池使用独立 pool listener，且应用标记为 pool；
- [ ] Mihomo 与应用之间的 listener 可达，但 controller 不对公网开放；
- [ ] 最终出口 IP 与 relay IP 分开记录；
- [ ] 连通测试、真实模型质量测试和账号迁移分三个步骤执行；
- [ ] rotation webhook 有 node allowlist，并验证返回的新 IP；
- [ ] rotation 失败不会自动恢复节点；
- [ ] 动态池单连接失败不会造成全池冷却；
- [ ] Guard 只迁移 auto 账号，不覆盖人工 sticky 绑定；
- [ ] 不读取、提交或打印真实代理 URL、token、账号凭据、状态卷和生产日志；
- [ ] 任何重载或重启动作都明确影响范围，并保留回退路径。

## 9. 最小脱敏配置模型

下面只描述字段关系，不是生产凭据模板：

```yaml
egress_nodes:
  - name: residential-session-a
    proxy_url: <mihomo-fixed-listener-a>
    proxy_pool: false
    rotatable: true
  - name: resin-pool-us
    proxy_url: <mihomo-resin-pool-listener>
    proxy_pool: true
    rotatable: false

quality_guard:
  mode: hybrid
  min_healthy_nodes: <redundancy-floor>
  rotation_url: <private-rotation-webhook>
  rotatable_node_ids: [<sticky-node-id-a>]
```

所有真实值应由部署系统注入。文档、补丁、Issue、PR 和 AI 对话只保留字段关系和行为约束。
