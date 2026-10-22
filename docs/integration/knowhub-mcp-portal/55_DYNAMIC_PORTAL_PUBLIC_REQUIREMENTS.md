# 第七批最终产品规则：动态 Portal 公共知识

本文件取代 46～53 中关于历史 KB 逐一首次 approved、员工实时在职 Provider 的要求。旧报告和原始测试结果作为历史证据保留，不代表现行产品规则。生产功能仍默认关闭；本次没有公开生产历史知识。

## 范围来源

统一范围为：**有效且 Published 的 Portal 空间中的全部有效 document KB，扣除显式公共限制，再与当前个人 Key 和用户独立拥有读取权限的私有 KB 取并集**。不硬编码 17 个空间或 48 个 KB，不建立逐 KB 审批清单，不回填 approved，不要求团队 MCP。未来空间发布、已发布空间新增 KB、普通文档完成索引后均按最新数据库状态发现。

`repository.ListPortalMCPCandidates` 动态枚举；`access.PortalMCPResolver.Resolve` 每次重新校验主体、Key、空间、KB 和文档快照；`EnterprisePublicReader` 的 `PortalPublishedDefault` 模式直接使用 Published。M2 原有批准模式保留给旧组件，APP 个人 MCP 装配使用新模式。`PolicyReference` 是服务端模式启用的业务依据配置，不是逐 KB 批准记录。

已保存的只读生产元数据中，理论公共范围为 17 个已发布有效空间、48 个 document KB。原“0 个可验证安全公开 KB”是旧审批模型的结论，不能作为新业务范围。生产未安装本批授权组件、个人 MCP 未启用，实际新 MCP 可检索范围仍为 0。显式禁止例外沿用管理权限设置，不增加逐 KB 检查或批准门禁，也不据此宣称保护未标记混合敏感内容。

## 读取与撤销

Published、空间有效性、KB 归属/删除和显式公共禁止同时参与请求授权与真实 SQL 候选过滤。关键词、向量、Hybrid、grep 的候选不能先进入模型再后过滤。模型消费和响应输出边界重新校验授权。旧 Chunk 引用必须通过当前文档与 KB 所属关系校验。

公共禁止不撤掉用户独立私有权限；Portal 取消发布也不影响独立私有授权。Key 被撤销、失效或用户停用后拒绝访问。普通旧 Key 不自动增加 `enterprise_retrieve`，机器凭据不能变成个人主体。

KB 例外写入已有 `kb_publication_policies`，以 `mcp_blocked/revoked` 表示禁止；恢复为 `pending_review/inherit` 即恢复 Published 默认范围，**不要求 approved**。沿用事务审计和 revision/CAS。旧文档显式拒绝保守阻断整个 KB，但不声称保护未标记混合敏感文件。超过当前 2048 文档快照预算保持拒绝并报告 HOLD，不截断为部分授权。

## 页面与部署

Portal 发布管理就是公共范围入口。新的 `PortalPublicKnowledge.vue` 展示动态范围及 KB 禁止例外；原逐 KB 审批组件不再用于当前页面。发布操作不增加“全员可检索”提示、确认弹窗或免责声明。Owner 或显式配置的 Portal SystemAdmin 管理例外，API Key 不拥有发布管理权。例外管理不构成额外 MCP 发布审核。

员工进入“我的 MCP”，显式创建带 `enterprise_retrieve` 的个人 Key，复制统一连接配置。真实主体来自既有 SID/OA 与 KnowHub 账号、服务端 OwnerUserID；本地账号有效性不是实时在职认证。不新增员工目录或 OA/HR 查询。

实际路径为 `/api/v1/mcp/personal`，使用现有 APP 和 Nginx，APP 后端仍为 8080，无需另建 Go 服务。内网及隔离只读入口共用同一 Key 和范围；五个只读工具在服务端注册与限制。隔离 Nginx 只转发精确 MCP 路径，允许必要 POST，其余路径拒绝。生产与隔离网络本批均未实际开通。

## 验收与回滚

新证据放在 `evidence/batch7/published/`，独立候选逐文件 SHA256；旧 `continued/` 结果不自动继承。动态变更、真实驱动、模型 spy、飞书导入边界、真实客户端、同 Key 双入口范围一致性分别记录，未完成项保持 NOT_TESTED/HOLD。

关闭 `personal_mcp.enabled` 可关闭新入口。未改动默认配置文件；缺配置或缺 PolicyReference 时拒绝启用。源码回滚应按候选清单逐文件恢复，保留前三批迁移和证据，禁止整体 reset/clean。无生产数据回滚需求，因为没有生产写入。
