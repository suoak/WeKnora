# M1 跨 Portal 统一检索方案

第二批状态：基础检索 ALREADY_SUPPORTED；公共范围边界按 [ADR-001](13_M1_FINAL_DECISIONS.md) 定稿。M2 可产生带真实 owner TenantID、显式非空 KnowledgeIDs 的精确文档 SearchTargets；尚未挂到真实 Hybrid/Rerank/Go MCP。质量/P50/P95 仍 NOT_TESTED。

零值/空 allow-set 直接返回拒绝/空 targets，不允许消费方将其回退为全 KB。新增 `AuthorizedDocumentSet.Constrain` 只可缩小调用者选择，目标 tenant/KB 不匹配、引擎模式未验证支持预过滤、求交为空全部拒绝。模型前重验及衍生依赖检查接口已有合成契约测试，真实驱动与外部模型 spy 的全链路验证属于 M3，见 [16](16_M3_CALL_CHAINS_AND_EXECUTION_PLAN.md)。

## 动态目标解析

服务端 public 分支：有效企业员工 + 新能力 → published/active/approved spaces → 有效、非临时、经准入的 KB → 已批准文档过滤。个人分支：Key.TenantScopes → 当前有效 membership → selected/all KB 范围 → 既有组织共享等授权 → 文档显式规则。union 按真实资源 ID 去重，每项保留真实 owner tenant 和两种授权 provenance；不同名字不作为 ID，相同名字在选择时返回歧义并要求 ID。

不得调用 Portal 前端列表拿 aggregate 当 KB allowlist；不得维护固定 KB ID 清单。Portal 撤销发布/KB 收权/文档撤销后下一请求实时更新；缓存需按 policy revision 和完整身份键验证。动态新 KB 只有通过 04 中服务端准入，才自动继承空间批准策略。

## 复用点和扩展点

| 源码 | 已有行为 | 本任务扩展 |
| --- | --- | --- |
| mcpserver/scope.go:searchTargetsFor | 每 target 带 KB.TenantID | 附可信 doc filter、provenance/revision；严禁 scope 空集退化全 KB |
| agent/tools/search_knowledge.go:concurrentSearchByTargets | embedding model identity 分组、跨 KB 查询、失败/embedding fallback 记录 | 首先按安全目标过滤；验证模型 identity grouping 的维度/端点/config；外部调用前重验 |
| service/knowledgebase_search.go | shared KB preflight、模型维度校验、SearchParams、候选处理 | 共享 policy 解析，不仅 HTTP handler 检查；document filters 贯穿候选和 enrichment |
| knowledgebase_search_storegroup.go/fanout.go | 按 VectorStoreID + owner TenantID 分组，budget 与不可变参数 | 每个 group 显式带授权 grant；真实 owner 的模型/存储上下文，不能借切换增加权限 |
| knowledgebase_search_fusion.go | BM25 归一化、RRF、向量原 score、dedup | 不把不同模型 cosine 当可比较值；保留现有单模型语义 |
| shared reranking 与结果格式 | rerank、引用、关键词标识符模式、部分失败 | 加公共安全 rank 融合、来源 Tenant/KB/doc/chunk 字段及政策复查 |

## 排序与可追溯性

先在各兼容分组内使用现有 Hybrid、FAQ、keyword 逻辑；跨 embedding/config 分组采用 rank-based RRF（例如 k=60，参数待基准确定），而不是合并原始相似度。若使用统一 Rerank，仅对已授权文本重排；模型失败以安全的 rank fusion 降级，附降级状态。现有同在 [0,1] 的归一化分数不代表跨模型已校准。

去重按 ownerTenant/KB/doc/chunk ID；不同文档的相同文字不盲删，保留来源。每条引用包含准确的空间、KB、document、chunk 与受控读取链接；标题和 metadata 也在授权后投影。wiki-only/graph-only KB 显示具体支持能力，不能混入无法执行的 vector 查询或误称全部类型已检索。

结果契约增加 complete/partial/failed、attempted/succeeded/failed target counts、degraded_modes、授权范围版本。错误消息对调用者仅披露其已授权资源的必要信息；内部错误码可追踪，密钥/模型地址不泄露。全部失败不能返回“无相关内容”；单空间失败不能称搜索了全部企业知识。限流、deadline、embedding fallback 都明确记录，不为补齐结果而扩大 scope。

## 质量与容量验证计划

隔离库准备中文/英文同义资料、完整错误码、路径、函数名、同名 KB、不同 embedding 维度/端点、不同存储、FAQ/document/wiki-only，以及一个不可达空间。标签只用于测试集分组，不作为授权。业务标注 query→relevant doc，记录 Recall@10、MRR/nDCG、重复率、完整/部分失败率，按单空间/跨空间、冷热缓存测 P50/P95 和 fan-out 负载。

所有测量先使用合成资料；真实业务资料经批准且不向外部模型发送敏感数据。现阶段没有真实性能数字，不能根据源码声称中文提问英文资料一定能召回。具体门禁见 09；不新建 RAG 引擎或第二套搜索实现。
