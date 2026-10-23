# 飞书发布控制、运维门禁与外部依赖（2026-10-23）

本文延续 `FEISHU_SYNC_P0_MERGE_READINESS_20261023.md`。基线 PR #35、候选 `70c7d56765e1b1ca2a6e3d33c8d7047a15422c71` 未合并。这里只准备发布控制，不批准生产变更。新的发布控制代码必须使用自己的最终 SHA 和隔离 CI 证据，不能继承旧候选的测试结论。P1 父子分块与版本归属修复独立分支。

## 证据边界与当前判定

复用 2026-10-23 已完成的五份本地只读报告：`feishu-p0-preflight-{decision,live,controls,final,permissions}-20261023.json`。文件位于调查工作站 `E:/AIData/Codex/`，不把带内部生产配置的原始报告提交到公开仓库。`decision` 中保存其余四份的 SHA256。此轮未重新全量扫描生产、连接飞书实时权限接口或发起生产网关请求。

已有证据只覆盖一个生产主机及其配置的 Redis DB：maintenance=4，消费 sync:2 与 low:1；后台模型设置解析为 8。不能推断完整集群总并发。用户补充及[阿里云官方限流说明](https://help.aliyun.com/zh/model-studio/rate-limit)已确认标称区域限额：北京/香港 1800 RPM、120 万 TPM，新加坡 1800 RPM、100 万 TPM；uniapi 管理员反馈该代理无额外频率/并发限制。这些信息不等于获得完整飞书预算：实际区域、主账号与具体模型配额作用域、同模型共享业务、近期实际使用量/余量、最大并发和批准的灰度 TPM 仍 UNKNOWN。不同型号独立 quota，不能因 Rerank/QA 与 Embedding 共用域名就断言消耗同模型限额。自动停止实际接线、合格首源也未确认。**生产仍为 NO-GO**；本文件中的隔离验收、操作方案不代表已在生产执行。

## 可重复的离线配置一致性门禁

工具：`scripts/feishu-release/check_manifest.py`；只读取调用者提供的 JSON，不连接 DB/Redis/SSH，不生成生产执行命令，不改变队列或数据源。Python 3.9+，无第三方依赖。

```text
python -m unittest discover -s scripts/feishu-release -p "test_*.py" -v
python scripts/feishu-release/check_manifest.py reviewed-manifest.json
```

退出码 2 表示 `NO-GO`，包括缺字段、额外字段、重复 JSON 键、未知值、过期快照或不一致。退出码 0 仅表示 `MANIFEST-CONSISTENT`，输出始终含 `authorization_to_deploy=false`。工具不是证据收集器，不能验证人填写的 true 是否真实，也不替代配额批准和发布审批。所有 `evidence` 需要引用经审阅的原始输出/审计回执；评审时核对文件哈希、采样时间、权限与采证范围。禁止把 ACL DRYRUN 结果填成隔离端到端成功。

严格 schema 的完整参考由 `scripts/feishu-release/test_check_manifest.py::fixture()` 提供。它是**虚构的隔离通过用例，不能用于生产放行**。仓库附带 `manifest.example.json` 是刻意缺少外部确认的 NO-GO 模板。填写规则：

| 字段 | 来源与核验 |
|---|---|
| `schema` / `observed_at` / `max_age_seconds` | schema=1；带时区时间；生产检查所有采证需落入声明窗口，最大 300 秒。过期后重新提交最新已授权只读快照，不自动更新旧证据时间 |
| `inventory_complete` / `inventory_evidence` | 运维 CMDB/编排清单的明确完整性声明；单主机 docker ps 不足。缺失为 false |
| `expected_instances` | 全部 APP、独立 Worker、嵌入 Worker、定时生产者的稳定唯一 ID；不遗漏 standby/旧部署/定时作业。停机实例的不可再启动证明单独归档 |
| `expected_redis_endpoints` | 所有队列 Redis 的独立规范化标识，至少区分集群/端点/DB/命名空间；别名需由运维归一化，不能靠 `redis:6379` 字符串认定同一个实例；不写密码/URI 用户信息 |
| `instances[]` | 每个实例的源码 SHA、Redis 标识、scheduler/worker 角色、显式 scope、支持该门禁的能力证明、maintenance 与 model 的 DB 行/ENV 及实际 maintenance 并发；采证时列出全部存活 Asynq 池，而非只挑已知 maintenance 池 |
| DB 行 | 缺行写 null；有行写 `{value, last_modified_by}`。保留 value 的 JSON 类型，字符串 `"4"` 与数字 `4` 不同。只导出对应非机密设置 |
| ENV | 缺失写 null；存在写实际字符串，包括 `"0"`、空字符串；不能用 `UNSET` 假装已确认非空值 |
| `control_policy_sha256` | 对已审阅的规范化控制配置计算 SHA256，覆盖出站端点、Redis 预算作用域、global in-flight/RPM/TPM、紧急阻断、watchdog 时限、控制器身份与版本。凭证只引用不可逆 ID，不能含明文。全部实例必须一致 |
| `redis_endpoints[].live_pools[]` | Redis 注册的所有存活池 ID、实例归属、pool 名称、实际并发、队列和权重。遗漏实例、孤儿池、意外 sync/low 消费者、重复池均拒绝 |
| 队列状态 | 发布前一律 sync_paused=true 且 sync_active=0；工具不会启用消费。single-source-ready 指“允许进入后续审批”的准备态，仍保持暂停 |
| `controls` | 网关预算确认、硬出站、闭环 E2E、指标新鲜、watchdog fail-closed、隔离真实暂停权限、fencing、备份恢复及 Startup Smoke 的证据；任一未确认填 false，不填写推测 |
| `canary` | scope-none 阶段可尚未选源；single-source-ready 必须与唯一真实 UUID 一致，非关键性、无父子、版本安全、现有身份读取/导出权限及 DNS 全部经证据确认 |

`scope-none` 只接受显式 `none`。`single-source-ready` 只接受一个规范 UUID（不是租户 ID、KB ID、库名），不接受空值、`*` 或多源。旧 Worker/旧调度者或不支持门禁的实例被拒绝；单靠写了新 ENV 不能证明镜像认识它。所有实例 SHA、scope 与控制配置指纹一致才通过。工具报告 maintenance 全集群槽位之和，并明确这些槽位与 low 共用，不将它们误称为 sync 独享并发。

### 配置优先级源码核验

`internal/application/service/system_setting.go::resolveRaw/GetInt` 实际为 DB > ENV > caller default；DB 读取/类型错误会回退，DB 缓存也会影响即时观测。`isBootstrapDefaultRow` 的例外是：`last_modified_by` 去空白后为空，且原始 JSON compact 后**等于 registry 默认 JSON**，视为 bootstrap placeholder，继续 ENV/default。手工写入默认值且有修改者，仍是 DB 优先；JSON 字符串 `"4"` 不等于 JSON 数字 `4`。

`internal/types/task.go::ResolveWorkerPoolConcurrency` 对解析结果 <1 再退默认：maintenance 默认 4，配置 0 并不是停止 Worker。`container.go::resolveModelMaxConcurrency` 和系统设置 model.max_concurrency 默认 32；模型可单独覆盖，且旧 governor 故障放行，不作为飞书新硬门禁的替代。模型 <=0 的原语义不同于 Worker，不得用同一个正数回退规则解释。设置已绑定启动的 Worker 必须以 Redis 实际注册并发核对，修改设置后不假定运行进程已更新。

本工具对这些解析细节有合成测试，但不谎称已测量所有生产实例内存缓存。完整实机配置优先级与缓存一致性仍需隔离 APP 实例检验和运维采证。

### 新发布控制的实际环境字段与监控契约

以下字段是本轮独立发布控制分支的新代码接口，并非现有生产已配置值。它们必须纳入每实例采证和控制配置指纹。`FEISHU_SYNC_SOURCE_SHA` 只是配置声明，必须与构建记录/镜像源码身份另行比对，不能把 ENV 自证当作可信构建证明。

| 配置 | 要求 |
|---|---|
| `FEISHU_SYNC_SAFETY_ENABLED` | 新灰度执行者显式 `true`；不能通过省略或 false 绕过硬保护 |
| `FEISHU_SYNC_SAFETY_POLICY_JSON` | 严格 Config JSON：`Namespace`、`EndpointHost`、`InFlight`、`RPM`、`RPS`、`TPM`、`TokenBoundVerified`、`TokenOverhead`、`WaitBudgetDuration`。拒绝未知字段；各副本规范化后指纹一致。`RPS` 必须显式正数，首次建议 1；`WaitBudgetDuration` 是 JSON 纳秒，建议 `65000000000`（65 秒上限），不能写字符串 `"65s"`。InFlight/RPM/TPM 必须获批准，尤其 TPM 不得从区域上限或剩余额度未知时猜测 |
| `FEISHU_SYNC_WORKER_ID` | 每个进程唯一稳定 ID，同 ID 存活冲突必须关闭；重启产生新的 incarnation |
| `FEISHU_SYNC_EXPECTED_WORKERS` | 逗号分隔完整 Worker 清单，各副本一致，不是当前恰能看到的局部列表；同时与 CMDB/Redis 注册核对 |
| `FEISHU_SYNC_SOURCE_SHA` | 40 位最终源码 SHA，所有实例一致，并与独立镜像证据吻合 |
| `FEISHU_SYNC_SAFETY_ALERT_URL` | 显式 HTTPS 接收器；离线 manifest 对应 `instances[].safety.alert_url` 必填、逐副本精确一致，拒绝 userinfo、fragment、非法端口和空主机。默认 SSRF 安全客户端，每秒健康回执及停止事件要求 2xx；失联关闭许可。禁止 URL 内嵌凭证；生产告警路由及权限仍需验收。离线 URL 校验不会连接接收器，不证明 DNS、鉴权或告警送达 |
| `FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS` | 起步 `none`；后续只能一个已批准的规范 UUID；不是运行中任务即时终止开关 |
| maintenance 实际并发 | 新控制器要求每实例实际解析为 1；DB 设置优先于 ENV，且仍与 low 共用。多实例总槽位不能声称等于 1，硬预算跨实例共享 |

监控收集者向部署 Namespace 对应 `MetricsKey()` 写严格 JSON，并给每次样本设置 2 秒 TTL。字段为 `observed_unix_ms`、`gateway_budget_known`、`gateway_healthy`、`online_healthy`、`consumer_healthy`、`scope_violations`、`quota_exceeded`、`online_p95_ratio`；布尔项和 scope 计数必须明确存在。`consumer_healthy` 必须来自全部预期 Asynq 消费者的真实运行/错误/积压监控，不能拿 APP 控制器心跳替代，异常或未知必须 false；未提供该字段也关闭门禁。`online_p95_ratio` 必须正数且 <=1.2，时间戳最多落后 Redis 2 秒、超前 100ms。重复字段、未知字段和尾随 JSON 被拒绝。样本来自可信监控身份，不能用飞书 Worker 自报“健康”代替外部指标。发布前还需确认 TTL、时钟、采样链路与报警接收实际符合此契约。

控制器每约 200ms 运行，注册租约 2 秒、出站许可 750ms，最多 10 秒 bootstrap 期间仍保持闭门；缺样本、缺 Worker 或配置漂移不能放行。trip 持久锁存，不提供自动解锁；`none`、队列暂停、进程退出与恢复 Redis 都不能视为授权恢复。需要受限 Redis ACL：监控身份只能写对应 metrics，Worker 不得伪造外部 metrics 或清 trip，控制身份的 permit/trip/队列暂停及审计权限须由运维核验，不能仅引用代码声称已隔离。

预算暂忙允许在最多 65 秒预算等待窗口内排队，不立即 trip；429 策略最多 3 次 wire attempt，每次重新消耗预算，遵从受限 Retry-After 或 1s/2s 退避，总重试等待最多 30 秒，耗尽后 trip。5xx、超时、配额拒绝与状态不可确认保持停止策略。任何 SDK 隐含重试都不能绕过每次 wire admission；对应隔离测试与最终源码一致后才可认定已验证。

## 隔离 Startup Smoke 与旧任务验证方案

以下为隔离执行要求，禁止把生产 DSN、Redis 凭证、飞书凭证或网关 Key 注入测试。源码 `go test`/tests-only 隔离测试容器不等于生产发布镜像；生产镜像构建及发布仍需另行审批。

1. 固定最终提交 SHA、基础镜像 digest、依赖清单。隔离专用 Postgres/ParadeDB、Redis、假飞书/假网关，默认禁止公网出站。所有 URL 和 DSN 严格校验为隔离服务；生成唯一 namespace，不能继承宿主生产 ENV。
2. 空数据库启动、迁移及历史结构升级各做一次；校验 migration 版本、health/readiness，记录镜像/源码对应关系。scope=none 下至少两个 APP/Worker：所有 cron、manual、旧排队投递均不得访问假上游；paused、revoked 与原 cursor/ACL 保持不变；在线问答用独立假路径确认不被飞书门禁误伤。
3. 核验 DB 配置为 2、ENV 为 1、默认 4 时实际池为 2；移除 operator DB 行后为 1；无 ENV 后为 4；bootstrap 默认数字行不覆盖 ENV，operator 默认行覆盖 ENV；无生产写入。通过 Asynq Inspector 读实际池，而非只断言 resolver 返回值。
4. 两实例配置相同，放行一个合成 UUID；非白名单 manual/schedule/旧消息取消，重复消息幂等；无正文和凭证泄露。让第二实例 scope 或策略版本不同，必须由新运行时门禁拒绝灰度，不能仅靠离线工具报错。记录拒绝与出站计数为零。
5. 在专用 sync 中制造 pending/retry/scheduled 和一个阻塞中的 active 任务。暂停队列后证明 active 仍在运行，再运行控制器紧急出站阻断、取消 active、等待句柄退出；验证 claim/generation CAS 拒绝旧代写入、旧知识仍可检索。pending 消息走受支持取消终态，不删除任务/清 cursor/修改 paused；每种未执行路径明确保留、终态或恢复策略。
6. low 放入独立合成任务，确认仅暂停 sync 不暂停 low。maintenance 只有一个槽位时，active sync 排空仍可能暂时占用 low 容量；记录 low 最大等待时间与退出超时。不能用权重 2:1 宣称预留一个槽位。
7. 注入 Redis 失联、控制器停止/指标缺失、429、5xx、超时和配额拒绝，核对最后一次灰度出站时间、熔断 latch、sync 暂停回执、活动任务退出、fencing、无自动重启、审计与告警。控制器不能因 Redis 故障无法写暂停就继续出站；必须有独立 fail-closed 门禁。重启后仍保持冻结，恢复需要显式批准。
8. 保存 JSON/JUnit 与关联 ID、时间线、出站计数、旧/新版本检索断言、Redis/DB 终态；负面测试必须真实触发控制器和假 HTTP 服务，不能只用 mock 判断器代替。离线 manifest 附引用后核验。

本任务范围内由控制器集成 CI 提供实际隔离测试结果；本文所列步骤不是运行结果。若 CI 没有覆盖某项，该项保持缺口，不将模板中的 true 作为证据。

## 运维需要补充的清单与外部协助需求

以下文字可由用户直接转发，代理本轮未发送给任何外部联系人。

| 负责人角色 | 需要交付的数据或能力 | 接口、作用域及验收方法 | 缺失的 NO-GO 影响 |
|---|---|---|---|
| Embedding 网关管理员 | 已确认区域标称 1800 RPM、北京/香港 120万及新加坡 100万 TPM，uniapi 无额外频率/并发限制；仍需最小补件：**实际 region、主账号与 Embedding 具体模型 quota scope、同模型 recent usage、明确 granted canary TPM、可靠 token bound 验证**，并确认 in-flight/剩余额度与重试策略 | 只读管理 API/OpenAPI、带时间的配置导出或管理台报表；标注 main account/model/region、quota/used/remaining/reset_at 及采样时间；没有 API 可签署同等字段报告。24h/7d 实际流量按相同模型/账号共享业务拆分，不把不同 Rerank/QA 型号混算；不给凭证明文。TokenBoundVerified 必须有该模型 Tokenizer/请求格式的保守上界证明，不能仅“字符数看起来足够” | 区域标称已知仍不能决定余量；没有批准灰度 TPM 和 token bound 即 NO-GO，不能凭应用自设预算承诺不影响在线问答 |
| 网关/网络安全负责人 | 飞书专用受控出站身份/路由、独立于模型函数的硬 RPM/TPM/in-flight 与紧急阻断；断控时默认拒绝，不能影响在线 QA 路由 | 确认网关是否原生支持，或既有 egress 层控制策略。提供配置读取接口、策略更新接口的鉴权/审计规范及只读验证方法；灰度标识必须由可信端注入，不能依赖客户端任意 Header。隔离证明并发竞争不越界、429/超额阻断、控制器失联 watchdog、旁路不能出站 | 没有外部可信边界时，旧版/未知实例可能绕过应用新保护；不能宣称“全生产硬阻断” |
| 运维/平台负责人 | 全部 APP、独立/嵌入 Worker、standby、旧部署、定时生产者；每个源码/image digest/host/启动时间/角色；全部 Redis 主从/集群/Sentinel、DB、namespace、逻辑与物理端点映射 | CMDB/编排清单与各 Redis Asynq live servers 对账；实例 ID 与池 ID 一一归属。收集非机密配置 DB 行（value/last_modified_by）、实际 ENV、启动绑定值、全部队列权重；说明缓存刷新与滚动更新时间 | 无法证明无旧实例/其他 Redis 绕过；不能评估总并发或完成排空 |
| 运维/监控负责人 | 控制器执行身份、网关阻断/暂停 sync/取消 active 权限、watchdog、指标采样与告警路由、审计保留、失联及维护模式规则 | 隔离真实执行并提供动作回执与告警接收证明；ACL DRYRUN 只作权限预检。生产本轮不做注入，后续授权窗口仅核对部署/配置与隔离版本一致 | 自动停止链路不完整；指标缺失应保持关闭，不能放行 |
| 运维/DBA/备份负责人 | 本次一致性备份计划、数据库/对象/本地文件覆盖、加密与保留、完整哈希、隔离恢复证据、可回滚镜像 digest/config、兼容性 | 校验备份目录、全量 hashes 与恢复成功；实际从备份恢复到隔离环境后验证权限/知识/Chunk/向量检索。重新启动原容器不等于恢复成功；不传敏感备份到公开 CI | 不具备可靠恢复与回滚基础，禁止发布 |
| 飞书管理员/业务数据负责人 | 使用**现有连接器身份**的读取及导出权限证明；确认一个非关键源、真实 UUID、内容/流量规模及发布窗口 | 实时接口调用另行审批后执行，或管理员提供现有审计证据。校验 Wiki/Drive 列表、元数据、读取/导出及 DNS/SSRF；不扩大授权；没有父子配置/版本冲突/hold，业务负责人签认可灰度 | 目前无合格首源；不能把“最近成功 0 条”当作内容授权充分；没有合格源即 NO-GO |

## 后续获批发布前的执行顺序

1. 外部缺口关闭并审阅证据。提交最终代码/CI/独立 PR，保持 PR #35 不自动合并。批准合并或生产镜像构建是后续单独动作。
2. 生产发布包应固定已批准 SHA、不可变 digest、SBOM、迁移清单，并完成上一节隔离 Startup Smoke。没有该镜像证据不能只引用源码测试。
3. 发布前由获授权人员做一致性备份及隔离恢复核验；保存原配置和精确回滚 digest，确认低队列影响窗口。
4. 冻结所有旧调度生产者；队列全局暂停 sync；紧急出站默认 deny；等待或受支持取消 active，核验 lease/claim/generation 与数据库终态，无 active 不能只看队列长度，需核对 in-flight 出站与 DB 执行。任何未知实例/Redis/no-receipt 均停止。
5. 全部新执行者 scope=none 启动，控制器保持阻断，逐实例比较镜像、能力、策略指纹。旧版不能靠新白名单保护；旧版回滚时保持外部阻断与 sync 冻结。
6. 运行离线 manifest 检查，人工复核来源和实时性。先 scope-none，再单源 ready，两次都保持 sync 暂停；获批后的实际启用不是本工具的职责。
7. 灰度批准后只恢复一个经确认 UUID，按已批准预算和硬出站策略执行。监控缺失、429/5xx/超时/配额触发时保持 latch，不自动循环重试放量；源权限/内容不变量异常直接冻结，人工决定回滚。
8. 回滚只恢复已保存镜像/config；维持全局出站阻断、停止旧生产者、暂停 sync 并排空/fence。旧版不识别白名单。不得清 cursor、删除来源/任务/备份、关闭版本/SSRF/权限/原子发布保护或整库覆盖“恢复”。

## 2026-10-27 19:25（北京时间）旧来源停更截止

该时间为既有约定，不由程序自动延长，也不是删除旧来源的授权。若在截止前尚未完成单源恢复、完整计划周期与重启验收、业务签收、备份恢复及自动停止验证，建议用户/业务/运维在截止前审批：保留旧来源、旧知识、权限、Chunk/向量与备份；保持停更/冻结状态以供查询，不擅自恢复旧同步；把替换与停更窗口延期到“阻塞解除 + 完整验收及签收后”的明确新时间。

新截止应由负责人约定具体北京时间、范围与退出条件，本轮不填写未经确认的时间，不改 cron、不自动延长、不清理任何生产旧来源。建议 2026-10-26 19:25 进行人工状态评审、2026-10-27 17:25 再评审；仅作为会议建议，没有创建提醒或定时任务。若到期仍无批准，维持现有安全冻结与数据保留，升级给负责人决定，而不是自动启用或删除。
