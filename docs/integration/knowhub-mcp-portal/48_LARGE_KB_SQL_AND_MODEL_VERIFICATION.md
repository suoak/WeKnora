> **Historical batch-7 checkpoint:** See [55: final dynamic Portal policy](55_DYNAMIC_PORTAL_PUBLIC_REQUIREMENTS.md) and [56: current acceptance](56_DYNAMIC_PORTAL_ACCEPTANCE.md). Old approval requirements and test passes do not define the final product or validate the new candidate.

# 第七批：大 KB、真实驱动和模型装配

## 实现

原 M3 `MaxDocumentAllowSet=256` 未修改。私有差异化文档集合继续受原约束，超限保持 HOLD。新增公共模式仅接纳**整库均已审核且当前版本匹配**的 KB；不存在空集合退回全库的普通驱动调用。

`access.PortalMCPResolver` 将整个公共 KB 的审核快照作为 opaque lease，不再为 N 篇文档分别重复加载 N 次整库快照。`PortalKBTarget.Revalidate` 仍核验员工、实时 Key、Portal、完整文档集合/版本，并分别调用已有 `LiveDocumentBaseVerifier.CheckDocumentReadBase` 核验 read/search；缺少 base verifier 时拒绝。未批准和混合 KB 不进入该模式。

`repository.populateDocumentVersions` 在同一个 repeatable-read 快照中批量加载策略、epochs、Chunk 修订元数据，生成与旧 `resourceVersion` **完全同格式**的 SHA256。保留原批准记录；缺 epoch/trigger 或超出 2048 篇整库快照资源预算即 HOLD。2048 是新的整库快照预算，不是把原 SQL allow-set 常量调大。

`postgres.portalCanonicalPredicate` 在实际 BM25/向量 SQL 的 WHERE 中，绑定 owner Tenant、KB、Document、Chunk、有效正文、源类型、启用和删除状态；公共模式还约束当前 document 普通策略及有效 Published 空间。原索引中的错 owner、过期正文和衍生 Chunk 不能作为候选泄漏给外部模型。指定 ID 只收窄，空授权、未知驱动和非法参数仍拒绝。

公共向量 SQL 使用 `WITH authorized AS MATERIALIZED` 在授权关系内做精确距离排序及单次 LIMIT，不分批、不用只扫前几批模拟全局 Top-K。旧非 Portal HNSW 查询不变。该选择以召回可解释性换取扫描成本，**不声称精确查询使用 HNSW 加速**；真实执行计划随测试保存。

Hybrid 复用现有驱动，每个 KB/模型/模式取候选并按 RRF 合并。每模式候选仍为 100，输出最多 30；这是有限候选窗口，**不是已证明无限候选上的精确全局 RRF**。grep 仍为关键词候选内 RE2 匹配，完整正则扫描召回未实现。本批不能以文档集合完整等同自然语言召回率 100%。

## 模型及存储注册

`service.BindPortalMCPAdapters` 接入现有 ModelService、RetrieveEngineRegistry 和真实 KB store 绑定：

- 重载 KB 真实 owner 和 model ID，目标归属/绑定变化拒绝。
- DB store 先验证 `StoreOwnedBy`，再 `GetOrLoadByStoreID`；环境 store 读取目标 tenant 的 effective engines。
- 仅 PostgreSQL/ParadeDB 路径获本批许可，不自动降级到未知驱动或任意 store。
- 使用目标 tenant 的 `GetModelByID` 与 `GetEmbeddingModelForTenant`；维度必须与输出严格匹配且数值有限，不串用另一个空间模型。

生产只读证据显示 48 个 KB 配置维度均为 **1024**，已存在的 HNSW 维度为 1024、798、3584。本批隔离规模测试实际使用 ModelRepository → ModelService → 原 Embedding HTTP 客户端 → 原 registry/hybrid engine → PostgreSQL/ParadeDB，维度与生产配置一致。

**HTTP Embedding 上游仍是受控合成服务，不是生产模型调用；没有测量真实模型网络时延或中文问英文知识质量。生产模型语义质量、Rerank 正式选型/排序、外部服务 SLA 和生产多 Chunk 分布继续 NOT_TESTED/HOLD。** 原有真实模型客户端 Rerank/LLM spy、撤权和 canary 用例在新候选上回归，不代表已向生产发送文档。

## 验证和性能解读

`TestPortalM7RealPostgresScale` 使用正式 0→3035 迁移，规模 128、257、512、864、1001，每篇一个合成 Chunk。测试完整授权文档集合、集合尾部召回、keyword/vector/Hybrid/grep、1024 维、真实注册装配、无授权收窄、取消、策略 deny 和并发。新增查询计划、跨空间合并及非相同向量断言的最终候选结果见 [51](51_SEVENTH_BATCH_TEST_RESULTS.md)，不得继承早期候选 PASS。

每规模/模式 6 次样本，P50 使用排序后的中位样本，P95 为 nearest-rank 第 6 个样本。小样本与 race 插桩会影响绝对时延；race 数据标记为正确性回归观测，不当作生产延迟 SLO。另列普通执行、三 KB/两 owner 范围与单 KB 基线。解析成本含当前权限快照重建。

超出 2048、未知驱动、权限查询失败直接 HOLD/fail-closed；生产四个大 KB 仍需以批准后的等价多 Chunk 数据、真实模型和并发负载独立验收，不能仅因 1001 篇单 Chunk 合成测试通过而自动开放。

## 回滚

保持生产入口 disabled，撤回本批 opaque 公共关系消费及 batch 版本读取即可回到第六批 256 文档试点；无需迁移或重建生产索引，不删除旧索引和批准审计。
