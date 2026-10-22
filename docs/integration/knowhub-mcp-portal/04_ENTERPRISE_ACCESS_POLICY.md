# M1 企业知识发布与授权设计

第二批状态：安全规则已按 [ADR-001](13_M1_FINAL_DECISIONS.md) 定稿；发布审核人映射、内容负责人及企业员工资格来源仍 NEEDS_DECISION。已实现默认关闭的 M2 组件与正式 003034 双数据库迁移，未注册运行路由、未回填或修改生产资源。下方第一批建议如有冲突，以本段和 ADR 为准。

M2 最小持久化采用三张表（space/KB/Knowledge 策略）和现有 audit_logs，暂不新增 grant/outbox 表。拒绝默认、双人独立 review、resource_version 审核绑定、revision/CAS 和事务审计均已有源码。每次 propose 都收紧到 pending_review；revoke 使用当前版本，不受客户端过期 revision 阻挡，旧 approve 必须失败。无批准行不公开；新普通资源自动继承 hook 尚未启用。确切实现和测试见 [14](14_M2_IMPLEMENTATION_AND_TESTS.md)。

## 安全不变量

有效授权为 `(企业公开只读授权 ∪ 个人既有授权) ∩ 凭据操作能力 ∩ 资源有效性 ∩ 文档允许规则`。企业能力和个人 scope 分别求交再合并，不能先将全部企业 KB 塞进旧 Key 白名单，也不能将旧 TenantScopes 当作已授权公共内容。所有分支必须保留原 Caller 和 resource owner TenantID。

1. 普通员工需服务端认证，账号有效。企业员工资格须来自已认可的账号供应链；“任意注册且 active”不自动等于企业员工。供应链不明确时禁用公共入口，不强绑 SID 尚未验收事项。
2. 现有 `retrieve` 只保留原空间/KB范围；新增显式 `enterprise_retrieve` 能力只给 user_mcp，且须同时具备 retrieve。旧 Key 默认无该能力，创建/修改接口显式展示变化，轮换不改变权限。Tenant Key、platform Key、mcp_ endpoint token 不获得该能力。
3. Portal published + 有效空间是公共候选条件，单独不能授权。独立空间发布策略必须 approved；KB/doc 均满足可信准入，才授予精确 `Search/List/ReadParsed/WikiRead`。原文件 `DownloadOriginal`、空间进入/管理、创建 Key 都不包含在内。
4. 显式 mcp_blocked、文档 pending_review、来源失权/资源删除优先于所有 MCP 和 API Key 读取，包括 full_access Key。策略不能凭管理员身份短路。内部运维读取需要独立、可审计流程。
5. 企业公开撤销不撤销员工原有合法私有授权；离开空间实时撤销个人分支；来源失权和文档显式拒绝仍覆盖私有分支。Portal archived/draft 只撤销公共分支。

## 决策表

| 资源策略 | 企业分支 | 个人分支 |
| --- | --- | --- |
| 空间未批准/未发布/停用 | deny | 继续既有身份、空间有效性和 scope 校验；停用 deny |
| KB inherit | 已批准空间 + 可信准入后允许 | 既有授权 |
| KB state=approved、policy=inherit | 仍要求 Portal published、空间和文档批准、普通来源及资源有效；不另设 enterprise_open 枚举 | 既有授权 |
| KB restricted / revoked / pending_review | deny | 既有合法权限；不得绕过文档策略 |
| 文档 inherit | KB 授权 + 本文可信准入 | KB/个人授权 |
| 文档 restricted | 不因公开 KB 获权 | 必须显式当前有效的 user/tenant/organization grant，同时满足 Key 的私有范围 |
| 文档 mcp_blocked / pending_review | 所有 MCP/API Key deny | 所有 MCP/API Key deny；Web 内部合法权限另按文档策略判定 |
| 文档来源 ACL 未知/已失权 | 不自动公开；失权直接 deny | 不能用导入应用权限代表员工；失权按可信来源规则撤销 |

restricted 的显式 doc grant 本身不能突破旧 Key 的 TenantScopes/selected KB 边界。M3 最小版本只支持“scope 内显式文档授权”；未来若支持单独跨空间 doc grant，需新增明确凭据范围，不复用空 selected 列表表达 all。

本表定义目标语义，M3尚未接入旧REST/文件路径；M2只对新组件执行整洁库拒绝规则。不能据此宣称旧系统已经实施全部文档拒绝策略。

## 最小数据库方案

复用 users、tenant_members、organizations/organization_tenant_members、tenant_api_keys、APIKeyTenantScope/KnowledgeBases、audit 设施。不创建并行账号/Token/组成员事实。M2正式迁移003034仅新增三张策略表；审核历史复用原audit_logs：

| 表 | 主键/关系 | 核心字段、约束 |
| --- | --- | --- |
| enterprise_space_policies | tenant_id PK/FK tenants | 公共准入state、policy、content_class、source_acl_state、inherit_new_ordinary、revision、resource_version、提出/审核/更新人及时间、reason |
| kb_publication_policies | knowledge_base_id PK/FK knowledge_bases | 同一组明确拒绝默认字段；审核绑定KB与文档集合版本 |
| knowledge_access_policies | knowledge_id PK/FK knowledges | 同一组字段，policy支持inherit/restricted/mcp_blocked/deny/pending_review；审核绑定文档及chunk版本元数据 |

三表均使用state=pending_review/approved/revoked；policy默认pending_review，content_class/source_acl_state默认unknown，inherit_new_ordinary默认false。approved必须有不同的proposed_by/reviewed_by、reviewed_at和有效resource_version，revision>0，reason非空。提出者/审核者/更新者引用现有users；resource owner从真实KB/Knowledge关系解析，不再存第二份owner。索引和审计都不能反向授予访问。

policy变更和现有audit_logs同事务，审计失败回滚；CAS/行锁拒绝旧批准，revoke优先收紧当前版本。所有FK删除默认RESTRICT，读取时另查软删除/账号/空间/凭据状态，不靠FK作为权限。knowledge_access_grants与可选outbox留待M3评审，不在本批建立；未来grant必须限制主体类型、有效关系、操作、过期/撤销和唯一约束，不能复用审计行当授权。

## 兼容与默认继承

没有策略行的历史资源不进入公共分支；既有私有 KB 授权保留。不能使用 created_at/文件名/channel/Metadata 单独断言历史已审核。审核具体 KB 时生成覆盖所有历史文档的审核快照，禁止批量把所有缺行设 inherit。

未来继承需要空间与KB同时approved、双方显式允许inherit_new_ordinary，以及服务端受控普通资源准入、父策略revision和来源记录；外部同步、未知ACL、敏感或异常继承仍pending_review。M2没有实现创建hook，即使开关为true也不能让缺策略行的新资源公开。新增API不能信任客户端自报“普通”；更新/替换、复制、移动、重新导入必须保留最严规则或重新审核，不能因新ID失去限制。

第一阶段仅允许整库清洁试点；M2发现任一混合、外部、未知或未审核文档就HOLD整库。KB的approved记录不代替单文档历史审核，Wiki和附件也不因此开放。

## 接口草案与管理约束

拟扩展 Portal 管理服务及 policy service：GET/PUT `/api/v1/enterprise-access/spaces/:tenant_id`、GET/PUT `/knowledge-bases/:id/publication-policy`、GET/PUT `/knowledge/:id/access-policy`、PUT `/knowledge/:id/access-grants`、POST `/enterprise-access/reviews/preview` 与 `/apply`。preview 返回受控 ID/数量、版本、审批影响及 HOLD；apply 使用 review_id + expected_revision，重新解析权限和资源快照，失效则 409，不接受客户端合成授权快照。

提出范围由明确任命的内容负责人执行，发布由独立、经批准的审核人执行。现有system_admin/空间owner/KB creator都不自动取得该角色；具体映射NEEDS_DECISION，PolicyAuthority缺失即拒绝。拟议管理接口尚未注册；enterprise_retrieve只能用于批准正文，不授予策略管理能力。收紧即时生效，放宽需新提案与独立审核。

## 迁移及回滚门禁

已在 `migrations/versioned/` 与 `migrations/sqlite/` 新增003034，保留所有旧SQL，包括冻结生产3033源码。PostgreSQL用timestamptz/BOOLEAN与现有ID类型，SQLite对应时间/BOOLEAN及FK；能力复用原JSON数组，只在显式申请时进入个人Key，不回填旧Key。迁移目录加载、current_latest和逻辑对等记录同步更新，immutable历史不改。

up只建新表/索引，无自动公开DML，不修改Portal/member/Key。隔离PostgreSQL和SQLite空库/旧库up/down/up、默认拒绝、FK/CHECK、旧Key保持与CAS均已有实测，详见14/17和原始输出；并非生产执行或真实规模锁时间验收。down只用于未激活隔离环境；实际敏感策略生效后保留表与守卫，不退回无ACL读取。业务角色、历史资源批准和生产激活仍需单独审批。
