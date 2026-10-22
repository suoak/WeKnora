# M0 技术基线审计

审计日期：2026-10-20（Asia/Shanghai）。本批范围为 M0 源码与生产只读审计、M1 设计；不实施企业开放、ACL 上线或生产切换。实现存在统一记 `ALREADY_SUPPORTED`，不代表测试或生产验收 `PASS`。

## 三条基线

| 对象 | 实测值 | 状态与证据 |
| --- | --- | --- |
| 原工作树 | `E:/code/WeKnora`，`feature/knowhub-sourceid-oidc`，HEAD `be308d0058db4e647cd5103ae27b39da3a20eace` | PASS：git branch/rev-parse/status |
| 原工作树改动 | 17 个已跟踪文件有改动，另有大量未跟踪企业集成文件 | 未覆盖、暂存或搬移；清单见 evidence/original-worktree-status.txt |
| 本批 worktree | `E:/code/WeKnora-worktrees/knowhub-mcp-portal-20261020`，分支 `audit/knowhub-mcp-portal-20261020`，同一 HEAD | IMPLEMENTED：只增加本批文档、证据、审计工具 |
| 上游稳定版 | [v0.8.3](https://github.com/Tencent/WeKnora/releases/tag/v0.8.3)，`3e22822c1d2397be730c458c7539d0eb8c113a24` | PASS：releases/latest 重定向及 git ls-remote |
| 上游 HEAD | `e0c28bef3562de7d2d78d6c1fa480a045468a40f` | PASS：git ls-remote upstream HEAD；git fetch --no-tags upstream HEAD |
| 分叉 | merge-base `bccb4b151bae403508da77fbb174efc79dc47c1a`；本地独有 271、上游独有 269 个提交 | PASS：git merge-base；git rev-list --left-right --count HEAD...上游SHA |
| 生产 app/frontend | `e62b17e2d305358467821aeff1cce63ce1d61254`，版本 `0.8.0-knowhub-rc3` | PASS：生产 image revision/version 标签；标签是构建声明，不是对二进制的独立重现构建证明 |
| 生产 Python MCP | `854f3b956ae3600e12a3ffd2fc40d4fe624ced74`，`weknora-custom/mcp:v0.8.0-knowhub.24-rc1-854f3b95` | PASS：运行容器与 image 标签；实际 Python 模块摘要匹配本地该提交及审计 HEAD |
| 生产数据库 | `paradedb/paradedb:v0.22.2-pg17`，schema_migrations `3033`，dirty=false | PASS：只读 SQL；未执行迁移 |

完整镜像 digest、采集 UTC 时间及数量均见 [生产证据](evidence/production-readonly-20261020.json)。app digest `sha256:b48c3a3bb52a7ccba7f1f2f388b4a7e3b505382c5e3cb9884f8833ce3fa89ed8`；frontend digest `sha256:538b2cda230265048c0f648c3ab200253a2b484163a4e14641580661a05a94ac`；MCP digest `sha256:14de96093a80c84863f73cb273a7982cc99a40989560f7d8432450a831a338b9`。

生产相关 `internal/application/access/`、`internal/mcpserver/`、Portal repository、个人 Key service、Knowledge 类型与本地审计 HEAD 的差异检查未发现变更；`internal/middleware/auth.go` 仅有个人上下文构建格式差异。此结论限指定文件，不等于整个生产版本与本地一致。生产 Python 模块 SHA256 为 `c0d2b07a68a0f1501bec7434abc2b3ad9e4b75f82e0be40aa18263f367db0a55`，见 [五基线摘要](evidence/source-baselines.json)。

扩展核对发现生产另有飞书 RC3 改动：`knowledge.go` 增加 personalRecovery 分派字段；`wiki_page.go` 增加 ReadETag/DependencyETag 的更新冲突检查；`datasource_service.go` 增加 LegacyFeishuSyncSuspended 准入/执行拒绝；`feishu_personal.go` 增加模式/恢复路径并拒绝已有导入记录直接更新（ErrPersonalUpdateDisabled）。这些是生产提交的源码差异，不代表当前所有开关已开启。本批不回退这些企业改动。完整 25 个关键文件函数行号与三基线摘要见 [source-index.json](evidence/source-index.json)；新功能应从经确认的候选发布基线衔接，不能直接部署较旧审计 HEAD。

## 已有能力及边界

| 能力 | 源码证据 | 实现状态 | 本批判定 |
| --- | --- | --- | --- |
| Portal 元数据发现、申请和发布 | repository/portal.go:ListPublished/resourceCountMaps；service/portal.go:SetStatus/reviewAccessRequest | ALREADY_SUPPORTED | 目录不授予知识读取权限 |
| 个人 Key 创建/范围/轮换/撤销/到期 | types/tenant_api_key.go；service/tenant_api_key.go:AuthenticateAPIKey/RotateUserMCPAPIKey；repository/tenant_api_key.go | ALREADY_SUPPORTED | 不重建凭据表 |
| 按当前账号、成员关系校验个人 Key | middleware/auth.go:authenticateUserMCPKeyRequest/attachUserMCPAuthContext | ALREADY_SUPPORTED | 要求单个 X-Tenant-ID，无跨 Portal 自动枚举 |
| 共享授权与执行空间隔离 | access/knowledgebase.go:ResolveKB/KBAccess.Context；kb_search.go；kb_shares.go | ALREADY_SUPPORTED | 真实 Caller 与执行 Tenant 分离，可复用 |
| 团队 Go MCP 12 工具 | mcpserver/server.go:toolCatalog/guardTool；router/routes_mcp_endpoint.go:RegisterMCPServerRoutes | ALREADY_SUPPORTED | tools/list 与 tools/call 双重白名单；默认含 ask |
| 跨 KB 搜索、模型/存储分组、部分失败 | agent/tools/search_knowledge.go:concurrentSearchByTargets；service/knowledgebase_search*.go | ALREADY_SUPPORTED | 不另建检索引擎；跨模型分数仍需评估 |
| 企业公开授权、文档 ACL、个人 Go 入口 | 现有模型、服务、路由及生产 schema 核对 | NEEDS_DECISION | 尚无本任务所需完整实现，不启用 |

上表源码路径省略共同前缀 `internal/`。实际测试结果见 [09](09_TEST_MATRIX.md)，静态阅读与生产只读统计不代替权限验收。

## 采集方法与限制

通过仓库外现有 Windows 当前用户 DPAPI 连接工具读取生产；严格验证已有主机公钥，不落盘解密凭据。仅 docker inspect、镜像 inspect、读取部署 Python 模块并在远端计算摘要和 AST 工具名、psql `BEGIN READ ONLY` 汇总。SQL 使用 stdin，10 秒 statement_timeout；stderr 不回传，失败仅记录错误类别。审计脚本 [production_readonly.py](tools/production_readonly.py) 可审查。未调用业务 MCP、SID 登录、外部模型或下载真实文档，未运行部署脚本。

M0 的版本和资源统计已采集；内容敏感性、历史审批、客户端 denyTools、缓存污染与真实越权事件为 NOT_TESTED。生产脚本只搜索本设计相关字段/表，不能证明不存在其他形式的所有权限机制。生产源内容未知，不能将任何知识判为已批准公开。

回滚：本批不改变运行环境，无数据库回滚需求；仅丢弃本 worktree 的新增文件即可。原工作树保留。未 commit/push/merge/release。
