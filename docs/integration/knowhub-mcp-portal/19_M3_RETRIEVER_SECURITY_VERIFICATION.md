# M3 真实检索安全验证

整体状态：**部分驱动隔离验证；全应用/全驱动 HOLD**。测试候选、命令、退出码见 [22](22_M3_TEST_RESULTS.md)。

## 已实现路径

`DocumentReader.Authorize(search)` → opaque `DocumentReadGrant` → `WithDocumentSearch` → `GuardDocumentRetrieval` → 现有 SQLite Retrieve → FTS5 / sqlite-vec → `SearchDocuments` 模型回调边界 → 再验证 → 输出。

没有新建检索引擎。`KeywordsVectorHybridRetrieveEngineService.Retrieve` 接入统一守卫，SQLite 仓储入口也独立执行守卫。未携带新受控上下文的旧调用保持原状，因此仍不属于新安全面；隔离屏障不认证这些路由。不能把这层可选上下文当成全应用已经接通。

边界：单 Tenant、单 KB、最多 256 文档；零授权集合拒绝，不调用驱动；用户过滤只能与允许集合取交集；错执行 Tenant、跨 KB、超限、未知模式/驱动、TopK 越界直接拒绝，不回退全库。多 KB 组合在新受控路径中拒绝，待精确分组认证后再开放。

SQLite SQL 在候选阶段加入 ID allow-set，并 JOIN 权威 knowledges/chunks，核实真实 Tenant、KB/Chunk 关系、启停/删除/完成状态、text 类型、直接 Chunk source_type 和当前正文一致性。旧索引中相同 ID 的旧正文、摘要、生成问题、图片等不会作为当前 Chunk 正文放行。此处还需后台索引同步以保证召回；不等待异步索引来决定撤权。

向量查询先物化候选，再在不含 JOIN 的 KNN 子查询中执行 rowid IN，之后才取索引行。实际发现旧头文件构建会使 sqlite-vec 缺少 IN 能力，不能仅看 runtime sqlite_version。受控向量读取还检查 EXPLAIN QUERY PLAN 的 `[___` rowid-IN 计划标记；未知版本/计划拒绝。

依赖源码 `sqlite-vec-go-bindings@v0.1.6/cgo/sqlite-vec.c` 的 COMPILER_SUPPORTS_VTAB_IN / VEC0_IDXSTR_KIND_KNN_ROWID_IN 与实际双构建测试共同构成证据。[维护者关于 JOIN/IN 过滤限制的说明](https://github.com/asg017/sqlite-vec/issues/196)和[计划格式](https://github.com/asg017/sqlite-vec/blob/main/ARCHITECTURE.md)仅解释原因，不代替测试。匹配 Go SQLite 依赖的构建头文件 SHA256 随证据保存；旧头文件构建有单独的 HOLD 断言。

## 驱动和模型边界

| 路径 | 本批处理 |
| --- | --- |
| SQLite 向量、FTS5 关键词、同 KB 双路召回 | 实际驱动 + TopK=1 敏感最近邻 canary + Rerank/LLM 回调 spy |
| PostgreSQL 检索 | 新受控路径拒绝；PG 迁移/ACL PASS 不等于 pgvector/ParadeDB 过滤 PASS |
| Elasticsearch 7/8、OpenSearch、Milvus、Qdrant、Weaviate、TencentVectorDB、Doris、其他驱动 | 在新统一 wrapper/受控 SearchDocuments 中拒绝调用；原驱动回归不是 ACL 认证 |
| grep/regex / 直接 agent SearchKnowledge / Go MCP grep | 未认证，隔离屏障 HOLD；未在所有候选读取入口接通 |
| 跨 KB / 多模型 / 多索引组 | 受控路径拒绝，不能隐式 pivot primary KB |
| 原 HybridSearchWithRerank/chat_pipeline/agent 模型链 | 尚未逐个接通；不得以本批回调 spy 宣称现有全链安全 |

`SearchDocuments` 在检索前、每一个模型回调前和最终输出前重新验证。模型调用期间撤权会阻止后续模型和输出；已经在撤权提交之前合法发送给模型的字节无法撤回。没有权限验证到网络发送之间“绝对零窗口”的承诺；真实调用应采用取消传播和清晰的授权线性化边界，不允许通过索引延迟或缓存复用放行新请求。

合成敏感 canary/旧索引 canary 不能出现在无权回调输入；断言还要求授权结果非空，避免用空结果伪装安全。SQL/检索失败直接返回错误，不改成成功空集合。原始失败也保留。

## 仍需验证

真实完整 Hybrid/grep/FAQ/图谱链路的候选前过滤；各驱动/维度/模型不同绑定、索引异步竞态；生产规模性能、Recall@10/MRR/nDCG、P50/P95；外部模型实际客户端的 HTTP spy；跨用户高并发/缓存/重试与断线。当前没有外部模型收到测试正文。对应 D01/D06/S01/S03/S04 为组件子集，S02/S05 未完成，不批准混合敏感 KB 试点通过。

生产影响无；回滚不启用新上下文/屏障。SQLite 查询变更影响原驱动的过滤执行计划，必须保留原向量/FTS/更新回归结果；不能只回退计划能力检查却保留开放配置。
