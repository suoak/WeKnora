# M3 调用链核对、夹具与下一批开发顺序

状态：详细设计与接口夹具完成；不是完整文档 ACL 实现。M0 基线证据保持原样。下面据本地已有实现整理实际入口和拦截点；未对生产做破坏性验证。

## 调用链与授权依赖

| 链路 | 必须统一的授权关系/顺序 | 当前缺口及下一步 |
| --- | --- | --- |
| Python get_knowledge/list_knowledge/list_chunks → REST routes_knowledge.go → KBAccess middleware → knowledge/chunk service → repo | 原 Caller + credential scope → doc/KB/tenant binding → document rule → metadata/body | 新三表没有接到旧服务；先加共用 `ResolveKnowledgeRead`，再批量/list SQL。不能只修改 MCP Handler |
| Python hybrid_search / Go search_knowledge / RAG → knowledgebase_search.go → buildRetrievalParams → CompositeRetrieveEngine → processSearchResults → rerank/prompt | scope 在 fetch candidates 前下推；enrichment/FAQ retry/neighbor/parent/建议都保持 allow-set；模型调用前重验 | 已有 KnowledgeIDs 不能表达空集；新增 DENY_ALL/allow-set 接口尚未接引擎，先逐 driver/mode 证明，不支持则阻断 |
| read_document/grep → mcpserver/tools_retrieve.go、agent/tools/search_knowledge.go | exact doc grant；read context/grep 每一项匹配父文档和 owner tenant | 现有以 KB 为主；增加 doc守卫、chunk-id 猜测、跨员工并发反例 |
| Wiki search/read/index/revisions/backlinks → wiki_page.go/wiki_ingest*.go | 页面正文、summary、标题、chunk refs/生成来源闭包及历史版本的全部源文档授权 | ChunkRefs 不保证所有生成文本可追溯；未知来源整页拒绝，不能仅删引用；Wiki dependency ETag 是并发保护，不是员工 ACL |
| FAQ entry/search → Knowledge/Chunk；图谱 entity/edge → source chunks；summary/questions → source doc | “可读所有依赖”才可读派生正文；跨 KB/tenant 依赖逐项验证 | 需要 persisted provenance、版本和同步失效；不能用 tag/metadata 自报 allow |
| 图片/原文件/preview/export → router/files.go + access/files/message_files + catalog bindings | 原 KB/会话/路径权限 + 当前 doc ACL + 独立 download operation | 需收口匿名签名、短链、对象直链、公共缓存，详见 15；混合绑定严格拒绝 |
| 来源同步 → datasource_service.go/feishu_personal.go → Knowledge/Chunk/index/Wiki | 应用可读/导入者可读不等于员工群体可读；来源主体/ACL revision/失权与版本保留 | 飞书个人导入校验和 RC3 recovery 是已有能力；GitLab token可读项目也不是全员权限。未知外部资源在 M2 全部 HOLD |

参考第一批 `evidence/legacy-tool-routes.json`（31 工具精确映射）与 `source-index.json`（函数及基线摘要）；这些原始证据不重新生成覆盖。第二批源码差异和候选 manifest 单独保存。

## DENY_ALL 与驱动支持

新增 `access/AuthorizedDocumentSet` 零值 DENY_ALL，授权 grant 只能产生显式文档目标。Constrain 校验 ownerTenant/KB、引擎预过滤已验证标记、非空 allow-set，再与客户端选择求交；不改变原调用参数切片、不将空集翻译成 legacy 无过滤。`CheckDerivedSources` 在源集合未知/空/有任一拒绝时阻止下游消费。

源码存在性：PostgreSQL、SQLite、ES7/8、OpenSearch、Qdrant、Milvus、Weaviate、Doris、TencentVectorDB 都能看到 KnowledgeIDs 的过滤构造。**这些驱动尚未通过本批文档 ACL 的实际预过滤认证**，支持标记不能由客户端参数或普通 metadata 设置。M3 要逐个实际启用驱动的 vector、keyword、FAQ、rerun/enrichment 模式验证，限制大 allow-set、空集、删除、源失权及缓存命中。unsupported/unknown=阻断或独立 KB/索引，不能最终过滤后声称安全。

当前接口测试是契约验证，不宣称已保护真实 Rerank/LLM。下一批必须用可观测 spy 在真实 pipeline 的候选、rerank body、prompt和日志边界证明敏感 canary 从未出现；模型出错/降级仍不能退回无过滤检索。

## 合成夹具

M2 现有 fixture：A/C 非目标空间成员，B 是目标空间 Viewer；owner 与 reviewer 独立；public-kb、未批准库、一份批准 text doc、模拟 restricted/mcp_blocked/deny、unknown/external渠道、空集、撤销/过期 Key、伪造主体、正文中资源链接。只含 `Synthetic... ERR_M2_42`，无真实员工或文档。

M3 扩展 fixture：同 KB 的 public/explicit-D/restricted-C/blocked/unknown 五文档；public Wiki 依赖 sensitive chunk；FAQ/graph/summary 依赖混合集；一对象多文档绑定；旧匿名签名和缓存命中；飞书/GitLab资源 source revision 变化；大 allow-set 超引擎限制。新增 D/E/F 并保持原 A/B/C。每份 fixture 固定 source/permission/index revision，收权后不重新签发或重建后才测试，避免掩盖旧资源旁路。

## 优先级与接口开发顺序

1. 固定 `ResolveKnowledgeRead` operation 语义和个人授权/企业公开授权 union：credential分别求交，doc deny覆盖两分支；restricted必须当前显式员工/已授权组，旧 scope仍有效。审批角色映射、enterprise directory、组语义先人工确认。
2. 建立可信持久化 provenance、内容版本和 doc grant；核查所有 content writer 是否递增可验证版本。M2 review绑定 doc/chunk revision metadata，不足以证明所有旧 writer 已遵守协议。任何不能验证的 writer/来源继续 HOLD。
3. 在 knowledge/chunk read service 和 batch/list/query统一接守卫，保持原 KB/ownership/Key检查；加 Python 旧 REST 对照负例。内部仅用于识别父关系的 lookup不能泄露正文。
4. 改造 candidate前filter、driver模式认证、FAQ/neighbor/parent/graph扩展；接真实 Rerank/LLM spy并强制前重验；对 unsupported引擎关闭混合库能力。
5. Wiki/摘要/问题/FAQ/图谱/cache来源闭包；权限收紧同步deny，再异步删索引/缓存和重建。旧无provenance正文不可继续提供。
6. 文件broker、当前binding、download原文件权限；存量匿名链接/缓存处置，执行 15 的 GET/HEAD/Range/304/跨用户历史引用矩阵。无法撤销则阻断激活。
7. 飞书/GitLab来源ACL可信映射、删除/失权/失败/重新导入；严禁未知失败回退公开，保留最严规则和同步身份。
8. 只有旧 REST/Python 与新组件共同通过 C/D/F全部负例、候选前安全及资源撤销门禁，才考虑M4个人Go入口、真实跨空间检索和M5 UI。默认八工具不意味着 Wiki已合格。

## 旧 Python REST 绕过验证计划

隔离同一数据集、同一有效/无效凭据分别调用工具与其后端 REST；对31注册工具做可达性/capability断言，并对详情/batch/chunks/revisions/hybrid/wiki/FAQ/graph/images/preview/download/export/raw/KB/message proxy/artifact/presigned/short-link补直接 HTTP请求。客户端 denyTools 不作为通过证据。含 full_access 的 Tenant Key也不能绕过显式 deny/mcp_blocked规定；个人Key不得拿系统管理员上下文。

记录候选 SHA+未提交bundle摘要、DB版本、身份/Key范围、HTTP/MCP状态、下游spy计数及原始退出码。正常调用成功不足以证明拒绝链路；被拒资源标题/片段/metadata和日志均要检查。实际旧REST全链路尚 NOT_TESTED，不能宣布敏感权限治理完成。

安全回滚：关闭公共入口、保留doc拒绝、停止新签匿名URL和新增公开审批，撤销受控Grant；不得回退到无ACL后端并继续让旧Python访问敏感库。不自动commit/push/merge/release/部署。
