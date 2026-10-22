# ADR-001：第二批 M1 权限决策（2026-10-20）

设计已定稿的安全不变量；企业角色和生产发布范围仍需人工批准。本 ADR 取代 04–08 中冲突的建议，保留第一批文档和原始证据。M0 不重做。

1. Portal published 只是目录条件。公共读必须同时有有效企业员工、有效 user_mcp 凭据的显式 retrieve + enterprise_retrieve、已批准空间/KB/文档及有效资源。缺策略行等价 pending_review，绝不 public。旧 Key、tenant/platform/mcp_ Token 不自动扩权。
2. M2 仅整洁 document KB 的已批准正文试点；KB 内任一有效文档未知、待审、受限、禁止 MCP、完全禁止或外部来源均 HOLD 整库。没有文件 ACL 前不把混合库作为通过案例。M2 不实现继承创建 hook；将来的自动继承需空间及 KB 双批准、双显式开关、可信普通来源准入和审核版本。缺行继续拒绝。
3. 公共 Grant 是独立、精确、可重新验证的 parsed-read/search 许可，不是空间成员，不是 KB editor/admin，不是原文件、图片、Wiki、Agent 或导出许可。M2 不给现有全局 KB 检查加旁路、不注册生产 HTTP/MCP 路由。只在隔离调用组件并复用现有 ChunkRepository 读取已批准的 text chunk。
4. 审批：内容负责人 propose → 与提出者不同的发布审核人 approve。二者身份均从可信 context 取；范围为持久化 space/KB/Knowledge，记录旧/新策略、版本、actor、时间、原因、scope。角色能力通过必需注入的 PolicyAuthority 逐次验证，nil 默认拒绝；生产未绑定角色。
5. 角色冲突：Portal 发布目前偏向 system_admin，空间 owner/KB creator 不等于企业发布审核人。NEEDS_DECISION：企业审核人由谁任命、哪些内容负责人有提出权限、企业员工资格的受信来源。不得把现有 system_admin/space admin 自动映射为新能力。隔离测试使用明确合成 authority，不写成员或角色表。
6. propose/approve 使用 revision/CAS；revoke 不受客户端旧 revision 限制，事务读取当前版本后收紧并递增。旧审批不能覆盖 revoked；必须重新 propose、独立 review。所有成功策略变更与现有 audit_logs 同事务，audit 失败则整个变更回滚。回收当前审批后，后续请求重新读 DB，不靠缓存过期。
7. M2 最小增加三张策略表：enterprise_space_policies、kb_publication_policies、knowledge_access_policies，审核历史复用 audit_logs。第一批五表建议中的额外 outbox/显式 grant 不在 M2 建立，M3 再评审；避免两套权限事实。缺省 state=pending_review、policy=pending_review、content_class=unknown、source_acl_state=unknown、inherit_new_ordinary=false。
8. 文档规则分开：restricted=仅明确获准员工可以读，所有 REST/文件/衍生接口均执行；mcp_blocked=阻止全部 API Key/MCP 内容路径，但不替代 Web 文档 ACL；deny=所有普通读取拒绝（任何入口、管理员也无隐式绕过）；pending_review=公共和机器路径拒绝，既有合法内部访问如何保留由 M3 明确策略，不扩大权限。显式拒绝覆盖公共与个人许可；个人许可不能突破 Key 的原私有范围。
9. M2 三张表可表达拒绝，但尚未把拒绝接到所有旧 API，不宣称敏感收权完成。旧 Python REST、文件/缓存/Wiki/LLM 的完整 M3 守卫验收前，不激活混合库和生产策略。撤销企业公开只撤销公共分支，既有私人授权不被错误删除。
10. 匿名签名/HMAC URL、一天公共缓存、对象直链和历史引用必须按 15 专项方案收口。无法证明既有 bearer URL/缓存可即时失效的资源保持 HOLD，短 TTL 不能替代撤销。已经下载到客户端的字节不能追回。
11. 默认关闭：组件零值/Options.Enabled=false 拒绝；没有路由、DI、环境默认开启或全量回填。发布审批权限接口、employee verifier 缺失时拒绝。变更不依赖 SID 验收、不改 OA/飞书/Portal 业务。
12. 迁移使用未占用 003034（高于已确认生产 3033），仅新增表/索引，不改 3033 及用户/Key/Portal。隔离测试覆盖 schema 3033 形状和真实迁移链可用性；两者结果分开。down 仅适用于未激活隔离环境；生效后的安全回滚是关闭公共入口并保留拒绝守卫。

验收界限：M2 合成组件 PASS 不能解释为 Go MCP 个人入口已实现、实际向量检索已验证或生产 ACL 已覆盖。具体执行结果与剩余项由 14/17 记录。
