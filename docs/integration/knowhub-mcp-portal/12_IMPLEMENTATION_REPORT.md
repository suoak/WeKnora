# 第一批 M0 + M1 交付报告

日期：2026-10-20（Asia/Shanghai）。交付位置：独立分支 `audit/knowhub-mcp-portal-20261020`，worktree `E:/code/WeKnora-worktrees/knowhub-mcp-portal-20261020`。

本批完成源码/上游/生产只读事实审计与 M1 可评审设计，未实现或启用公共授权、文档 ACL、个人 Go MCP 路由。M0 中版本/资源/模块核对 PASS；敏感内容分类、真实客户端与漏洞利用验收仍 NOT_TESTED；M1 模型 NEEDS_DECISION。本报告不宣称 M2–M6 已完成。

## 核心结论

1. 个人 user_mcp 生命周期与实时成员检查、Portal 元数据目录、团队 Go MCP 12 工具、access 共享授权和跨 KB 检索已有，ALREADY_SUPPORTED；不得重建。
2. discoverable 仍只意味着可发现。缺少批准空间→有效 KB→合格文档的企业只读授权；需要独立企业能力，旧 Key 不静默扩权。
3. 敏感 ACL 必须覆盖 05 所列服务/REST/旧 Python/新 Go/检索前过滤/外部模型输入/文件和衍生资源；匿名签名文件的一天公共缓存是即时撤销的明确设计缺口。
4. 生产 Python 实际为 weknora_api_key、无进程共享 Key。31 工具含管理/写入，但后端 capability 仍限制调用；未验证客户端 denyTools 或真实越权事件。存在两条有效 tenant full_access Key，需盘点用途，不擅自轮换。
5. 生产 17 已发布有效 Portal 空间、48 非临时 KB、2718 Knowledge。914 飞书系、1055 GitLab。历史审批与读者 ACL 未明，全部保持企业开放待审核；没有将任何历史 KB 设公开。
6. app/frontend e62b17e2d、Python 854f3b956、schema 3033；审计分支 be308d005、上游 HEAD e0c28bef、稳定 v0.8.3。生产 Python 模块摘要实际匹配审计 HEAD，不能仅凭镜像 SHA 推断缺失。

## 交付与状态

| 交付 | 状态 | 证据/风险/生产影响/回滚 |
| --- | --- | --- |
| 00–03 审计报告、脱敏生产证据 | IMPLEMENTED；事实 PASS，内容审查 NOT_TESTED | 指定源码函数、版本和查询见各文档；只读，无生产写入，无需环境回滚 |
| 04 发布授权、数据库/接口设计 | IMPLEMENTED（设计文档）；NEEDS_DECISION | 无生效 DDL/回填，不改变 Portal/member/Key |
| 05 ACL 全路径/候选过滤设计 | IMPLEMENTED（设计文档）；NEEDS_DECISION | 旧链路旁路尚未解决；混合 KB HOLD，不宣布收权完成 |
| 06–08 主体、搜索和 UX | IMPLEMENTED（设计文档）；NEEDS_DECISION | 复用现有引擎和组件；无新路由/UI，不扩权 |
| 09 验收矩阵与实测 | IMPLEMENTED；按命令分别 PASS/FAIL/BLOCKED | 不将源码阅读当生产权限验收，详情见 test-results.json |
| 10–11 迁移及回滚准备 | IMPLEMENTED（方案）；NOT_TESTED（演练） | 未切客户端、未退役 Python、未部署 |
| tools 审计/验证脚本 | IMPLEMENTED；已运行来源索引和只读采集 | 远端仅读，输出白名单；本地 HTTP 验证仅合成数据 |

本批未添加授权运行时代码，因此不会把未经评审策略接入现有 API。没有新正式迁移；表结构/索引/兼容要求在 04，正式迁移与双数据库验证在后续实现门禁中。原工作树保留，其未提交变更未复制到本分支。

## 文件级开发拆解

下表为待实施工作，不代表修改已完成。新增文件名为建议，实施前核对版本及同名已有改动。

| 批次/任务 | 具体文件与复用函数 | 要实现和验证 |
| --- | --- | --- |
| M1→M2 发布策略持久化 | 新 types/enterprise_access.go；repository/enterprise_access.go；service/enterprise_access.go；现有 interfaces、audit、Portal service | 五类资源策略/授予/outbox，review preview/apply、revision/CAS、受控审批角色 |
| M2 正式迁移 | migrations/versioned/、migrations/sqlite/ 新编号 up/down | 空库/旧库 up/down/up，零历史自动公开、零旧 Key 扩权，兼容生产 3033 后续迁移 |
| M2 批准试点解析 | repository/portal.go:ListPublished/resourceCountMaps；access/ 新 enterprise scope resolver | 独立公共读取 grant，发布/停用撤销，非临时/来源未知过滤；不改 discoverable |
| M3 文档策略 | access/knowledgebase.go、kb_search.go、files.go、message_files.go；新增 document_read.go | 精确 operation、策略优先级、grant 主体实时关系与来源失权 |
| M3 服务读取 | service/knowledge.go/chunk.go/knowledgebase_access.go/knowledgebase_search*.go/wiki_page.go；handler/knowledge.go；router/routes_knowledge.go/files.go | 全部 detail/list/batch/chunk/FAQ/图谱/衍生/文件路径，旧 Python REST 不能绕过 |
| M3 引擎 | types/retriever.go/search.go；repository/retriever 各驱动；service/retriever/ | 显式 DENY_ALL/allow-set 下推、空集保护、驱动 canary；不支持则隔离 KB/索引 |
| M3 来源及副产物 | service/datasource_service.go/feishu_personal.go/wiki_ingest*.go；resource binding 与缓存 | 同步/替换/删除/失权保留策略，衍生闭包、签名/短链收口及缓存失效 |
| M4 个人认证 | middleware/auth.go 抽共用 owner/key 校验；新 user_mcp_auth.go；types/principal.go/context_helpers.go；tenant_api_key.go | 既有 Key+新企业能力，真实 User 主体，旧 REST 兼容，拒绝假 tenant/header |
| M4 Go 接入 | router/routes_mcp_endpoint.go；mcpserver/server.go/scope.go/tools_retrieve.go/tools_wiki.go/resource_urls.go | 同一 server 多主体，八工具调用白名单，按 user/key 限流和个人 usage |
| M4 搜索 | agent/tools/search_knowledge.go；service/knowledgebase_search_fanout/storegroup/fusion/rerank/results | 带 owner Tenant 的授权目标，跨模型 rank 融合、truthful partial、外部模型前二次检查 |
| M5 UI/客户端 | frontend/src/api/portal.ts；Portal 页面、现有 McpServerIntegrationSettings.vue 与个人 Key API/UI；i18n | 复用原未提交选库改动，搜索/进入分离、权限可见、一次连接、三个客户端验证 |
| M6 运维 | compose/反向代理候选配置、隔离部署与验收/监控脚本、10/11 | 固定 digest，合成测试，生产审批与灰度；不在本批执行 |

## 依赖、工作量与门禁

估算为一名熟悉项目的工程师的有效开发/验证人日范围；不包含业务审批等待、未知生产异常修复和跨团队客户端排期。不是已完成工时或承诺。

| 里程碑 | 前置依赖 | 估算 | 完成门禁 |
| --- | --- | --- | --- |
| M1 定稿 | M0 + 企业主体/来源/发布责任人决策 | 2–4 人日 | 模型、默认兼容、全读取面与数据库评审；本批交付可评审稿 |
| M2 清洁 KB 试点 | M1 通过、精确资源审批、隔离环境 | 4–7 人日 | A 非成员可读试点且不进管理/下载/受限 KB；不公开历史未知 |
| M3 文档治理 | M2 模型与数据、所有引擎/文件/衍生资源能力 | 12–22 人日 | C/D、旧 REST/Python、候选前过滤、签名/缓存/source revoke 全路径；不通过则混合 KB HOLD |
| M4 个人 Go/统一搜索 | M2–M3 安全守卫验收 | 6–10 人日 | 真实 user、旧 Key 不扩权、八工具、跨 tenant/model/store、完整/部分失败与并发 |
| M5 Portal/客户端 | M4 稳定接口、发布管理员权限 | 5–8 人日 | 可检索/可进入分离，三客户端、轮换撤销、scope 展示、多语言 |
| M6 迁移准备/演练 | M3–M5、真实数据审批、观测基线 | 4–7 人日 | 隔离并行、新旧映射、安全回滚实测；生产动作另批 |

M3 是主要不确定项，跨存储匿名直链、历史 Wiki 来源不完整、引擎过滤上限可能增加工作或导致特定 KB 持续 HOLD。不能为了日期承诺移除门禁。

## 可立即做与需评审项

可立即在独立分支做：已有鉴权负例测试、源/工具/路由清单、纯接口/数据模型草案、合成测试夹具、迁移设计与隔离验证准备、共享授权调用图审查。不得将这些操作称为生产开放验收。

需先安全/业务评审：企业员工资格、谁能批准公开、哪些历史 KB/doc 合格、外部 ACL 信任与撤销 SLA、文档 grant 组语义、匿名资源既有链接处置、混合 KB/历史 Wiki 是否允许公开、旧 Key 增能力的明确用户操作、正式迁移和生产切换。

当前停止点：第一批审计和设计交付，等待上述模型及资源评审；未 commit、push、merge、release 或生产部署。后续可按文件表顺序开始已批准的隔离实现，无需重复构建现有 MCP/RAG。

第二批已按用户后续指令继续；本文件作为第一批历史交付保留。最新M1 ADR、默认关闭M2实现/迁移/合成结果、M3准备及尚未通过项请看 [第二批交付报告](17_SECOND_BATCH_DELIVERY.md)。M0原始证据未重做或覆盖。
