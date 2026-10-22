# 第五批真实检索驱动状态

**HOLD / NOT_TESTED：本子批没有完成生产 PostgreSQL/ParadeDB 关键词、向量、Hybrid、grep 或多 KB 实际候选链接入验收。** 不是检索 PASS，不能开放混合敏感 KB。

已实现的是 Knowledge 列表的 SQL 预过滤：`repository/knowledge.go::ListPagedKnowledgeByKnowledgeBaseID` 在 COUNT 与分页之前使用 opaque scope 限定实际 owner Tenant/KB/document；空集合显式 `1=0`，scope 查询失败、主体变化、撤权、上限超出均拒绝。它是目录列表，不是向量或关键词召回验证。

`knowledgebase_search.go::HybridSearch`、`knowledgebase_search_rerank.go::HybridSearchWithRerank`、`knowledgebase.go::GetQueryEmbedding` 在试点启用且尚无已认证候选通路时拒绝；HTTP 层对应路径也 HOLD。没有用最终结果后过滤宣称安全；没有把第三/四批组件、SQLite adapter 测试自动继承为当前生产后端 PASS。

## 后续实现和验收顺序

1. 固定生产实际 ParadeDB/pgvector 版本与维度、索引定义，隔离数据盘 PostgreSQL，复用正式迁移器与合成资源；不访问生产内容。
2. 在现有 registry/driver 接口中绑定 tenant、KB、精确 document IDs、DENY_ALL；授权 ID 只可取交集，不能 union 扩大；审查 keyword / vector / hybrid / grep / 多模型多 KB 全部入参和最终 SQL。
3. 不支持相同语义的驱动、超限、策略查询失败、权限上下文缺失直接拒绝；不得回退无过滤查询。
4. 实际执行关键词、向量、Hybrid、grep、多 KB；抓取候选进入外部模型前的集合，并用真实 HTTP Rerank/LLM spy 验证敏感 canary 不曾进入网络输入。
5. 授权快照绑定当前数据版本；缓存命中、重试、输出/模型调用前再次复验；撤权优先于索引异步更新。补充并发撤权与外部调用阶段竞态。
6. 最后接 Agent/RAG/流式与引用；完整通过前所有相关路径保持 HOLD。

权限事实继续使用现有 access/DocumentReader 和授权版本；不另建检索引擎、不重复 Go MCP 或个人 Key。更改过滤合约后需重新执行现有驱动套件，不能靠聚焦测试覆盖完整套件失败。性能与召回指标本子批 **NOT_TESTED**。
