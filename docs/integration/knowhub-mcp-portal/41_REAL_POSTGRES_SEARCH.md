# 第六批真实 PostgreSQL / ParadeDB 检索

## IMPLEMENTED

`postgres/repository.go::KeywordsRetrieve / VectorRetrieve` 在实际驱动入口消费 opaque Portal 检索范围，绑定真实 owner Tenant、KB 和文档集合；未知旧上下文不自动取得新权限。`portal_scope.go::portalCanonicalPredicate` 在实际候选 SQL WHERE 中，BM25/vector LIMIT 之前关联 canonical Chunk/Knowledge，核对 tenant、KB、document、source_type、删除/启用、text 类型及正文相等。

正文相等阻断索引旧内容、错 owner 记录、生成摘要/问题行；授权资源 epoch 复验负责拒绝审批之后的正文变化。不是先召回未授权内容送模型、再过滤最终结果。普通未启用原驱动路径保持原查询语义；其他驱动不被个人 Portal 服务自动接入。

`PortalMCPKnowledge.Search` 对每个已授权目标调用现有 PostgreSQL driver 的关键词/向量检索，按模式 rank 做 RRF 融合及精确来源去重，不跨不同 Embedding/KB 比较原始余弦与 BM25 分数。每个 KB 的 EmbeddingModelID 与真实 owner context 单独传给模型 adapter。失败目标计数为 failed_kbs，返回 complete/partial/failed，不能称已检索所有企业知识。

grep 使用真实关键词候选及 RE2 匹配，不承诺任意正则的全量扫描/召回；候选上限可能影响召回，权限边界仍先于候选。多个索引版本在同 KB 的语义兼容/性能调优不等于已通过生产质量评测。

## 实测设计

`router/portal_mcp_postgres_test.go` 使用隔离 PostgreSQL 17.9、ParadeDB pg_search 0.22.2、pgvector 0.8.1，正式迁移器空库 0→3035。继承原 RC3 迁移链，没有组合临时迁移目录、Force 或重编号。只通过数据盘独立 Unix socket 连接，网络 none，不发布端口。

合成数据包含两个 Tenant 的公共 KB、员工独立私人 KB、混合/未知 KB、敏感 canary、故意错 owner 的索引行及 stale body 索引行。测试实际执行 keyword / semantic / hybrid / grep，验证来源、联合数量、去重及精确 allow-set；另执行直接 driver 调用、越权 KB/文档、上限、空范围零 engine 调用、部分失败与撤权边界。正式结果见 [44](44_SIXTH_BATCH_TEST_RESULTS.md)。

生产实际后端索引版本、规模下 P95、召回率、全部驱动矩阵、全库 race、全部异步重试不在本次已验证结论内。不支持安全过滤的驱动与混合 KB 公共路径继续 HOLD。

向量用例采用 **3/4 维合成向量**，实际执行 pgvector SQL，并非模拟 driver；没有验证生产 798/3584 等维度下 HNSW 执行计划、选择性过滤召回和规模性能。不能将该用例写成生产 HNSW 索引验收。真实生产模型/维度/索引绑定和大 KB 验证仍是启用前门禁。
