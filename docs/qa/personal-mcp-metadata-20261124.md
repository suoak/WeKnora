# Go Personal MCP 知识库元数据契约

## 基线与根因

开发基线为 suoak/WeKnora main `d4a9bf1f51b3cc2366256feae461758a69a8508e`；2026-11-24 拉取的 Tencent/WeKnora main 为 `71c32af9273482e0dad013d0e3bea2f4d719516c`。

线上 APP 镜像只读检查得到 revision `b911e746c6ab5122be8a910324abdca64cf60d9b`。该版本至开发基线的 `internal/mcpserver`、Personal MCP resolver/coordinator 没有代码差异；不能仅凭历史部署文档判断线上版本。

Go 有两个独立产品入口：工作空间 Endpoint `/mcp/:endpoint_id` 和 Personal `/api/v1/mcp/personal`。8443 代理转入 `/internal/mcp/isolated/personal`，同属 Go PersonalServer。前端的 Codex、WorkBuddy 等个人 Key 配置指向 Personal 入口。生产近七天已记录入站工具调用：go_personal 4,785、go_endpoint 54、python 13；这是调用统计，包含可能的测试/探针，不等同于真实用户数量。

Personal 原先把已授权 PortalKBTarget 转成 ID、空间、名称、public、capabilities、searchable 的精简 DTO，没有请求元数据投影。上游 Go 的工作空间工具已有 description/type，但没有所需创建人与可读文档数。旧 Python 的 list/get 工具转调 REST，不能替代 Personal 的员工身份、Key 范围、来源限制与独立发布校验。

REST 的 `enrichKBCreatorNames` 使用批量账号查询和统一显示名选择；本次抽出原有显示名规则供两处使用。Personal 的 SQL 只投影已授权 KB 的创建人 username，不读取或回退输出 email。旧数据、删除账号或无 username 时返回空字符串。

## 契约

`list_knowledge_bases` 无新增参数，保留顶层 JSON 数组和既有七个字段；text content 与既有输出安全检查保持一致。新增字段始终存在：

| 字段 | 类型 | 权威来源/含义 |
| --- | --- | --- |
| description | string | knowledge_bases.description，经现有文本安全处理 |
| creator_name | string | knowledge_bases.creator_id 对应未删除账号的 username；不输出账号 ID/email |
| created_at | RFC3339 string | knowledge_bases.created_at |
| updated_at | RFC3339 string | knowledge_bases.updated_at |
| document_count | integer | 当前 Key、用户与来源策略下可读的文档数，不是全库资产数 |
| type | string | knowledge_bases.type；现有 Personal resolver 仍只接纳 document 类型 |

`get_knowledge_base({"knowledge_base_id":"准确 ID"})` 返回一个与列表元素相同的对象。仅接受准确已授权 ID，拒绝名称、未知 ID、越权 ID及缺失参数；同名库通过 ID 区分。仅加入 Personal 的只读目录，不改变工作空间 Endpoint、REST 或 Python 工具。

文档计数复用本请求的授权目标及可读 document identities，不重新解析整套授权，不读取文档正文。元数据以最多 500 KB 为一批查询并连接创建人展示名。非平台本地服务上下文的个人发布文档先批量识别，再复用已有独立发布证明；未验证文档与 list_documents 一致地排除。所有调用仍由 guard 在输出前重新核验身份、Key、授权、来源和版本。

## 范围与风险

- 本次代码不新增权限、Key 能力、数据库迁移或数据回填。统一 main 发布时通过项目迁移器执行 main 已包含的 3051 迁移，不直接修补生产业务数据。
- 8443 仅新增只读详情工具；参数白名单、输出脱敏、写入拒绝保持有效。
- 名称/说明可能经现有安全过滤而替换 URL；不会为追求原文一致绕过过滤。
- 独立个人发布证明有不可省略的文档级校验成本；普通文档不增加逐文档元数据查询。
- Personal list_documents 保留 PortalMCPHit 原有字段，新增 file_name、file_type、type、parse_status、created_at、updated_at。先对授权文档 ID 分页，再批量查询本页元数据；默认每页 20、最多 100。关键词筛选保留原有安全处理后标题的大小写不敏感子串语义，使用最多 500 个标题的批次，不加载正文。工作空间 Endpoint 的独立 DTO 不在本次修改范围。
- Python 仅作源码对照，不改动、不下线；其少量已记录调用不能视作无人使用。

## 验证与发布

验证记录、性能对比、PR、合并 main 的构建来源、部署与回滚证据在执行后补充。顺序必须为分支测试 → PR/全部适用 CI 通过 → main 合并 → 从统一 main 构建 → 测试环境验收 → 生产发布 → 真实 Personal MCP Key 验收。不能以分支构建替代 main 发布。

回滚不涉及数据库：保留切换前 APP 镜像摘要、启动参数与挂载，失败时恢复旧 APP 镜像并检查健康、认证拒绝和只读 MCP 检索。前端、数据库、来源凭证与内容不应随本次回滚改动。


## 测试与生产验收证据

功能 PR：https://github.com/suoak/WeKnora/pull/262 。合并 main SHA：`b3431b77ac288edbea5b1b98fcdc656a11bc82b3`。PR 及该 main 的全部适用 CI 通过，条件跳过任务不计为测试通过。

测试环境使用独立合成数据，不复制生产数据。覆盖单元测试、SQLite、真实 PostgreSQL/ParadeDB 授权路由、Key 认证、删除与撤权、私人知识库、跨空间访问、同名 ID 区分、双入口只读边界、检索 BM25/vector/hybrid、SSE 和 3/5/10/20 并发。完整相关包回归约 519 秒；最终修正后的 PostgreSQL 集成测试、race、go vet 和已有 3051 正反迁移兼容测试均通过。拒绝场景同时接受 HTTP 认证拒绝与 MCP 工具错误，不把 HTTP 403 误判为缺少 isError。

501 个知识库的元数据分为两次 SQL 查询，文档元数据查询为零。1,007 篇合成文档的列表场景，包含授权解析的 SQL 从 1,116 次降至 163 次，减少 85.4%；样本耗时从 424.7ms 降至 247.1ms。普通一页 20 篇文档的元数据使用一次 SELECT。此结果为 SQLite 合成测试，不代表生产吞吐基准。

合并后的 main 原生二进制在测试环境完成启动、正常 REST 账号与 Key 创建、空授权范围、双入口只读工具契约和拒绝行为验证；有内容场景由上述真实 PostgreSQL 测试覆盖，生产随后使用已有真实 Key 验证。未完成带嵌入模型的新账号有内容初始化，不能把空范围启动测试描述为完整内容链路验收。

生产现有验收 Key 可访问 70 个知识库。上线后两入口均一次返回全部新增字段；70 组详情与列表一致，70 组文档列表 total 与 document_count 一致。只读事务限制在该 Key 已授权 ID 内核对权威数据，说明、创建人 username、创建/更新时间和类型均零差异。未知 ID、无效或缺失 Key 以及 8443 写入均被拒绝。新 get_knowledge_base 的成功和拒绝均正常进入现有调用统计。

生产同 Key 的三个样本中位数：普通入口目录 4,390ms → 4,578ms，8443 目录 6,156ms → 4,250ms；代表性文档分页 484ms → 250ms。目录样本有限且受生产负载影响，普通入口变化约 +4.3%，不据此宣称稳定加速或精确回归幅度。

部署镜像 `sha256:558c40a9fa663c2834ad217cd327875c888d86e073e7e19fcddcc4e806907b55`，二进制 SHA256 `48dbb6fbe0c070025a0836dde4145ddad76e183ecad27942c1b66c8cae526c65`。保留原配置、环境、挂载和运行资源；仅替换 main 二进制及对应迁移目录。原镜像 `sha256:aee25017d5be5137ee25817f776ef6ae8d25348af190b331b13bc0793887b83b` 用于应用回滚，3051 已验证兼容旧应用，不在应用回滚时降级数据库。

完整原始证据保存在操作工作站 `E:/AIData/Codex/personal-mcp-metadata-20261124`：`ci.json`、`main-ci.json`、`final-suite-status.json`、`runtime-smoke-status.json`、`release-status.json`、`production-probe-before.json`、`production-probe-after.json`、`authority-acceptance.json`。不将真实 Key、账户信息或私有部署参数提交仓库。客户端及并发结果以该目录的最终交付报告为准。
