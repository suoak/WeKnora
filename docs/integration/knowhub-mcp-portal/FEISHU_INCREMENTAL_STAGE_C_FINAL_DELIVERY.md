# 飞书 Wiki/Drive 安全增量与 Go MCP V1 联合交付

当前状态：隔离合成联合验收及独立 RC3 补丁验证已完成；生产恢复 HOLD，旧镜像回退不能默认采用。生产保持不变。

## 管理摘要

已在打包服务器独立实例完成真实 APP、前端/Nginx、PostgreSQL/ParadeDB、Redis、内置 Worker 的联合验证。飞书与模型 HTTP 服务为合成夹具，账号为合成本地账号；不是生产来源、真实 SID 或正式模型验收。

Wiki/Drive 均通过稳定 Knowledge ID、实际索引更新、未变化跳过、500/429 保留旧内容及 Cursor、403/未知缺失拒绝、完整来源恢复。真实浏览器页面及 Codex 五个工具通过，敏感文档直接 ID 被拒绝。来源删除后的旧 Python、REST、MCP 和同一条未过期 HMAC 文件链接拒绝，schema 3036 备份恢复保留拒绝。实际 Worker 被终止及 Redis 中断的恢复通过。

仍有发布边界：独立测试不代表生产可解除停更；真实飞书权限事件与积压回放、正式 SID、正式模型、正式网络及生产数据覆盖待授权验收。当前一个 HOLD/revoked 来源可能使统一 MCP 请求整体拒绝，这是安全拒绝下的可用性限制。历史个人 Key 不会自动取得未来新私人空间授权，“我的全部知识”在该意义上尚未完全满足。

## 源码、镜像与实例

| 对象 | 标识 |
|---|---|
| RC3 HEAD | `e62b17e2d305358467821aeff1cce63ce1d61254` |
| 联合工作树 | `E:/code/WeKnora-worktrees/knowhub-v1-feishu-integration-20261021` |
| 联合分支 | `integration/knowhub-v1-feishu-20261021`，尚未提交 |
| 联合完整源码 SHA256 | `b47025118adfc4cff2d1f75cabc6f98265a0bb9ca1ad816f409099081947f0fc` |
| 联合业务源码 SHA256 | `1d533edfe33957dc6b74960594ff7d12ab33e4cfe670f631d10074fc9e9bad1a` |
| 联合源码包 SHA256 | `1d2477f7edebd0c48d11063db242366f7221bd91582a1b6fbafebb19acf641e3` |
| APP Image ID | `sha256:90844661b75ffe2682dc70793c459685afb34fd95a04fac6e3ac4531667515a7` |
| 前端 Image ID | `sha256:988cb86a46f0cb37d524d21bbf9d785b5f2a831f7af0eacc5fe7bb29a57a2350` |
| 独立补丁工作树 | `E:/code/WeKnora-worktrees/feishu-safe-incremental-20261021` |
| 独立补丁源码 SHA256 | `02a8c5eb187764a93381bc5d794d69a86fd046145fce7ab29c18f686b2434c2c` |
| 独立补丁业务源码 SHA256 | `2ac380a033af80f022d98a3b45cd5e30645b148d07cde8c76c46cce94787392b` |
| 独立补丁 APP Image ID | `sha256:0a0117c920c4fed402e7f744578b539a5ef5e6ef8da7c141ff5e9ac15031b73d` |
| Compose 项目 | `knowhub-v1-feishu-b47025118adf` |
| 服务器候选目录 | `/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-feishu-20261022/b47025118adfc4cf` |
| 回环入口 | `http://127.0.0.1:59079`，个人 MCP 为 `/api/v1/mcp/personal` |
| 数据库 | PostgreSQL 17.9，pg_search 0.22.2，vector 0.8.1，版本 `3036:false` |

镜像为本机构建，RepoDigests 为空；Image ID 不能冒充注册表 Digest。PG 扩展版本与生产已知版本一致，不代表镜像字节相同。APP 内置 Worker，没有新增独立 Go MCP 进程或后端监听端口。前端 Nginx 转发到 APP:8080。服务器只监听回环，客户端经临时 SSH 转发验证，不代表已开通内网或隔离网。

原冻结 V1 业务 SHA `5468da42c518cc83539040506c85b7964bdf2ae20826e641dbd030253316e231`、原包、实例及历史报告保留。完整源码 SHA 包含冻结当时文档和工具；冻结后修正的夹具及测试编排另存 SHA256 工件。没有用 Git HEAD 代替未提交代码版本，也没有修改原冻结包。

## 实现范围与调用链

| 源码 | 已实现内容 | 边界 |
|---|---|---|
| `service/feishu_legacy_staging.go::StageFeishuLegacyItem` | 私有解析、分块、Embedding、候选索引验证，成功后发布 | 当前普通扁平 document、共享 PG、受支持本地存储；不支持类型 HOLD |
| `repository/feishu_legacy_atomic.go::Begin/Prepare/Publish/Receipt` | 稳定 KID、来源与模型快照、版本围栏、事务可见切换、回执确认 | 不依赖先删旧知识再创建；不等于所有外部索引支持原子更新 |
| `repository/datasource_execution.go`、`service/datasource_execution.go` | Lease、执行代次、Outbox、重试与 Cursor 回执校验 | 旧任务不能把新执行代次视为自己所有 |
| `repository/feishu_legacy_quarantine.go::SetSourceUncertain/RestrictSource` | 先持久拒绝，再清理；未知权限、未知缺失 HOLD | HTTP 403/404/列表缺失不是可信删除证明 |
| `types/feishu_legacy_read*.go`、REST/文件/模型边界 | 最新来源状态约束读取、候选、模型及资源响应 | 无完整历史衍生来源图、外部 CDN/对象直链即时回收证明 |
| `migrations/*/003036_feishu_legacy_atomic.*` | 私有资源/尝试/索引候选及旧写入触发器围栏 | SQLite 拒绝受管理原子写入；不把 down 作为默认回滚 |
| `retriever/postgres/repository.go`、`portal_scope.go` | 真实 BM25 物化候选后保留完整服务端过滤，规范化索引内容与当前 Chunk 对照 | 保留 ParadeDB custom scan 过滤风险失败证据；不以应用最终后过滤替代候选安全 |

上述路径均位于 `internal/application/`，除明确标出的 `internal/types/`、路由及迁移。独立补丁不包含个人 Go MCP 产品源码。联合候选复用现有个人 MCP，未扩展工具、HR Provider、逐 KB 审批或 WorkMate 专项。

## 当前候选测试矩阵

所有相对证据路径以本目录下 `evidence/` 为根。完整命令、argv、退出码、开始结束及日志见各 `results.json`、`commands.json`、`result.json`，不以本文摘要替代原始输出。

| 门禁 | 结果 | 原始证据与实际范围 |
|---|---|---|
| 开发期完整 Go、race、vet、构建、迁移 | PASS（有 SKIP） | `feishu-incremental/linux-joint-release-source/results`；独立补丁对应 `linux-b2-materialized/results`，各 21 条退出 0 |
| 精确业务源码真实 PG canonical/race/vet | PASS | `linux-joint-release-source/canonical-probe2`；合法索引格式、真实 BM25 分数、SourceID、旧正文/标题、向量候选 |
| APP/前端同源构建 | PASS | `feishu-joint/build-b47025118adfc4cf`，13 阶段退出 0，含前端测试、类型检查及构建 |
| 默认关闭启动、测试实例启用 | PASS | `deploy-b47025118adfc4cf`，8 阶段退出 0，关闭 403、启用后未认证 401，ReviewEnabled=false |
| Portal 动态范围、私人空间、撤权、真实 PG 检索 | PASS（合成） | `acceptance-b47025118adfc4cf/http-business*`、`postgres-security-scale.log`；两个私人空间、发布/取消发布、Key 轮换撤销、直接 ID 与写工具拒绝 |
| 1024 维 HTTP Embedding、798/3584 异常 | PASS（合成模型） | 同上；不是正式模型注册表与生产端点验收 |
| 大 KB 与跨空间 Hybrid | PASS（有限样本） | 128/257/512/864/1001 文档；1001 文档 Hybrid p95 2733.287ms，3 KB/2 租户 p95 2848.195ms，均 6 样本；不代表生产 SLA 或 >2048 支持 |
| Python 完整回归、实际 HTTP/SSE SDK | PASS | `python-full.log`：55 测试；`python-sdk.log` 实际 REST；来源允许/拒绝另见生命周期和续测目录 |
| 完整 service/agent | PASS（有 SKIP） | `service-full.log` 34 条 `--- SKIP`、`agent-full.log` 4 条；包含环境缺失/扩展下载限制，不能宣称这些路径已验证 |
| Wiki/Drive 实际 Worker 生命周期 | PASS（组合证据） | `feishu-app-a4-b47025118adfc4cf/events.json` 两种来源的 `source-lifecycle-pass`；该轮整体仍 FAIL，原因是后续删除断言写成 200 而真实返回 204 |
| 删除、旧链接拒绝、备份恢复 | PASS | `feishu-app-post-a4b-b47025118adfc4cf/result.json` exit 0；明确续接 a4，不覆盖 a4 FAIL；相同未过期链接与拒绝状态恢复 |
| 实际 APP/Worker 终止及 Redis 中断 | PASS（限定故障点） | `worker-fault-b47025118adfc4cf/result.json` exit 0；终止于 Connector HTTP 阶段、发布前；单条测试日志注入 31 分钟老化，不声称自然等候 31 分钟 |
| 真实浏览器页面闭环 | PASS（本地合成账号） | `browser-b47025118adfc4cf/attempt-2026-10-22T01-50-50-314Z`；原路由、创建/查看/轮换 Secret、复制、工具发现、桌面/窄屏截图，无页面错误 |
| 实际 Codex CLI 五工具 | PASS | `codex-b47025118adfc4cf/attempt-20261022T015225Z`；页面复制配置、环境变量凭据，五工具完成、敏感 ID 明确拒绝；以实际目录为准 |
| 功能关闭及恢复 | PASS | `rollback-b47025118adfc4cf`；保留硬化 Reader 和 schema 3036，关闭 MCP 403、原文件链接与 REST 仍拒绝 |
| 独立 RC3 补丁运行 | PASS（离线配置、合成） | `independent-hotfix-offline-b47025118adfc4cf/result.json` exit 0；实际冷启动、Wiki/Drive Worker、稳定 KID、原 HMAC 拒绝、关闭功能仍保留保护，无个人 Go MCP |
| 直接换回旧 APP | FAIL / UNSAFE | `old-image-offline-probe-b47025118adfc4cf/result.json`：健康 200，但相同已撤权来源旧 HMAC 链接返回 200。探测命令 exit 0 表示观察完成，绝不是安全回退 PASS |
| 正式 SID/模型/飞书云端 | NOT_TESTED/BLOCKED | 缺独立授权条件，不使用真实生产来源、数据和凭据 |

## 历史失败、测试修正与未覆盖事项

原 FAIL 全部保留：旧索引内容与实际 writer 不一致、ParadeDB SourceID 过滤风险、关闭 custom scan 的不支持查询、冷启动首次 HTTP reset、当前首次 Worker 等待超时、a2 非 JSON 403、a3 夹具旧时间戳、a4 误断言 DELETE 200、首次续测恢复 schema 冲突。

测试修正均有独立脚本 SHA：真实 4xx 纯文本按拒绝保留；导出与块接口返回一致故障；合成恢复使用更新版本；通过官方 Asynq Inspector 提前执行指定任务 Retry，保留五次上限、payload、身份及断言；恢复库用 `template0`，不忽略 pg_restore 错误。自然队列退避时长未验收。a4 生命周期与续测的依赖关系必须一起审查，不能将 a4 整体 FAIL 改为 PASS。

独立补丁首次冷启动 FAIL 也保留：Go 诊断栈定位到 `NewDuckDB` 下载扩展，私网无公网出口；补充验证仅使用源码已有 `DUCKDB_SKIP_EXTENSION_LOAD=1`，不改 APP。`hotfix-startup-diagnostic` 保存 SIGQUIT 栈，范围仅自有无外部端口的探测容器。离线补充 PASS 不包含 Excel spatial/excel 扩展。正式部署需预装扩展或具备获准下载条件，不能把这项配置当成所有导入功能兼容证明。

旧镜像同样以离线配置补充验证：`old-image-offline-probe` 实际启动健康后，已撤权来源的原链接返回 200，明确判定直接回退 UNSAFE。探测使用新合成数据库、新空 Redis、文件只读挂载和无 host port；随后停止自己的探测容器。镜像的准确源码 HEAD 未知，不将它重标为当前 RC3 HEAD。此发现加强“保留硬化 APP/Reader”的回滚约束，不授权生产测试旧镜像。

尚未覆盖：真实云端可信删除/撤权事件入口、生产 2 Wiki/5 Drive 积压数据回放、所有历史派生内容来源、CDN/对象直链回收、非本地存储/非 PG 原子发布、混合敏感 KB 完整 ACL、真实 SID、正式模型、生产性能与网络。403、404、暂时失联及未知移动继续 HOLD，不误判已确认删除。已支持的可信本地数据源删除与云端可信事件不能混为一谈。

## 回滚与生产门禁

先关个人 MCP 和安全增量开关、重新停更，保留硬化 APP 的最新读取拒绝能力和数据库 3036，再执行经过验证的恢复。原 APP 可能缺少来源拒绝保护，不能直接覆盖回退后继续开放所有读取。保留原快照、数据库备份、候选镜像和卷，不默认 down。独立探测容器可停止，保留证据；只操作带当前候选前缀的容器。

生产从未修改，未解除 `FEISHU_LEGACY_SYNC_SUSPENDED`，未提交、推送、合并或发布。原约定停更期限为北京时间 2026-10-27 19:25；若正式安全门禁届时未通过，应由人工决定延长停更和撤权事件处理方案，不自动解除保护。

## 下一步及最终判断

1. **原始“我的全部知识”是否满足？** Portal Published 动态公共范围和当前 Key 已授权的多个私人空间已实现并通过隔离验证；未来加入私人空间仍受原 Key scope 限制，完整动态私人授权目标 PARTIAL。本轮未静默扩大旧 Key。
2. **Wiki/Drive 能否安全恢复自动增量？** 当前支持的 PG/本地存储/普通文档链路已通过隔离安全更新与故障恢复，但真实云端来源、权限变化和生产积压未验收，不能据此解除生产停更。未知缺失、撤权证据不足及不支持资源继续 HOLD。
3. **是否可先独立发布飞书修复？** 技术上可以形成独立 RC3 APP 候选，且独立运行已通过，不含 MCP 产品源码；3034/3035 携带原正式 SQL 保持连续链，新增 3036。正式发布仍需独立审批及真实来源、配置、迁移副本和回滚核验，本次不执行发布。
4. **Go MCP V1 是否具备正式测试验收条件？** 是，可以进入获准正式测试环境，当前联合候选的隔离合成闭环通过；不是生产发布 PASS。单来源 HOLD/revoked 导致统一请求整体拒绝的影响应作为正式可用性门禁，不能把这种保守拒绝宣称为正常跨库服务。
5. **能否申请恢复生产同步？** 可携带本报告进入发布/恢复评审，尚不能执行恢复。剩余真实门禁为：测试飞书应用/来源权限与积压演练、正式模型与 SID、生产数据/存储类型覆盖、恢复与配置确认、持久拒绝后旧版本回退方案，以及 HOLD 对业务可用性的处置。

建议顺序：先以获准测试应用完成真实 Wiki/Drive 的变更、403/429/5xx、缺失、删除/撤权和积压回放；核对生产来源属于已验证支持范围及 migration 3033→3036 副本；确认模型、SID、扩展和网络条件；处理 HOLD 影响及回滚限制；最后由人工决定灰度与停更策略。不新增 HR Provider、逐 KB 审批、WorkMate 专项或完整 M3 扩围。

全部交付 SHA、命令及证据文件由补充交付 Manifest 记录。冻结源码包、测试补充包与证据包分别校验，不把测试脚本修正伪装为已构建源码版本。完成后停止，不解除生产停更、不发布。
