# 第四批：检索驱动与模型输入验证边界

生产相关 PostgreSQL/ParadeDB 文档 ACL 检索仍为 HOLD。本批实际 PostgreSQL 验证针对正式迁移、CAS、epoch 与故障恢复，**不是 PostgreSQL/ParadeDB 检索安全认证**。

## 已有源码与新基线回归

`access/document_filter.go` 定义零值 DENY_ALL 和 allow-set 收窄；`access/document_search.go::GuardDocumentRetrieval` 检查受保护上下文、资源 tenant/KB/主体/操作、上限与实时 Grant。`service/retriever/keywords_vector_hybrid_indexer.go` 和 `repository/retriever/sqlite/repository.go` 已保留第三批接入。无上下文的既有 legacy 调用尚未升级成完整业务文档 ACL。

`document_retrieval_integration_test.go` 在实际 SQLite FTS、sqlite-vec 和 Hybrid 上重跑候选前过滤：空集拒绝、不能扩张 allow-set、错误 tenant/KB 拒绝、最近的未授权 canary 不挤占 TopK、陈旧 index body 不进入输出、模型 callback 前重新验证及撤权阻断。`document_old_sqlite_test.go` 用不支持 rowid-IN 计划的构建确认 HOLD，不通过版本号臆测能力。

这些是实际 SQLite 驱动与合成模型 callback spy；没有调用外部真实模型，没有发送生产内容。它们不等价于 RAG/Agent 的实际模型 HTTP 网络 spy。

## 当前驱动能力

| 驱动/路径 | 文档保护状态 |
|---|---|
| SQLite FTS / sqlite-vec / Hybrid | 有受保护上下文的合成实际驱动已实现，最新回归见 30 |
| 旧 SQLite header/不支持 rowid-IN | 受保护向量请求拒绝，不能回退全库 |
| PostgreSQL/ParadeDB | 受保护 Grant 路径拒绝不认证的驱动；实际关键词/向量/Hybrid ACL 未接入，HOLD |
| grep/正则、多 KB | 完整业务候选前授权未接入，HOLD |
| 其他向量/关键词驱动 | 普通单元测试通过不代表 allow-set 安全通过；未认证能力保持拒绝/HOLD |
| RAG、Agent、引用补全及缓存 | 缺少完整实际业务 Grant 传递/网络 spy，HOLD |

`.env.example` 的默认检索配置为 postgres；此处仅静态核对候选，未重新连接生产获取运行配置。此前生产只读基线仍保留，不推定部署运行参数完全等于样例。

## 下一阶段验收

先核对 PG BM25/ParadeDB 和向量 SQL 的候选表、真实 tenant/KB/document 关联、索引内容版本，再将精确 allow-set 直接放入候选查询。参数分批不能导致超限 fail-open；SQL/权限查询失败必须拒绝。模型/索引命名空间也必须绑定到真实资源所有者。

执行真实关键词、向量、Hybrid、grep、多 KB，以及未授权标题/摘要/chunk canary 的外部 Rerank/LLM HTTP spy，模型请求前和输出前复验撤权。未通过这些检查，不能用最终结果后过滤或 SQLite PASS 替代生产检索验收。

生产无影响。当前迁移对照和 isolated SQLite 源码可撤去，公共能力仍默认关闭；已知业务面缺口不会因为 P0 通过而消失。
