# KnowHub MCP 当前状态与发布准备度审计

审计日期：2026-10-21，Asia/Shanghai。性质：源码、Git、既有报告与原始证据的只读审计；本文件是本次唯一新增交付文档。没有修复功能、启动新批次、运行新业务测试、访问生产正文、修改数据库、清理历史资料、提交、推送或部署。下文“当前”指本次审计时点；生产数量另标原始观察时间。

## 管理摘要（一页）

**当前结论：个人 Go MCP 的只读 MVP 已实现，并在隔离环境形成真实页面、HTTP、PostgreSQL/ParadeDB 和 Codex 调用闭环；可以进入正式测试环境交付，尚不具备直接生产启用条件。完整 M3 仍为 PARTIAL/HOLD。** 不给出缺乏统一分母的“完成百分比”，也不把组件测试通过等同于生产验收。

| 管理关注项 | 审计结论 |
| --- | --- |
| 最新成果位置 | `E:/code/WeKnora-worktrees/knowhub-mcp-b4-rc3-20261021`，分支 `audit/knowhub-mcp-b4-rc3-20261021`；第一至第七批成果仍未提交 |
| Git 与源码标识 | HEAD / 当前 GitHub main 均为 RC3 `e62b17e2d305358467821aeff1cce63ce1d61254`；审计前完整源码候选 SHA256 为 **`ca5c49a4545fec99f741559d76cfa2a9e2602e6bc4113bd4b20461567aad6d9a`**，6012 文件逐项复核无差异 |
| 已实现关键能力 | 个人真实用户 Key、显式 `enterprise_retrieve`、5 个只读工具、动态 Published 公共范围与私人授权并集、跨空间 PG Hybrid、KB 公共禁止例外、撤销复验、大 KB 安全适配、个人页面、默认无限期新 Key、Generic JSON / Codex TOML |
| 实际可用范围 | 合成账号/文档、真实 JWT/Key API、真实 PG 与 Nginx 测试入口；普通与 race 检索规模验证至 1001 文档/KB，受控 1024 维模型；Codex 有真实工具调用。测试入口现已停止，不是常驻员工试用站 |
| 明确未完成 | 最终整包正式部署验收、真实 SID 员工→个人 Key→真实模型→MCP 联合验收、实际 17/48 数据可检索覆盖与链接影响、生产模型质量/容量、正式域名网络；WorkMate 会话调用失败；完整文档/文件/衍生/来源 ACL 未闭环 |
| 生产能否上线 | **目前不能直接启用**。代码未形成审阅后的发布版本/镜像，生产 3033→3035 尚未演练真实数据副本，最终业务与真实模型/入口未联合验收，生产启用也未获授权。不是因为缺 OA/HR 实时在职接口或逐 KB 审批 |
| 下一阶段安排 | 冻结与审阅候选 → 默认关闭部署正式测试环境并演练迁移 → 普通员工及真实模型/数据闭环 → Codex 与撤权/负载验收 → 单独申请生产灰度和回滚窗口 |

**最优先五项任务：** ①形成可审阅源码/镜像指纹并核对并行未合入改动；②在正式测试环境完成实际 APP/前端/Nginx 装配和 3033 副本迁移/恢复演练；③明确验证 17 空间/48 KB 的动态实际覆盖，尤其正文链接保守拒绝和显式例外；④使用真实员工、实际 Embedding 与 Codex 完成业务和权限撤销验收；⑤完成性能、监控、关闭开关回滚及获批灰度。无需重新开发已存在的个人 MCP、逐 KB 审批或身份目录。

## 1. 审计口径与安全收口

状态分两层：`IMPLEMENTED / PARTIAL / NOT_STARTED` 描述源码，`VERIFIED / BLOCKED / NOT_TESTED` 描述证据或交付条件；测试行使用 `PASS / FAIL / BLOCKED / NOT_TESTED`。`HOLD` 表示尚不能开放该链路；`UNKNOWN` 表示现有证据无法确定。设计文档已完成不等于运行能力已实现。

本次先关闭了残留的本地 `knowhub-b7b-network-tunnel.py` 进程 PID 11868，按完整命令行核对后停止。随后检查本任务相关 Python/Node 命令行无残留；测试机 `docker ps --filter label=knowhub.batch=7` 返回空，exit 0；当前运行容器清单中也无 `knowhub-mcp-b7-...`。没有停止其他团队的 RC3、OA、历史 MCP 或数据库。最新合成 Key 的撤销/清理已有浏览器及客户端证据；本次不轮换任何凭据。

审计使用 `git status / log / diff / ls-files / worktree list / ls-remote`、文件 SHA256、源码阅读、既有 JSON/日志，以及只读 SSH `docker ps`。`git diff --check` exit 0；`git ls-files -u` 为空；暂存区为空。本次不重跑测试，以免把新执行与既有验收混淆。SSH 查询后 Paramiko 曾输出析构期关闭异常；远端两个只读命令均 exit 0，不把该本地提示算成业务测试结果。

证据入口：[第七批最终校验](evidence/batch7/business/final-validation.json)、[逐文件测试候选](evidence/batch7/business/candidates/)、[最终完整快照记录](evidence/batch7/business/checkpoint.json)。本审计没有覆写任何旧结果。

## 2. Git、工作树、生产及上游关系

### 2.1 工作树与未提交状态

| 工作树 / 分支 | HEAD | 成果及当前状态 |
| --- | --- | --- |
| `E:/code/WeKnora` / `feature/knowhub-sourceid-oidc` | `be308d0058db4e647cd5103ae27b39da3a20eace` | 用户原工作区；17 tracked 修改、608 untracked。包含其他 MCP UI、共享范围重验及浏览器测试，不能当作第七批部署源 |
| `.../knowhub-mcp-portal-20261020` / `audit/knowhub-mcp-portal-20261020` | 同上 `be308d005...` | 第一至第三批原始成果；10 tracked 修改、179 untracked，保留未覆盖 |
| `.../knowhub-mcp-b4-rc3-20261021` / `audit/knowhub-mcp-b4-rc3-20261021` | `e62b17e2...` | 第四批语义移植第一至第三批后，继续第四至第七批；本次审计前 35 tracked 修改、1096 untracked（含文档/工具/非忽略证据），常规折叠 status 为126行。没有暂存/提交 |
| `.../knowhub-oa-feishu-rc3-20261020` / `release/knowhub-oa-feishu-rc3-20261020` | `96b4f376578ad1e352b3e833b82cd0f0b2294189` | RC3 合并前发布分支；随后 main 合并形成 e62，不是新的 MCP 候选 |
| `.../oa-username-fix-20261021` / `fix/sourceid-jit-email-username-20261021` | `e62b17e2...` | 另有 `sourceid_jit.go` 未提交修改和两个用户名测试；尚未纳入 MCP 候选，本次未修改/合并 |

还有 `knowhub25-mcp-*`、`knowhub25-personal-mcp-lifecycle-*` 历史工作树，属于此前个人 Key/团队 MCP 基础开发，不是第一至第七批最新实现。不能按照分支名称推断其代码未进入 RC3；合并前须按差异审查。

原 `be308d005...` 不是当前 RC3 HEAD 的祖先（`merge-base --is-ancestor` exit 1），不能把两者当连续提交链直接覆盖。第四批采用保全快照、差异移植和冲突处理；原完整快照及 3033 来源见 [24](24_RC3_BASELINE_TRANSPLANT.md)。所有批次新增授权/个人入口仍是未提交文件，GitHub main 没有包含这些新能力。

### 2.2 当前 GitHub main 与生产基线

本次实际 `git ls-remote origin refs/heads/main` exit 0 返回 **`e62b17e2d305358467821aeff1cce63ce1d61254`**。提交标题为 `Merge KnowHub RC3: OA Code Flow TLS/UserInfo and Feishu application imports (#28)`，与用户确认的 RC3 生产源码参考一致；本地 `origin/main` 同值。

生产 APP/前端 e62、旧 Python 854f3b956、schema3033 的依据为 [M0 只读基线](00_BASELINE_AUDIT.md)；最新保存的生产 KB/模型清单观察于 **2026-10-21 10:39:19 上海时间**，见 [原始清单](evidence/batch7/production-inventory-readonly.json)。本次没有重新连接生产主机核验镜像/配置，所以**生产在审计瞬间的实际运行指纹为 UNKNOWN**，不能把历史观测称为实时认证。

本次只读检查的打包/测试机有常驻 RC3 `knowhub-rc3/app:0.8.0-oa-feishu-rc3-1e2ded5b6` 等多套实例，旧 Python `WeKnora-mcp` 仍监听8082；它们不是本次最终个人 MCP 源码。冻结候选测试运行在独立源码目录/容器，现已停止。**现有常驻测试网站、Git HEAD、最终未提交源码、冻结执行候选四者不能互换。** RC3 旧文档的“尚未合并 main”是历史时间点，当前以实际 ls-remote 为准；它们的 CI/员工验收也不能覆盖本次新 MCP。

### 2.3 腾讯上游同步

本次 `git ls-remote upstream refs/heads/main` exit 0：**`9110cb2da17000e9f8fcc90dadb59bce3e31392d`**；本地缓存 `upstream/main` 为较早的 `2c0d8384398ca756ee414eef01b71caf91e2caa6`。没有 fetch、merge 或 cherry-pick。

通过 [GitHub 官方比较接口](https://api.github.com/repos/Tencent/WeKnora/compare/2c0d8384398ca756ee414eef01b71caf91e2caa6...9110cb2da17000e9f8fcc90dadb59bce3e31392d) 确认远端比本地缓存前进38个提交；另逐文件查询两个版本的 `internal/mcpserver` 目录，避免比较接口文件数量限制遗漏。两版本均15文件，只有 `tools_ask.go` blob 改变，新增会话 `OwnerAuthSource` 绑定。当前个人五工具不开放 ask，这一差异不阻断只读 MVP，不能声称全量上游已同步。

RC3 已有上游内置 Go MCP 与12工具。相对本地缓存上游，RC3 的 `tools_retrieve.go`、`tools_wiki.go`、`server_test.go` 有差异，缺 `tools_annotations_test.go`、`tools_read_document_test.go`；涉及上游只读注释和 read_document 查询/分页契约演进。个人入口复用现有工具 schema/mcp-go 库并新增隔离主体包装，不是另建检索引擎。上游最新还有 pgsearch scoring、shared KB 发现等改动，未在本批引入；是否需要择取须依据正式环境实际兼容性判断，不要求先整体升级上游。比较详情参见 [上游目录](https://github.com/Tencent/WeKnora/tree/9110cb2da17000e9f8fcc90dadb59bce3e31392d/internal/mcpserver)。

### 2.4 指纹、快照与一致性

| 标识 | 含义及本次复核 |
| --- | --- |
| `e62b17e2...` | Git 基线，不包含未提交 MCP 改动 |
| `ca5c49a4545fec99f741559d76cfa2a9e2602e6bc4113bd4b20461567aad6d9a` | 审计前6012文件完整候选，排除 evidence；按原校验器 `SHA256(json.dumps(files, sort_keys=True))` 重算一致，无缺失、额外或内容变化 |
| `f7846934d480494cae5b3ef5665538be47442a38361f0c2e749ce000de64d08e` | 本次只读计算的4162个实现/测试输入集合：`internal/ cmd/ migrations/ mcp-server/ frontend/src/` 及 go.mod/go.sum/frontend package与lock；不含报告，便于后续文档追加时仍标识实现 |
| `31514a0c30a37f6391e83c470f157788bfec1bf13f78b90b2c56fbcd15e420e9` | 最新 Go 业务测试冻结候选；当前 Go/cmd/migrations/依赖逐文件一致；其前端随后改了3文件，另有最终前端证据，不能把该次 Go 运行说成测过新前端 |
| `b979d1ab41fca8b8befaedbd53932d374cb411c5f1a15da0fd6bf4bd4fda6c65` | 最新真实 HTTP/浏览器服务夹具候选；当前所查 Go、Python、迁移及前端输入均匹配 |
| `E:/AIData/Codex/knowhub-b7-business-final-20261021.tar.gz` | 最近完整快照，6898文件已有完整回读验证；archive SHA256 `ddef951c88bbfd1d1a1c300400b39a7a27a272261aa1ce47ae9ee48386410ef2`；本次未重打包或改写 |

最终前端三条命令均绑定776个输入，本次复核全部无差异。[final-validation](evidence/batch7/business/final-validation.json) 记录的 exit0 是一致性验证，不是全仓库/生产验收 PASS。本文件追加后，包含文档的全树指纹自然改变；上表 ca5 是**审计前业务交付集合**，不是含本报告的新全树。4162项实现指纹不因本报告变化。

### 2.5 重复、并行改动和历史候选

没有 Git 冲突标记/未合并索引，但存在需要发布审阅的并行成果：原主工作树 `McpServerIntegrationSettings.vue`、`mcpKnowledgeBaseOptions.*`、`mcp_endpoint_shared_revalidation_test.go`、`shared_scope_revalidation_test.go`，以及另一工作树 OA JIT 用户名修改。当前候选未纳入，不代表可以丢弃；合并时应语义比较，不复制整文件覆盖 RC3。

旧 `PortalKnowledgePublication.vue` / `PortalKnowledgeReview.vue`、`enterprise_employee.go` 与 PolicyAuthority/首次审批服务仍保留；当前两个页面虽然保留局部组件别名 `PortalKnowledgePublication`，实际 import 均为 **`PortalPublicKnowledge.vue`**。这些历史组件/接口未构成当前 Published 公共读取前置条件。M2旧批准模式与新 Published 模式并存是兼容代码，正式配置须明确选择当前 APP 装配，不能启用旧整体 REST 试点屏障代替新个人入口。

00～58共59份编号报告及证据保留；46～53及43中的旧首次审批/资格/256限制结论已被55～58和实际代码替代。历史 FAIL、临时失败候选、被否定的 WorkMate 脚本结果均保留，不清理、不改为 PASS。

## 3. 第一至第七批与当前功能矩阵

这里按用户本次指定含义使用“M5核心REST、M6个人GoMCP、M7产品”；早期12号报告曾将M4/M5/M6用于不同规划，不据旧编号误判已经交付的个人 MVP 尚未开发。

| 阶段 / 批次 | 当前源码 / 交付状态 | 真实验证范围 | 当前边界及来源 |
| --- | --- | --- | --- |
| M0 / 第一批 | IMPLEMENTED：源码、生产元数据、上游及旧Python审计 | VERIFIED：当时版本与数量事实；非生产内容分类 | 00～03、12；当前版本关系按本报告更新，不重复开展生产正文审计 |
| M1 / 第一二批及后续变更 | IMPLEMENTED：拒绝默认、主体与只读边界、链接/缓存设计；当前Published业务规则明确 | 设计已收口到55/58；正式运行权限行为以测试为准 | 不再待定逐KB审批或员工HR Provider；复杂M3用户级deny/来源治理仍PARTIAL，不阻断当前整体KB只读MVP |
| M2 / 第二批 | IMPLEMENTED：access公共Grant、策略表、revision/CAS、事务审计、3034迁移 | VERIFIED：SQLite/PG合成授权、旧Key不扩权、并发撤权、迁移；当前相关包有回归 | 13～17；旧approved逻辑保留但不阻断新Published分支；生产未迁移 |
| M3-A / 第三批 | IMPLEMENTED：DocumentReader、LiveDocumentBaseVerifier、精确Grant、3035 epoch、SQLite候选过滤、输出复验 | VERIFIED：组件及有限SQL/HTTP合成范围；后续补真实PG仅认证个人入口路径 | 18～23；全部读取面、所有驱动、所有写入/衍生仍PARTIAL/HOLD |
| 第四批 P0-A | IMPLEMENTED：RC3来源语义移植、正式3033迁移链 | VERIFIED：PG/SQLite 0→3035、3033→3034→3035、允许down/up、故障副本恢复、旧行不变 | 24/25/30；原be308正式升级FAIL保留；生产真实副本恢复NOT_TESTED |
| 第四批 P0-B | IMPLEMENTED：旧Python SSE撤Key的关联RPC错误/关闭 | VERIFIED：SDK2.2/2.3、HTTP/SSE单Worker；四项分判通过 | 26；原SDK超时FAIL保留；生产旧Python未改、多WorkerNOT_TESTED |
| M5 / 第五批核心REST | IMPLEMENTED：Knowledge detail/list/batch、Chunk detail/list、分页SQL、输出门禁和repository旁路HOLD | VERIFIED：真实service/repository/handler/router五条路由，单Worker旧Python→REST | 35～38；保护模式默认关闭且在完整APP启用会阻断其他业务，不能当生产全局ACL完成 |
| M6 / 第六批个人GoMCP | IMPLEMENTED：5工具、真实OwnerUserID、公共/私人并集、模型/store绑定、默认关闭APP装配 | VERIFIED：真实PG/pgvector/BM25、模型spy、并发、Key/Portal撤销、HTTP工具 | 39～45；早期3/4维、256文档及Provider注入范围已由M7扩展，不继承旧“0safeKB”业务结论 |
| M7 / 第七批最终业务 | IMPLEMENTED：Published动态范围、KB例外UI、个人单对话框、跨私人空间、大KB、现有Nginx同服务 | VERIFIED：动态合成PG、普通/race规模、浏览器及实际Codex；正式员工/模型/数据联合验收NOT_TESTED | 54～58，最终证据分business与published；不能称全M3或全客户端PASS |
| 最新Key有效期 / Codex模板 | IMPLEMENTED：新user_mcp默认无限期；旧Key/省略PATCH与rotate保持；JSON与环境变量TOML | VERIFIED：HTTP/race、41项前端测试、类型/构建、真实浏览器；最新UI模板复制后实际CLI重新连接NOT_TESTED | 58及business证据；早先真实CLI成功不能替代新模板复制流程全验收 |
| M3-B文件/衍生/来源完整治理 | PARTIAL：有设计、内存来源proof与合成broker；实际存储/缓存闭环未完成 | NOT_TESTED/BLOCKED：持久lineage、旧匿名URL/CDN即时撤销、飞书/GitLab可信ACL回收 | 15/20/37；相关资源HOLD，个人MVP不提供文件/Wiki/FAQ/Graph工具 |
| 生产切换 / 旧Python退役 | NOT_STARTED（本次MCP生产实施） | NOT_TESTED | 有运维方案和隔离迁移证据，无生产启用、迁移、Key批量改动或Python下线 |

## 4. 新版个人 Go MCP：15项关键回答

| 问题 | 当前实现与证据 | 未验证或限制 |
| --- | --- | --- |
| 1. 内置工具数 | 原团队Go MCP `Server.toolCatalog` **12个**：list_knowledge_bases、search_knowledge、grep_chunks、list_documents、read_document、ask、wiki_search、wiki_read_page、wiki_index、add_document、update_document、delete_document | 端点可再过滤；不是所有员工开放12个。默认团队工具含ask/生成行为，不能称团队端点严格只读 |
| 2. 个人开放数 | `PersonalServer`仅注册并在tools/call再次限制前5个，**5个全部只读** | 不开放ask、Wiki及3个写工具；不要求首版增加到12 |
| 3. 实际URL | 源码固定 **`/api/v1/mcp/personal`**；正式配置形式 `https://<KnowHub现有Web域名>/api/v1/mcp/personal` | 未发现已启用的新生产可用URL，正式域名/外部MCP_PUBLIC_URL须验收，不编造可用地址 |
| 4. 端口/协议 | APP同进程8080；Stateless Streamable HTTP；GET/POST/DELETE注册。外部复用80/443 | 测试为loopback临时HTTP+SSH，不是正式443/TLS通过；DELETE为传输动作，不删除知识 |
| 5. Nginx | 现有 `/api/` 可转发；隔离测试已真实Nginx精确路径验收 | 生产Nginx未改，生产链路未验收；现有泛用代理重试规则需针对MCP POST审阅 |
| 6. 个人认证 | Bearer→PersonalMCPAuth→既有Auth，要求有效user_mcp、非FullAccess、OwnerUserID、retrieve、active真实用户；公共分支再查enterprise_retrieve；live key/成员/范围复验 | 不信任身份Header；本地用户有效性不等于实时在职认证。没有新HR接口/Provider依赖 |
| 7. 新旧Key期限 | `userMCPExpiry`未传到期即nil；UI默认never，90天显式发送日期；PATCH/rotate省略保留 | 原Key不回填/延期；只改变新建user_mcp默认值。后端HTTP与race已测；生产未创建此版本Key |
| 8. 公私并集 | 动态Portal公共KB ∪ 用户独立私人KB；公共Grant不产生成员/管理权。公共禁止或取消发布不会撤掉独立私人权 | 私人分支仍受当前Key scope、成员、共享/Agent已有授权；不是读取所有系统KB |
| 9. 多私人空间 | 每请求枚举KB并检查现行权限，跨workspace服务端重建受控上下文；浏览器实测tenant1和7同时可见 | 新Key默认包含创建时scope-options的全部有效私人空间。未来新加入但未列入Key scope的私人空间**不会自动扩Key范围**；需更新原Key范围，不重建Key。selected旧Key仍收窄；复杂shared/Agent组合未全测 |
| 10. 新空间/KB/文档 | 动态数据库目录、Published、有效document KB、completed/enabled file/manual正文；无需团队MCP、approved或静态48ID | 新文档只有索引就绪才命中；测试新建KB/发布走真实REST，解析索引有受控夹具；真实所有同步Worker未联合认证 |
| 11. 取消发布撤销 | 每请求解析，SQL候选前检查Published、liveGrant模型前/输出前重验；历史document/chunk按现归属验证 | 隔离撤销/竞态通过；不承诺撤回已交付客户端的内容，也不代表旧匿名URL已撤销 |
| 12. >256文档 | PG专用可信whole-KB关系过滤，非巨大ID列表；128/257/512/864/1001实测普通与race | >2048文档快照拒绝；显式document_id allow-set仍限256，显式KB请求列表限128；不是无限规模或其他驱动已支持 |
| 13. approved依赖 | APP明确 `PortalPublishedDefault:true` / `PublishedDefaults:true`；无approved记录也授权 | M2批准模式供旧组件保留；3034/3035表仍是技术依赖，缺表失败关闭，不代表需补审批 |
| 14. 多余流程/Provider | 当前页面使用PortalPublicKnowledge，发布仍原Portal管理，只有显式KB例外。KnowHubPersonalUserVerifier直接核实既有账号 | PolicyReference是整体启用配置，非逐KB审批；旧审批代码存在但ReviewEnabled默认false。不能误配成旧模式 |
| 15. 与Python并行 | Go内置APP路径与旧Python独立端口/路径无冲突，未删除旧服务 | 源码与隔离分别验证；真实生产并行灰度、共享负载与代理未测，旧Python不自动获得新动态Portal权限 |

源码入口：[个人server](../../../internal/mcpserver/personal.go)、[团队12工具](../../../internal/mcpserver/server.go)、[个人认证](../../../internal/middleware/personal_mcp_auth.go)、[APP装配](../../../internal/application/service/portal_mcp_composition.go)、[路由](../../../internal/router/routes_personal_mcp.go)、[Key期限](../../../internal/handler/user_mcp_api_key.go)。

## 5. 最终业务规则逐项核对

| 最终规则 | 当前判定 | 实现与验收边界 |
| --- | --- | --- |
| Published全部有效KB默认公共 | IMPLEMENTED / VERIFIED（document KB） | `portalPublishedDocuments`直接取Published；当前生产清单48个均document。FAQ等未来非document KB不在本版支持范围，不能承诺所有类型自动开放 |
| 17空间48KB按业务默认纳入 | 理论范围已重新计算；实际数据读取NOT_TESTED | 无逐KB批准需求；实际新MCP未部署，不能宣称生产已开放48KB |
| 后续新空间发布自动纳入 | VERIFIED | 动态PG Draft→Published；最新publish通过原Portal REST完成 |
| Published空间新增KB自动纳入 | VERIFIED | 原Owner创建KB REST→同Key自动发现；不存在静态ID白名单或配置修改 |
| 文档新增/更新/同步后有效索引自动可搜 | VERIFIED（受控索引/同步边界）/ PARTIAL（真实Worker） | 不比较旧批准版本来阻断普通更新；epoch和canonical chunk校验防旧索引正文，飞书入口有独立合成测试 |
| 不逐KB人工批准/二次确认 | IMPLEMENTED / VERIFIED | 当前页面无首次确认流程、无额外发布免责声明/弹窗；SystemAdmin执行原Portal publish即可；Owner仅管理既有KB及显式禁止例外 |
| 非成员用个人Key读取公共知识 | VERIFIED | 公共KB无需目标空间membership；来源tenant在SQL中绑定，不能借公共Grant管理该空间 |
| 私人知识独立合并 | VERIFIED（两私人空间） | 仅Key现行授权空间动态KB；未来新私人tenant需原范围更新，属凭据约束，不能静默扩权 |
| 未发布空间不自动公共 | VERIFIED | Draft普通新空间、敏感例外、其他用户及直指ID负例有证据 |
| Portal撤销/归档 | 撤销VERIFIED；Archived独立最终E2E NOT_TESTED | 源码仅匹配published，因此归档会排除；不把源码推断写成已执行归档测试 |
| 空间失活、KB移出/软删除 | VERIFIED（合成真实PG） | canonical实际归属/有效性，旧引用拒绝；本版KB有效性是非删除/非临时/document，不存在已验收的另一个KB启用状态开关 |
| Key撤销/账号失活/权限查询失败 | VERIFIED（对应合成身份/故障） | 正常Key认证、live范围及输出复验，失败关闭；最新UI撤Key下一tools/call401 |
| 内网/隔离同Key同范围 | VERIFIED（测试APP与Nginx） | 不另建知识授权体系；隔离正式通道NOT_TESTED |

### 5.1 PolicyAuthority / Grant / CAS 是否冲突

当前 `NewPortalPersonalKnowledge` 明确选择Published默认模式，`EnterprisePublicReader`从现行快照签发精确只读Grant。它不查询“是否存在历史approved”作为准入条件。`Grant`是请求内授权证明，`CAS`用于可变例外/旧策略并发安全，均可复用而无需维护公共白名单。

`NewConfiguredPortalService`将旧审批API挂在独立的 `ReviewEnabled`；KB例外由 `ExceptionsEnabled` 控制。恢复例外写回pending/inherit即可恢复Published默认，不要求approved。`PolicyReference`和`Enabled`是全局默认关闭/受控启用约束，**不是需要重新等待业务审批方案**。正式测试应固定ReviewEnabled=false，验证例外管理权限与审计；不能自动启用旧Owner→Reviewer首批流程。

### 5.2 理论公共、显式限制与实际范围

[重算记录](evidence/batch7/published/production-scope-recalculated.json) 使用2026-10-21 10:39:19只读元数据：17个Published active空间、48个有效document KB、2718条Knowledge，2713条元数据标记就绪；5个空KB，4个KB超过256，最大864。所有KB模型配置维度1024，现存索引维度包含1024/798/3584，不能据此认定每条向量完整或模型输出一致。

**理论范围17/48；新例外表尚未在生产安装，不能将“未配置例外”写成“内容无敏感”；按最后核实部署事实，新个人MCP实际生产可读KB为0。** 本次没有新生产快照，审计瞬间数量变化UNKNOWN。旧报告“0个可验证safe公共KB”是旧approved口径，不再阻断当前业务授权，也不作为48KB内容不安全的结论。

此外，有一个影响实际可用性的明确源码限制：`document_search.go::safeEnterpriseText`保守拒绝包含 `http://`、`https://`、对象存储协议、`/files/`、`/r/`、图片Markdown等文本；个人读取/候选补全会使用该安全检查。它避免在尚无broker闭环时泄露可绕过撤权的链接，但**普通飞书/GitLab文档含链接也可能导致读取/检索报拒绝**。现有生产统计未检查正文，不能计算最终可读比例。P0应在获准的测试数据副本中测出实际影响；本次不放宽或修复安全检查。KB标题/空间名安全检查也可能使整次范围解析失败，而非只隐藏一个结果。

未标记混合KB敏感文件不受完整文档ACL保护；已知explicit deny conservatively HOLD整个KB。首版可使用已确定的KB禁止企业公开例外处理明确敏感KB，无须给其余普通KB补approved。

## 6. 检索、模型、撤销与读取面

### 6.1 真实驱动路径

`repository.ListPortalMCPCandidates`动态枚举有效document KB，无旧129候选截断。`PortalMCPResolver`逐目标加载可信snapshot，绑定principal/Caller/Key/tenant/KB；`SearchContext`只产生服务端opaque授权上下文。客户端KnowledgeIDs只能收窄，额外tenant/KB、未知参数、过滤上限及不支持驱动均拒绝。

PostgreSQL `portalCanonicalPredicate`在BM25/向量LIMIT之前连接实际Knowledge/Chunk/KB/tenant，检查当前归属、启用与解析状态、text chunk、索引内容与canonical内容一致、Published与显式禁止。whole-KB模式的空KnowledgeIDs表示**该opaque上下文内已经认证的KB关系**，不是legacy空allow-set代表全库；真正空授权集合返回空/拒绝。向量采用受控精确候选扫描，不能冒称ANN高性能已认证。

`BindPortalMCPAdapters`使用目标owner的store/model registry，验证store归属、engine类型、模型维度及向量有限值，禁止任意模型fallback。支持当前个人路径的PG/ParadeDB；其他驱动公共检索fail-closed。SQLite的M3细粒度合成driver验证不等于个人公共whole-KB可在SQLite生产提供同等检索。

检索前、Embedding前、候选补全、可信model consumer和输出边界有live复验；真实HTTP spy证明特定测试下未授权canary没进入模型。**当前个人产品主要返回检索文本，不注册ask；Rerank/LLM consumer spy不代表生产RAG/Agent全链已接通。** 已发给客户端/外部模型的文本无法追回，撤销语义是阻断后续请求和尚未输出/发送的边界；本版不能承诺不存在任何两个检查间的极小并发窗口或跨系统原子撤权。

### 6.2 读取面当前覆盖

| 读取面 | 当前覆盖 | 结论 / 首版影响 |
| --- | --- | --- |
| 个人Go5工具 | live身份、公共/私有解析、PG候选前过滤、正文读取、响应复验 | VERIFIED隔离MVP；正式装配/数据待验收 |
| 核心REST五条路由 | Knowledge detail/batch/KB knowledge list；Chunk list/by-id，服务与输出边界 | IMPLEMENTED/VERIFIED于默认关闭M5试点；不是当前生产全局开启 |
| 其他REST标签/摘要/历史revision/复杂neighbor | 部分有组件/拒绝guard | PARTIAL/HOLD；完整统一放行未认证 |
| 旧Python REST/HTTP/SSE | 真实SDK→M5五路由及撤Key协议 | PASS单Worker合成；生产旧模块未替换，不继承新Portal动态公共范围 |
| 团队Go12工具 | RC3已存在权限与白名单 | 保持原功能；不等于个人新Grant或文档治理通过 |
| RAG/chat/Agent流式与重试缓存 | 有局部scope/模型前guard、相关包回归 | PARTIAL；不是完整敏感ACL验收，个人首版不开放ask/写入 |
| Wiki/FAQ/Graph/摘要/多文档衍生 | 设计及内存proof，缺完整持久来源闭包 | HOLD；个人工具不开放，不能据此阻断普通document KB MVP |
| 文件/预览/图片/附件/导出/旧HMACURL | 合成broker与文本保守链接门禁 | PARTIAL/HOLD；未认证对象直链、CDN撤销，不提供任意文件下载 |
| 飞书/GitLab来源权限 | 现有导入/同步组件、M7飞书受控入口；来源ACL治理未闭环 | 导入应用可读≠员工来源权限；不承诺实时继承源ACL。既有legacy来源暂停守卫不因本MCP自动解除 |

## 7. 内网与隔离只读部署

当前个人服务的五工具同时受到注册表和调用时白名单约束，即使客户端直接构造写工具名也拒绝；不能仅靠denyTools。`GET/POST/DELETE`用于MCP协议，**必须允许POST**，不能按HTTP方法推断知识写入。

两边可复用同一路径、同个人Key、同权限计算、同APP进程；后端无需新增端口或服务，现有Compose APP8080 / frontend80可承载。443由实际TLS入口终止。当前无必要另建只读后端路径；以后若个人内网增加写工具，必须先把隔离入口绑定到独立只读registry/path，保持同进程与授权服务，不能让它继承新增写工具。

现有Nginx `/api/`可路由，但正式入口尚未验收。隔离测试模板只放行精确 `/api/v1/mcp/personal`，其他REST、管理、团队MCP、文件等返回403；关闭缓存/buffering/POST重试。正式隔离网还需DNS、可信TLS证书、路由、防火墙/ACL、Authorization保留、代理超时与重连验证，以及禁止绕过入口直接访问APP8080/旧Python/通用Web。是否另加Nginx监听取决网络通道；**不需要为此另起Go服务**。

已实测通道为本机→SSH→测试机loopback Nginx→Go APP夹具，最新测试Nginx临时端口49601、APP夹具33379，均已停止，不能作为员工可配置的正式地址。旧 [54](54_UNIFIED_MCP_NETWORK_AND_READONLY.md)、[网络检查](evidence/batch7/published/network-check.json) 与 [最新配置实物](evidence/batch7/business/nginx-isolated-config.conf) 是证据/模板，不是生产已安装配置。本次不开通任何网络。

## 8. 前端与真实用户体验

| 功能 | 源码/当前体验 | 验证分层 |
| --- | --- | --- |
| 我的MCP管理 | 复用现有MCPAccessKeys入口，创建个人Key、列表、详情、状态、停用、重新生成Secret | 前端相关41测试/类型/构建PASS；真实浏览器创建、连接、撤销PASS；不是全部按钮每组合都浏览器验收 |
| 简化创建 | 主个人入口一个对话框，只填名称/有效期；固定retrieve+enterprise_retrieve | 实际UI创建Key，scope包含两个私人tenant；不需手选Portal空间 |
| 无限期默认 | 前端ref/reset为never，后端create缺期限即nil；90天显式传date | HTTP/race+UI PASS；旧日期、能力不变已有断言 |
| Generic配置 | 统一URL+Bearer JSON，一次性Secret展示 | 模板、浏览器连接PASS；不是所有第三方客户端格式均认证 |
| Codex配置 | `[mcp_servers.knowhub]`、统一url、`bearer_token_env_var="KNOWHUB_MCP_KEY"` | 最新模板单测/浏览器PASS；需用户另设环境变量。实际CLI早期有五工具成功，最新复制模板→CLI流程未重跑 |
| Portal管理 | 原发布管理就是公共范围来源；新增面板展示范围/KB禁止例外 | 真实JWT/REST publish/unpublish和例外流程有证据，无重复审批提示 |
| 私人范围 | 默认创建当前scope-options全部私人空间，KB all模式动态 | 两私人空间实测PASS；旧selected/将来新tenant不会无声扩Key。高级编辑仍有原范围/能力选项，需要产品文案继续明确 |

源码：[MCPAccessKeys.vue](../../../frontend/src/views/integrations/MCPAccessKeys.vue)、[配置模板](../../../frontend/src/views/integrations/personalMcpConfig.ts)、[Portal公共面板](../../../frontend/src/components/portal/PortalPublicKnowledge.vue)。

**不同证据不能替代：**

- 前端41项为相关八组测试，不是全前端1577项重跑；类型检查/构建分别exit0。
- 浏览器自动化使用真实Vue/JWT/Key API/Go MCP，账号与文档为合成、模型受控；未进行SID登录仪式。
- Python MCP SDK集成是真HTTP/SSE与真实M5 REST，单Worker；不是新版Go实际Codex客户端。
- Codex真实exec有服务端tools/call和撤销证据，非只看配置文本；对应较早客户端候选，最终Key/UI改动后的完整CLI路径待正式测试补一次。
- WorkMate 2.4.0真实initialize/tools/list发现五工具PASS；最终对话会话没有实际知识tools/call，FAIL。用户已要求停止排查，本次未调用或修改WorkMate。

WorkMate `workmate-local-configured-attempt2.json`原脚本误把ToolSearch参数匹配为实际调用而报exit0；[人工复核记录](evidence/batch7/published/client-acceptance-reviewed.json)明确FAIL，且其中单个`actual_tools_call:true`残留字段与最终`server_actual_knowledge_calls:0`不一致。以服务端协议和最终判定为准，不据该字段声称成功；证据本次只指出不修改。首轮最新浏览器radio隐藏input超时FAIL也保留，改用可见标签后原断言通过，不覆盖原结果。

## 9. 最终候选的测试与兼容性矩阵

### 9.1 候选及命令索引

为缩短表格，以下代号都是冻结**文件候选**，不是Git提交。完整argv、容器参数、起止UTC、exit、原始输出均可由相应results.json旁`.log`/`.exit.txt`复核。

| 代号 | 候选SHA256 / 原始命令索引 |
| --- | --- |
| B | `31514a0c30a37f6391e83c470f157788bfec1bf13f78b90b2c56fbcd15e420e9`：[最新业务Go](evidence/batch7/business/linux/results-31514a0c30a37f63-business/results.json) |
| U | `b979d1ab41fca8b8befaedbd53932d374cb411c5f1a15da0fd6bf4bd4fda6c65`：[最新HTTP夹具](evidence/batch7/business/linux/results-b979d1ab41fca8b8-clients/results.json)、[浏览器](evidence/batch7/business/browser/result-attempt2.json) |
| A | `50d88df3cad2e4050e9d2e0512035e93f7eae053f0c19d2b59d193137d733353`：[相关完整包/race/PG/Python](evidence/batch7/published/linux/results-50d88df3cad2e405-all/results.json) |
| S | `e393014e31e8a6bbd15415eae83dd0a148615c54e1fdd5315e3369b8df049c24`：[独立Redis补齐后完整service/Agent](evidence/batch7/published/linux/results-e393014e31e8a6bb-service/results.json) |
| I | `a59c5bf118c539b3bdf0e495b1d264e3e44bf193b460d8ee8ebab8f03c285b13`：[普通PG/规模](evidence/batch7/published/linux/results-a59c5bf118c539b3-integration/results.json) |
| C | `3c24a26d469f2e03ec78d3f7a0c004989764719d42a63502ea404a6dfb7ae8e0`：[早期真实客户端](evidence/batch7/published/linux/results-3c24a26d469f2e03-clients/results.json)、[复核结论](evidence/batch7/published/client-acceptance-reviewed.json) |
| F | 当前776前端输入：[最终三命令与逐文件SHA](evidence/batch7/business/local/20261021-164557-frontend-results.json) |
| P4 | `04bed643e30d6a7e9bf6bd071e919722dfbe7e0eb6b73784f07fcd25eeafd3e4`：[第四批来源/结果索引](evidence/batch4/delivery-index.json) |

环境：隔离Linux Go1.26.7/CGO/SQLite3.46.1 headers、PG17.9、pg_search0.22.2、pgvector0.8.1、Redis7.0；Python3.11.16/MCP SDK2.2.0；另有Windows SDK2.3历史协议验证。真实模型spy是受控1024维值，部分早期M6为3/4维。没有把这些描述为生产Embedding服务。

当前与A相比，仅运行时代码 `internal/handler/user_mcp_api_key.go` 改了新建默认期限；另有前端与测试夹具变化，B重跑了handler/router/mcpserver完整包、相关race/vet及动态PG，U与F再验证页面。S的service/Agent应用源码及迁移/Python与当前一致；不是把A整套FAIL改成最终整包PASS。**尚无一次以最终发布镜像运行全部必需检查的统一流水线。**

### 9.2 测试矩阵

| 项目 | 判定 / exit | 真实命令或入口 / 候选 | 证据与适用边界 |
| --- | --- | --- | --- |
| 最新受影响Go包完整测试 | PASS / 0 | `go test -tags sqlite_fts5 -p 2 -count=1 ./internal/handler ./internal/router ./internal/mcpserver`，B | B personal-core.log；当前Go输入一致 |
| 相关核心Go全包 | PASS / 0（历史集合+定向补验） | A `go-core`：access/...、repository、retriever/...、database、types、middleware、router、handler、container、mcpserver、config | A results/log；变化包B补验，不是最终全仓`go test ./...` |
| 全仓最终Go / 正式CI / 原生AnyDoc构建 | NOT_TESTED / — | 当前未提交候选无正式CI | RC3基线曾有完整Go/CI，不继承到本候选；正式测试发布包须构建验证 |
| 新期限HTTP/单元race | PASS / 0 | B `go test -tags sqlite_fts5 -race -p 2 -count=1 -v ./internal/router ./internal/handler -run 'TestPortalM7PersonalKeyDefaultExpiryHTTP|TestUserMCPExpiry'` | B personal-expiry-race.log |
| 授权场景race | PASS / 0（指定场景） | A `go test -tags sqlite_fts5 -race ... -run 'TestPortalM6|TestPortalM7|TestSourceIDM7|TestEnterpriseSnapshotM7|TestDocumentM5|TestDocumentM3|TestEnterpriseM2'` | A race.log；不是全仓race；最新REST写入版动态PG并未重跑race |
| Go vet | PASS / 0（相关包） | A `vet`及B `go vet -tags sqlite_fts5 ./internal/handler ./internal/router ./internal/mcpserver` | 两份results；不是当前全仓静态门禁全部通过 |
| PG/ParadeDB关键词、向量、Hybrid、grep、多KB | PASS / 0 | I普通/A race `go test -tags sqlite_fts5,knowhub_m6_postgres [-race] ... ./internal/router -run 'TestPortalM6RealPostgresPersonal|TestPortalM7RealPostgresScale|TestPortalM7PublishedDynamicRealPostgres'` | I/A postgres-personal.log及EXPLAIN；真实SQL/受控模型；最终动态REST版B另0 |
| 跨空间/两私人空间联合 | PASS / 0（合成） | B动态PG；U浏览器五工具HTTP | U browser/result-attempt2.json及client-protocol-b979d1ab41fca8b8.jsonl |
| 动态发布/新KB/文档/撤销 | PASS / 0（已列范围） | B `go test ... -timeout 5m -run '^TestPortalM7PublishedDynamicRealPostgres$'` | B postgres-personal.log；发布/新KB真实REST，解析索引就绪部分直接夹具；Archived独立E2E未测 |
| 大KB规模与性能 | PASS / 0（合成规模）；生产容量NOT_TESTED | I/A 128/257/512/864/1001，三KB两空间/四并发 | [ordinary-postgres-metrics.json](evidence/batch7/published/ordinary-postgres-metrics.json)：864 Hybrid P95=1761.598ms，1001=1960.756ms，6样本/每文档一Chunk；不是SLA，多Chunk真实负载未验收 |
| 模型输入spy/维度拒绝 | PASS / 0（受控） | A/I/B同PG集成与HTTP Embedding/Rerank/LLM spy | 未授权canary不进入测试模型；配置/输出维度不匹配拒绝 |
| 实际Embedding维度、召回质量、多模型store正式配置 | NOT_TESTED / — | 保存元数据1024、代码registry装配已实现 | 没有真实远端模型输出、全部索引命中率或生产召回基准证据 |
| service完整包 | A FAIL / 1；S PASS / 0（仍有SKIP） | `go test -tags sqlite_fts5 -c ... ./internal/application/service`后Python3.11镜像执行 `service.test -test.v -test.count=1` | A缺Redis4项失败保留；S加隔离Redis/internal网络后重跑整包，有33条SKIP，不能称所有用例执行通过 |
| Agent tools完整包 | PASS / 0（仍有SKIP） | S同样编译/执行`./internal/agent/tools` | Excel/DuckDB离线扩展等SKIP，非生产Agent ACL全闭环 |
| Python完整回归Linux | PASS / 0 | A `python -m unittest discover -s mcp-server -p 'test_*.py' -v` | python-full.log，55 tests；Python输入当前一致，tomllib受控3.11环境，不再沿用第二批3.10失败为当前Linux失败 |
| 旧Python真实REST HTTP/SSE | PASS / 0（单Worker） | A tagged构建 `sqlite_fts5,knowhub_m5_integration`；`router-python.test -test.v -test.count=1 -test.run TestDocumentM5PythonActualREST` | A router-python-sdk.log；旧Python生产实例/多Worker/所有工具不在覆盖内 |
| SSE撤Key四项属性 | PASS / 0（隔离候选） | P4真实SDK2.2/Windows2.3；后续A旧REST重验 | [26](26_SSE_KEY_REVOCATION_FIX.md)：server auth、SSE错误/EOF、SDK有界异常、撤销后REST业务读取为0分别PASS；历史超时FAIL保留 |
| Windows Python完整55项 | FAIL / 1；BLOCKED_OS_PRIVILEGE | `.../venv/Scripts/python.exe -m unittest discover -s mcp-server -p test_*.py -v` | [最新Windows命令与输入](evidence/batch7/local/20261021-120148-windows-python-results.json)：WinError1314 symlink；不跳断言。不是Linux部署首要阻塞 |
| Windows TMP/TMPDIR/TEMP与readiness | PASS（既有专项）；全Python仍FAIL | 第二批middleware及transport，第四/五批cwd及DEVNULL修复 | 14/17/34，平台修复存在，不能据此把symlink全套标绿 |
| OA/SID兼容 | 相关包PASS / 0；最终真实联合NOT_TESTED | B handler/router与S service；历史RC3真实员工浏览器 | [RC3最终证据](../rc3-20261020/RC3_FINAL_CI_FIXES.md)证明基线登录/三格式/退出；当前MCP只合成JWT，未重测真实SID→Key→MCP |
| 飞书Docx/Sheet/Bitable | 现有包/基线有限PASS；当前端到端NOT_TESTED | A飞书入口 `go test ... ./internal/application/service -run TestPortalM7PublishedFeishuIngressPostgres` exit0；RC3三格式真实导入历史 | M7远端与解析模拟；RC3三格式曾completed/verified/检索，复杂Bitable汇总截断与大表仍DEFERRED；不自动升级为新MCP三格式实测 |
| Portal发布/KB业务路由 | PASS / 0 | B真实JWT、Owner建KB、SystemAdmin发布/取消，Viewer负例 | B postgres-personal.log；无第二审批，无伪造body tenant扩权 |
| 前端相关测试/类型/构建 | PASS / 0、0、0 | F：`node ...tsx... --test`八组文件；`node ...vue-tsc... --build --pretty false`；`node ...vite... build --configLoader runner --outDir ...` | F精确argv、41tests、776输入；非全前端重跑 |
| 浏览器业务闭环 | 首次FAIL；最终PASS / 0 | `tools/batch7d_browser_flow.cjs`，U | [result-attempt2](evidence/batch7/business/browser/result-attempt2.json)：创建无限期Key/两公两私/五工具/禁写/Portal撤销/Key401；真实SID未测 |
| MCP SDK集成 | PASS / 0（已列transport/身份） | P4/A实际PythonSDK HTTP/SSE；Go真实HTTP/browser协议 | 不等于所有桌面客户端通过 |
| 实际Codex CLI | PASS（四场景判定）；撤Key原始exit1是期望负例 | C客户端脚本中真实 `codex exec`，Generic命令细节见工具与原日志 | [客户端复核](evidence/batch7/published/client-acceptance-reviewed.json)和对应protocol；不是最新UI模板已在正式URL连接 |
| 实际WorkMate | FAIL；连接发现PASS，会话调用FAIL | 保留attempt6～8及真实会话/服务端日志，最终0次知识call | 早先错误exit0不予通过；本次不重试 |
| Key/Portal撤销、隔离及工具限制 | PASS / 0（合成当前范围） | B/U和[network-check](evidence/batch7/published/network-check.json) | 同Key双入口list+Hybrid一致、伪造Header/非MCP路由/写工具拒绝；正式网络NOT_TESTED |
| PG/SQLite正式迁移链 | PASS / 0（历史正式源码隔离）；真实生产副本NOT_TESTED | P4正式runner3033→3035/down/up及0→3035；当前A database套件与PG夹具0→3035 | [25](25_FORMAL_MIGRATION_ACCEPTANCE.md)、P4 pg-live-cas.log与results；迁移SQL/启动器/map逐文件仍匹配P4，非生产已迁移 |
| 生产迁移/恢复/灰度 | NOT_TESTED / — | 当前无生产执行 | 需真实副本规模、触发器开销、约束删除兼容、备份恢复和关闭开关验收；不批准直接down丢策略数据 |

### 9.3 历史FAIL处置

第三批正式3033升级失败与SSE撤Key后SDK超时两份原始FAIL均保留。P4在正确RC3来源上实际修复/通过，不能说原候选变绿。A的service全套exit1也保留，S的完整包exit0为后续新证据，SKIP如实保留。Windows Win1314、WorkMate会话工具、初始浏览器selector及过期夹具FAIL均不由聚焦PASS覆盖。

## 10. 是否具备交付条件

| 评估项 | 判断 | 具体剩余问题 |
| --- | --- | --- |
| A. 功能研发完成度 | 核心只读MVP已实现；整体PARTIAL | 不应重做个人MCP/UI/动态检索。当前限制是>2048、非document/非PG、含链接文本与复杂私人共享组合；完整M3另立范围 |
| B. 隔离开发环境可用性 | 已有可复现VERIFIED闭环，当前停止 | 冻结候选、PG、模型spy、Nginx、浏览器/Codex可重现；不是当前常驻可访问的员工测试站。启动需原候选与测试配置，不是任意RC3容器 |
| C. 正式测试环境部署准备度 | 有条件具备，待发布包与配置/迁移验证 | 需审阅未提交改动、冻结镜像/前端、启用配置引用、实际服务依赖与MCP_PUBLIC_URL、Nginx/TLS；正式项目启动而非只用夹具 |
| D. 真实员工业务验收准备度 | 工具/UI基本具备，验收NOT_TESTED | 普通SID账号、两个私人空间、已发布与未发布公共数据、实际模型、真实Codex模板、撤销与例外；不需要新增HR Provider |
| E. 生产灰度准备度 | BLOCKED，当前不建议启用 | 最终版本/正式部署证据、真实17/48实际覆盖、生产副本迁移恢复、模型与性能、运维观测/回滚及用户发布批准未齐 |

最可能影响“上线即能用”的不是缺更多工具，而是：最终整包是否真能按正式APP装配启动、实际模型/store/index是否一致、含普通链接的文档会被拒绝多少、迁移触发器和当前KB约束是否影响原业务、入口证书/代理与员工Key范围是否正确。这些都要证据，不靠设计结论。

## 11. 最短后续交付计划

以下是建议，**本次不执行开发、修复、合并或部署**。

### P0：正式测试及生产上线必须完成

| 顺序 / 工作 | 必须交付的证据 | 放行范围 |
| --- | --- | --- |
| 1. 冻结可审阅版本 | 当前35 tracked及新增源码逐文件清单、完整依赖/工具/迁移manifest、无Secret扫描、与main/并行OA及共享测试改动的语义审查；经用户批准后才提交/建正式发布版本，锁定APP/前端镜像digest | 防止把HEAD或其他工作树作为第七批；不需要先全量同步最新上游 |
| 2. 部署正式测试环境与迁移演练 | 从该冻结源构建正式APP/前端；默认关闭启动检查，再在授权测试配置启用personal_mcp及PolicyReference；ReviewEnabled=false；配置异常KB管理；MCP_PUBLIC_URL/TLS/现有Nginx；实际3033测试副本→3035、空库、允许down/up/恢复、旧Key/成员/Portal/原知识不变、SQLchecksum及dirty记录、触发器性能/硬删除兼容 | 不使用全局M5拒绝屏障替代新个人入口；生产仍不写入 |
| 3. 证实实际知识范围 | 对获准的数据副本/测试资源，核对17/48理论清单、实际有效KB/就绪文档/索引与显式禁止；统计链接保守拒绝影响，选择可真实使用的普通文本验收；测新发布/新KB/索引/同步/移出/删除/归档/独立私人权 | 不是再审批48KB；对明确敏感例外设置正常KB限制；不能放宽链接守卫后无证据开放 |
| 4. 真实员工与模型/客户端验收 | 用现有SID普通账号正常登录→页面新无限期Key→复制Codex模板并设环境变量→正式测试URL五工具；两个公共空间+两个私人空间、其他员工隔离、无权ID、机器/旧Key、禁写、Key轮换/撤销、Portal撤销；真实Embedding输出1024及store所有权/索引匹配、检索相关性；必要受影响全包/race/vet和原失败完整包统一流水线 | 先以已经验证可用的Codex交付；WorkMate若非强制首发客户端可P1，不重复拖住KnowHub |
| 5. 规模/回滚与获批生产灰度 | 实际Chunk数量和48KB并集的延迟/超时/四并发以上基线、授权复验成本；开关关闭/Key撤销即时拒绝、监控/日志不含Secret、错误/授权拒绝指标；备份恢复及旧APP/前端回滚演练，保留3034/3035策略数据；受批窗口、小范围员工灰度、实际URL冒烟及17/48覆盖对账 | 不先开全员；不要求立即迁走旧Python，不把隔离网络开通作为普通内网Codex首发前置 |

第2～4项可在同一正式测试候选上连续完成，避免为了新增功能反复换候选。若发现实际正文链接使大部分检索失败或整包启动/迁移问题，再单独批准最小修复并冻结重验；本审计不越权修复。统一流水线应记录所有SKIP，并按首发范围明确哪些需要补执行，不能继续以分散历史PASS替代正式版本门禁。

### P1：上线后短期完善

- WorkMate客户端会话工具装载/调用兼容，需其真实tools/call证据；若业务要求首发必须WorkMate，则仅此项提升为对应客户端P0，不把连接成功算通过。
- 未来新加入私人tenant的Key范围更新体验、复杂shared/Agent多空间组合与旧selected范围说明；不能以默认扩权解决。
- >2048文档预算、实际多Chunk规模和批量snapshot/重复复验优化；若首发实际存在超限KB，则该KB支持提升为P0，否则保持清晰HOLD。
- Windows真实symlink权限环境全套复测；更多客户端、完整前端/i18n回归、上游read_document/工具注释的兼容性择取。
- 旧Python多Worker/代理会话亲和与各SDK版本矩阵；只读新入口不依赖旧Python修复部署，旧生产服务保持原状。

### P2：长期优化

- 混合KB文档级个人deny、持久化Wiki/FAQ/Graph/历史引用来源闭包、文件broker/旧匿名URL/对象存储/CDN撤权，以及飞书/GitLab可信源ACL同步。
- 更多检索驱动安全预过滤、ANN受控性能、跨模型质量优化、更多上游工具及以后独立只读路径演进。
- OA/HR实时在职、独立目录等只有以后业务另行要求才评估；当前不开发、不作首版前置。

## 12. 回滚、生产影响及停止点

当前生产没有本批写入、迁移、启用或凭据轮换，无须本次执行生产回滚。新增个人能力默认关闭：缺 `Enabled`、`PolicyReference` 或依赖即拒绝；原REST、OA/SID、Portal和旧Python继续原行为。M5全局试点保护策略在container中仍为 `NewDocumentReadPolicy(false,nil)`，不得误当新个人MCP所需开关。

未来回滚优先关闭个人入口及其精确代理路径、保留审计/策略/Key记录，再按已验证镜像退回；不要自动down3034/3035丢权限元数据，不批量改成员或轮换所有Key，不删除知识/历史快照。3035含Knowledge/Chunk DML/TRUNCATE epoch，不能仅依赖updated_at；完整来源/衍生写入覆盖仍属M3后续，普通document MVP不可据此宣称所有资源版本已闭环。

本报告完成后停止。保留00～58全部报告、旧工作树、快照、失败与原始测试；本次仅关闭了本任务残留隧道并新增本文件，没有修改任何功能源码、历史测试证据、配置或生产资源。下一步应是**正式测试候选验收与发布准备**，不是启动新的架构或功能批次。
