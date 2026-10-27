# 第 27 份：OAuth 来源绑定与控制器预算收口

2026-10-27，北京时间。本报告只处理第 26 份恢复失败后的 P0。固定候选五组完整 CI 已通过；代码修复不能代替真实两来源自动更新验收。

## 一、执行边界

- 工作分支：`feature/knowhub-feishu-access-v2`。未合并 main，未部署生产，未扩大来源白名单、同步预算或 namespace。
- 最新核对的 `origin/main` 为 `080f7bb6a01fb6293344b438f1dd74ecb7a681ae`；PR #50 已合并。AnyDoc Reader、其候选测试和 VLM Remote API 三个受保护文件与 main 一致。
- 第 25 份批准已经消耗。本轮没有迁移真实来源连接、重新登记发布授权、清除 trip、恢复 hold、重新投递旧终态任务或发布新内容。
- 新管理写入口默认关闭：`FEISHU_SOURCE_REBIND_OPERATIONS_ENABLED` 必须为 `1`，且必须具备系统管理员会话。隔离候选升级不会启用该开关。
- 未要求用户再次授权或粘贴 Sheet/Bitable 链接。HTTP 回调的原到期值未延长。

## 二、原候选 `020a2fb0` 的完整 CI 最终结果

固定 SHA：`020a2fb068e371783c402a2c104039d6c3c837a0`。

| 门禁 | 最终结果 | 运行 |
|---|---|---|
| V2：前端、lint、真实 PostgreSQL/Redis | success | [40347984269](https://github.com/suoak/WeKnora/actions/runs/40347984269) |
| Native 全量 | cancelled，未通过 | [40348506675](https://github.com/suoak/WeKnora/actions/runs/40348506675) |
| Integrated | success | [40348514476](https://github.com/suoak/WeKnora/actions/runs/40348514476) |
| Portal | success | [40348519878](https://github.com/suoak/WeKnora/actions/runs/40348519878) |
| Python MCP | success | [40348524787](https://github.com/suoak/WeKnora/actions/runs/40348524787) |

Native 长时间停留在系统依赖安装，最终日志在 2026-10-27 00:39:03 显示 `The operation was canceled`；不能视为测试成功。没有使用这个不完整结果部署 `020a2fb0`。

新版 Native 门禁优先验证已安装的构建依赖，缺失时才安装，并给安装步骤设置有限网络重试和执行时限。没有跳过编译、原生解析、依赖审计或应用回归。

[原候选完整 CI 回执](evidence/report27-original-candidate-complete-ci-20261027.json)。

## 三、身份绑定根因与实现

### 已确认的现场根因

两个来源保存的 connection 引用由旧 GrantEpoch 派生。补充 OAuth 后 GrantEpoch 变化，当前有效 connection 引用随之变化；既有来源引用没有迁移。

| 资源 | 来源摘要 | 原连接摘要 | 当前连接摘要 | 同 KnowHub 用户 / App / OpenID |
|---|---|---|---|---|
| Sheet | `6d1698a6e0d9` | `978b52db04c1` | `be8a187c88fa` | 均为 true |
| Bitable | `1dcf458346ba` | `978b52db04c1` | `be8a187c88fa` | 均为 true |

正常 UserToken 刷新只更新令牌与有效期，不旋转 GrantEpoch。补充授权或重新连接产生的新 grant 才会使旧引用需要处理。刷新响应未返回 scope 时保留原 grant 的 scope 清单；返回明确的新清单则采用新值。

### 新的受控操作

实现位于 `internal/application/service/feishu_source_rebind.go`。管理入口为 `POST /api/v1/system/admin/feishu/sources/rebind/:action`，普通员工导入流程不增加操作。操作人必须是会话本人对应的真实 User ID，事务内复核其有效账号和系统管理员资格，不接受任意操作人标签代替审批人；管理员撤权后拒绝提交。

1. **prepare**：只读生成有效期 10 分钟的 AES-GCM 加密计划，不持久化来源或发布授权。精确选择来源，最多 16 个、去重排序，同一 owner 与目标 KB。校验原 Import 的 OpenID、App、资源、Knowledge 映射及当前连接；核验用户、成员、KB 权限和 view/copy/export，并核对实际 Scope 清单。
2. **authorize-publication**：单独的新发布授权，最多 24 小时，绑定计划、当前连接/GrantEpoch、资源当前版本、目标及受众。不得把迁移批准当作发布批准，不继承原 continuing policy 的有效性。
3. **apply**：再次查询实际资源权限与版本，再在单个数据库事务中按固定顺序锁定全部来源、原 Import、身份、权限和新授权。CAS 对照配置、状态、保护记录、Cursor、执行记录和已发布数据摘要。任何条目失配，全部回滚。
4. **rollback**：需要单独操作批准；只有迁移后的来源、Cursor、执行及索引仍匹配回执时才能整批恢复原绑定。已有新执行、索引变化、保护变化或新策略时拒绝回滚，转人工对账。旧发布授权不复活。

`apply` 对来源本身只更新 `config.settings.credential_reference.connection_id`；不改变 ExternalID、Knowledge ID、Cursor、调度、hold/revoked/paused、文档保护记录或内容。原配置加密快照、前后摘要、备份 SHA、新旧授权关联和操作引用写入事务内的加密审计回执。旧持续策略先归档，再失效，不带入新身份。

新授权撤销、过期、GrantEpoch 改变或 KB 受众改变后，提交门禁继续拒绝，不回退使用旧批准。连接迁移不等于放行同步。

原用户侧只按 App 匹配的 `rebind` 已拒绝执行，原按钮已移除。OAuth 状态提供 owner 范围内的 `sources_requiring_rebind`；来源详情在执行前可显示 `source_identity_rebind_required`，不再误导用户重复补充授权。

### 测试中的缺陷与修正

首轮真实 PostgreSQL 测试拦住了公共 DTO 隐藏 AppID 导致的计划还原错误。新计划将必要身份字段独立保存在密文内，并在解密后恢复；测试显式核对 AppID/OpenID。未部署含该缺陷的候选。

另对 JSONB 保护记录使用规范化摘要比较，避免 JSON 空白变化造成误报；非空保护记录必须在迁移、冲突回滚和恢复原绑定中完整保留。

## 四、`audit_unavailable`：事实、机制缺陷与证据限度

### 不能混为一谈的三次事件

1. 首次消费者丢失：`automatic_permit_consumer_lost`。
2. 后一次 membership probe 超预算：`automatic_membership_probe_context_deadline`。
3. 本次 2026-10-26 23:00:22 新停止：`audit_unavailable`。现场记录两个控制器 Step 分别约 712ms、664ms，失败阶段为 audit。

第三次记录的是整个 Step 到失败时的耗时，**不是 XADD 单独耗时**。历史二进制没有连接池、网络读写和阶段细分回执，不能追溯断言 fsync、XADD 或网络就是该次物理根因。

### 已确认、可针对性修复的控制链路缺陷

- 控制器与普通业务共用 Redis 连接池，业务占用可能挤占 registry/audit/consumer probe 的连接。
- Redis 客户端未启用 `ContextTimeoutEnabled`；Step Context 不构成完整的网络 I/O 预算边界。
- Asynq Inspector 的 `Servers()` 没有调用方 Context 参数，内部 Background 请求丢失 Step 截止时间。

修复：同 endpoint、身份、TLS、DB、namespace 的专用控制连接池；连接/读/写/池等待上限仍为 500ms，启用 Context 截止时间并关闭隐式命令重试。仅对 Inspector 丢失截止时间的请求恢复当前 Step Context；显式的独立停止 Context 不受其影响。包括 Permit 在内的命令若成功返回时 Context 已超时，也按失败处理，不能把迟到确认当成有效放行。

复核还发现 Guard.Trip 原来仍借用业务池。新增 `TripUsing` 让控制器通过同 Redis 的控制连接写入原有 trip、删除原有 Permit、写入原有审计，不改变普通业务 Guard 的客户端或预算。测试验证业务 Redis 客户端不可用时仍能持久停止，且原始原因不被覆盖、审计不重复。审计失败或确认太晚仍关闭出站并保留 trip；Step 500ms、Permit 750ms 未延长。

真实故障注入还暴露了停止脚本冷缓存的依赖：先执行 EVALSHA、再因 NOSCRIPT 回退，会多一次受限往返。停止写入改为直接 EVAL 原有原子脚本，仍按原规则一次完成首次原因、删除 Permit 和审计写入。测试覆盖脚本缓存不可用时仍能持久停止，以及健康同伴不能重新放行；没有清除业务 Redis 脚本缓存或停止锁。

### 定向观测

`FEISHU_SYNC_CONTROL_DIAGNOSTICS=1` 仅在隔离站启用。结构化日志包含：

- Step 请求/开始/结束、锁等待、Step 关联 ID、实例和 incarnation。
- registry、audit、membership、consumer、permit、独立 stop 阶段边界；各阶段开始时的剩余 Context 时间。
- Redis 命令开始/结束和成功确认；命令名，不记录参数、Key、错误正文或内容。
- 连接池实际累计等待差值，以及 Dial/Read/Write 开始、结束、字节计数与失败标志。
- 有界缓冲和采样；日志消费不能阻塞安全控制循环。

Read 耗时包含服务端响应与网络等待，不将其标为纯网络时延或磁盘 fsync 耗时。stop 使用独立预算，不能与失败前的控制阶段混计。

### 现场只读健康证据

2026-10-27 00:18 的 80 次 GET 采样：P50 0.121ms、P95 0.263ms、最大 0.605ms。00:32 的只读取证：49 个连接、0 个 blocked client、0 个 rejected connection、AOF pending fsync 为 0；`aof_delayed_fsync=85` 是累计计数。SLOWLOG 和 LATENCY LATEST 返回空。

这些证据不能把历史故障归因于磁盘，也不能排除未被记录的历史抖动。正式关闭历史物理根因仍需新观测形成对应阶段/请求的实际证据，不能以当前低延迟样本代替。

[控制链路只读基线](evidence/report27-readonly-before-20261027.json) · [Redis 定向取证](evidence/report27-redis-forensics-20261027.json)。

## 五、隔离故障注入与回归门禁

本地控制器测试、管理写入口默认拒绝测试、部署环境改写测试已运行。真实 PostgreSQL 与 Redis/Asynq 门禁以及另外四组完整门禁均已在最终固定 SHA 通过，完整回执如下。

| 门禁 | 结果 | 运行 |
|---|---|---|
| V2：前端、lint、真实 PostgreSQL/Redis、race | success | [40425146786](https://github.com/suoak/WeKnora/actions/runs/40425146786) |
| Native 全量与原生解析 | success | [40425216405](https://github.com/suoak/WeKnora/actions/runs/40425216405) |
| Integrated 全量与真实停止链路 | success | [40425215596](https://github.com/suoak/WeKnora/actions/runs/40425215596) |
| Portal / Go MCP / 升级治理 | success | [40425215495](https://github.com/suoak/WeKnora/actions/runs/40425215495) |
| Python MCP 多版本与打包 | success | [40425215478](https://github.com/suoak/WeKnora/actions/runs/40425215478) |

[同一固定 SHA 的五组完整回执](evidence/report27-complete-ci-20261027.json)。Python MCP 的 PyPI 发布步骤按既有分支条件跳过，没有发布生产包。

必须包含：

- 真 PostgreSQL：同批两个来源事务迁移及回滚、第二条冲突零迁移、GrantEpoch 再变化、换 OpenID、源权限拒绝、新批准撤销、索引变化、并发仅一个赢家、索引变化后拒绝回滚。
- 真 Redis、两个 Asynq OS 进程：在一次性 GitHub Actions Redis 服务上定向注入 650ms 服务暂停，使用生产 500ms Step 预算，分别覆盖 registry 与 audit；保留停止锁、暂停队列、撤销 Permit，正常同伴及迟到确认均不能恢复放行。服务暂停用例拒绝在非 GitHub Actions 环境执行，不操作业务测试站的 Redis。
- 独立连接池：业务池占满后原共享 audit 超时，专用 audit 仍可确认。
- 停止写入不再依赖业务池；迟到的成功 Permit 回执必须被撤销并触发停止。
- 正常刷新保持绑定；新 OAuth epoch 主动标记需要管理员处理，不自动迁移。
- 原版本、个人权限、共享批准、Cursor、保护记录、Portal、Go/Python MCP 等既有回归。

最终候选 V2 成功日志中，受控迁移的 10 个场景分别在定向、全量和 race 步骤通过，共 33 条含父测试的通过回执。真实 Redis 故障注入分别在 registry/audit 记录生产预算 500ms、实际耗时 500ms，出站关闭且 trip 保留；四条回执来自定向与全包的重复执行。[固定 SHA 的 P0 原始测试摘要](evidence/report27-exact-source-p0-ci-20261027.json)。

## 六、下一次实际迁移的精确操作清单（尚未获批，未执行）

只针对上表两个既有来源，所有实际 ID 从隔离数据库读取，不在报告输出资源 Token、OpenID 或源 URL。

1. 固定经过完整门禁的应用 SHA；定向观测必须已在 APP/worker2 正常产生，无新的未解释超预算。保持当前 trip、sync 暂停和两个 hold。
2. 只读重新生成精确计划，核实当前 GrantEpoch/Scope、原用户/App/OpenID、同一资源/KB、当前版本和受众；任何实际不匹配都停止。
3. 取得新的**绑定迁移批准**与独立的**当前版本发布授权**，不得复用第 19/25 份引用。持续共享授权和后续 hold 对账另有独立范围与期限。
4. 受控 drain、停消费者，确认没有 active/running 执行。新建数据库备份并独立恢复；核对 9 张业务表计数及 data_sources/sync_logs/knowledges/chunks/embeddings 内容摘要。记录新备份 SHA。API 本身不打开备份文件，操作工具必须提供已经完成恢复核验的外部回执，不能只填任意 SHA。
5. 临时启用隔离管理写入口，分别登记新发布依据，再一次事务修改两个 connection_id：`978b52db04c1` → 操作时仍匹配的 `be8a187c88fa`。当前 GrantEpoch 若再次变化，旧计划立即失效。
6. 同事务归档/失效旧 continuing policy，写加密前后快照与审计。保持 hold、Cursor、Knowledge/ExternalID、索引、trip、队列暂停。关闭管理写入口，逐项对账。
7. 若任何 CAS 失败，零条来源提交。提交后若要回滚，需新的回滚批准，且必须满足无后续执行/索引变化；只恢复原绑定并撤销新批准，不恢复旧共享策略。

风险：新的授权可能再过期；新观测可能发现实际 Redis/CPU 抖动；KB 受众或资源版本可能在计划期间改变。上述情况均停止，不替换目标、不降级身份、不扩大访问。单次 source rebind 本身不会生成任何新 Knowledge 或向量。

## 七、停止锁与 hold 的独立恢复条件

本轮没有申请或执行解除。

- **持久停止锁**：须证明当前实例拓扑、有效租约、审计确认及剩余预算均健康；解释或修复实际观测到的超时；固定最新原始 trip 值/事件，取得新的明确批准后才可 CAS 处理。不能用迁移成功、当前 Redis 快速或旧批准代替。
- **来源 hold**：须先完成有效连接绑定、新授权及原保护原因对账；原终态 SyncLog 不复活。若仍须全量安全对账，必须新批次式单次执行授权、限定来源和时间窗口，完整安全发布成功前保持 hold。
- **revoked/paused/人工保护**：没有被 rebind 或清锁隐式解除；各自沿原有明确操作和权限流程处理。
- **后续业务验收**：自然入队与 sync_logs、无变化不重建、已改字段自动发布、原 Knowledge ID 不变、Chunk/向量/BM25/版本对账，以及 Portal/Go/Python MCP 新值检索。当前均不能因首次入库旧成绩或代码门禁通过而勾选。

## 八、现有数据保护基线与部署门禁

| 资源 | Knowledge 摘要 | Chunk / 向量 | 配置 MD5 | Cursor MD5 |
|---|---|---|---|---|
| Sheet | `d0c4602a75ca` | 24 / 22 | `82b3cb65b7a6ec50fd2cd56c5f7fa45e` | `4432c02be06179a9cb98afd64a1f50bc` |
| Bitable | `f48df03f6510` | 30 / 27 | `4cd258fe90d889c84c1f4b571a918c01` | `3e3d26e2874a8abf283252e8e1336fe1` |

向量 ID 集合摘要分别为 `7c38a4c1b9f272abeabc541ecf97154f`、`e896eda14f85620f9b2bce70984cfb42`。两来源均 `error/hold`，没有 active/pending/scheduled/retry，trip 为 `audit_unavailable`、Permit 缺失、sync 队列暂停。

[隔离数据只读保护基线](evidence/report27-protected-before-20261027.json)。

只有最终候选五组门禁均完成且 success，才使用既有隔离部署流程升级；备份/独立恢复核验失败则拒绝升级。候选部署只改变应用/前端镜像 SHA 和隔离诊断开关，不改变 Redis、数据库、DocReader 镜像，不解除锁或 hold。升级后还必须重复核对该表和保护数据。

## 九、最终固定候选与部署结果

上一候选 `5f85bdf713e88674bc91f0eab30e4e9415dccf18` 的实际后端、并发及控制故障测试通过，但最后 Python 工具测试因 CI 没有 `paramiko` 而失败，未部署。测试现直接以 AST 读取部署脚本中的实际函数，不导入 SSH 依赖；本机 `python -S`（不加载第三方包）运行 16 项全部通过。

最终部署候选：`b2cd0a63eb806648730c241edc875ff0f80cd822`，仅增加上述测试依赖修正，运行时代码与上一候选一致。五组完整 CI 已全部按新 SHA 重跑并通过，没有借用上一候选的成功子步骤。

集成门禁中真实停止链路的启动竞争已定向修复：测试必须等两个控制器均完成首次 Step，并核验健康 Permit 后才恢复测试队列；不增加生产超时，不关闭停止保护。修正后的该链路已通过。

2026-10-27 08:10 北京时间再次只读核验：原停止原因为 `audit_unavailable`，同步队列暂停、Permit 缺失、四类队列计数均为 0。两来源仍为 `error/hold`，旧连接引用、Knowledge、配置/Cursor 摘要、Chunk 数和向量 ID 集合与本报告基线相同。

08:07、08:10 的新 Redis 日志出现 `fsync is taking too long` / asynchronous AOF 告警。这是后续时间点的真实观测，不能据此追溯认定 23:00:22 停止必然由 fsync 引发。后续恢复前需结合新阶段观测及宿主机 I/O 健康重新判断，不能凭当前一次 GET 快速放行。

之后的只读采样为 GET P50 0.126ms、P95 0.310ms、最大 0.431ms，连接 51、blocked/rejected 均为 0、AOF pending fsync 为 0，但累计 delayed fsync 已增至 96。SLOWLOG 有一条 EVALSHA 19.627ms，仍不足以归因 500ms 整体预算的消耗。[升级前新 Redis 回执](evidence/report27-redis-pre-upgrade-20261027.json)。

### 实际升级与保护对账

- 2026-10-27 08:44 北京时间，使用既有隔离项目完成 APP/worker2 受控停止、备份、独立数据库恢复校验及代码升级。9 张业务表计数、5 张保护表全行摘要在恢复库与原库一致，校验不符会拒绝升级。
- 备份 SHA256：`ac5fec6c9c095e4f5f28458a0baab9f756caffa4ced0dc3cf7dbc3d2d9125467`。私有备份与独立恢复库均保留，未通过覆盖业务库来回滚数据。
- APP/worker2：`sha256:5833cedb993437a9b058feac2e5512d29b821f820f24baa63af10118f8ae719f`，均 healthy。前端及入口：`sha256:e5e1f82e21dfa9e07e424bd4d9ace4feb8dab5d4c55e748d60dcae3d7e6a986b`。
- DocReader `6bd74bcf12a2`、PostgreSQL `d6fb8f50e25e`、Redis `426f6fb54a1b` 沿用原镜像，原容器持续运行，未重启这三项基础服务。
- 健康和私有预览、前后端 SHA、未认证 API 拒绝、OAuth callback 响应头检查通过。core/maintenance/shared/wiki 等 Worker 池均观察到两个实例。`test` 仍为有效账号且连接记录存在。
- 两来源配置、原连接引用、Cursor、Knowledge ID、Chunk 数、向量 ID 集合均与升级前一致；无 active/pending/scheduled/retry。原 `audit_unavailable` trip、sync 暂停和两个 hold 保留，Permit 仍缺失。没有登记新授权、迁移连接、恢复 hold 或发布新版内容。

[升级、独立恢复和启动回执](evidence/report27-deployment-20261027.json) · [升级前保护数据](evidence/report27-protected-pre-upgrade-20261027.json) · [升级后逐项相等回执](evidence/report27-protected-after-20261027.json)。

### 部署后的实际阶段观测

截至 08:47:50，北京时间，APP/worker2 合计取得 8 条采样：registry/membership/consumer 均有记录，7 条进入 Permit 后按保留的 trip 停止；另 1 条为刚启动时消费者尚未就绪。最大 Step 耗时 16.327ms，含独立停止阶段。两条有完整 audit 阶段的采样在调用前分别剩余 499/498ms，audit 耗时 0.408/0.133ms；最大 Redis 命令 1.022ms，采样中的连接池等待为 0ns。记录包含关联 ID、实际 I/O 边界与独立 stop 预算。

新容器的停止事件仅观察到两条 `automatic_permit_tripped`，对应已有锁的正常拒绝；最近审计持续收到两实例 `controller_alive`。该窗口未观察到新的 registry/audit/membership 超预算。AOF delayed fsync 为 98，较首次部署后采样未增加，但仍不能据此排除未来宿主机 I/O 抖动。

[部署后脱敏阶段、命令、I/O 及租约回执](evidence/report27-control-after-20261027.json)。这些是**保留停止锁情况下**的观测，不等于已恢复自然调度，更不等于新值检索验收。

复查过程中还修复了本地诊断传输工具的 64KiB 截断问题：完整报告最多读取 1MiB，超过上限明确拒绝，不解析截断前缀。实际约 222KiB 的格式化报告已成功取回，16 项离线工具测试和 Python 编译检查通过。该支持工具修正提交为 `3c77ad478`，没有替换上述已验证、已部署的应用 SHA，也不能把候选 CI 声称为这个后续工具提交的完整 CI。

### 尚未关闭的验收项

历史 23:00:22 的物理故障仍缺少当时细分证据，不能宣称已证明是 fsync、网络或 XADD 导致。已复现并修复共享业务池、预算丢失、停止脚本冷缓存和迟到确认等控制链路问题；新阶段观测已实际工作，恢复前仍须核对宿主机 I/O 和控制链路稳定性。

两来源仍需按第六节另获**新批准**后，办理有效发布依据与受控连接迁移；清除持久停止锁和恢复 hold 各有第七节的独立条件。自然调度、无变化不重建、真实改值发布及 Portal/Go/Python MCP 新值检索均尚未完成，不以本轮 CI 和启动检查代替。未合并 main、未部署生产。

隔离站仍为 [http://172.30.232.42/](http://172.30.232.42/)，没有改称 HTTPS，没有延长临时 HTTP 授权期限。普通员工入口保持原有简化流程；本轮不要求再次授权或重新提交链接。
