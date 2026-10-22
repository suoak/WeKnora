> **Superseded policy:** First-approval and real-time employment requirements in this historical report have been replaced. See [55: final dynamic Portal policy](55_DYNAMIC_PORTAL_PUBLIC_REQUIREMENTS.md). Historical test passes do not validate the new candidate.

# 第七批续作：以可操作的 Portal 统一 MCP 为优先

## 本次有效范围

本报告按用户最新两次补充调整优先级：复用现有 KnowHub 账号和 `user_mcp`，不新增员工目录、实时在职 Provider 或 OA/HR 接口。本文取代 46～52 中把正式在职 Provider 作为隔离研发阻塞的表述；旧候选测试与原始 FAIL 仍保留，不改写历史结果。

生产入口保持关闭。历史 48 KB 没有自动批准。没有提交、推送、部署或生产数据写入；完整混合敏感 M3 仍 HOLD，未扩大本批文件级 ACL、Wiki/FAQ/Graph 治理。

开始续作前完整保存 6582 文件检查点：`E:/AIData/Codex/knowhub-b7-business-rules-checkpoint-20261021.tar.gz`，逐文件及归档哈希见 [检查点](evidence/batch7/business-rules-checkpoint.json)。新原始证据均放在 `evidence/batch7/continued/`，前次结果不覆盖。

## 账号与角色

`repository.KnowHubPersonalUserVerifier.IsEnterpriseEmployee` 复用已有的接口形状，但实际含义是 **KnowHub 个人账号校验，不是实时在职认证**：必须是服务端认证的 `web_user`、匹配的 caller/user_id、有效 `user_mcp` 上下文和当前 active、未删除 users 记录。

原 `PersonalMCPAuth`、LiveDocumentBaseVerifier、Grant 和策略快照继续检查 OwnerUserID、Key 状态/有效期/撤销、retrieve、显式 enterprise_retrieve 及原私人范围。旧 Key 不扩权；机器 Endpoint、Tenant/Platform Key、Embed/匿名主体不转成个人用户。RJGH/RJEMAIL、SID 绑定/JIT/现有会话生命周期由原登录体系负责，没有重建或增加新登录路径。

续作中按前一条指令起草、尚未启用的资格 Provider 改动已停止，草稿保存为证据后，仅将本轮两份相关修改恢复到检查点。既有前批 SourceID verifier 文件保持原样，不接到当前优先个人 MCP 流程，也不宣称它证明自然人或实时在职。

`service.PortalPolicyAuthority` 的隔离映射为：

- `propose`：目标空间 `tenant_members` 中当前 active、未删除的 Owner，且用户 active。
- `approve`：真实 JWT SystemAdmin **并且**数据库当前 SystemAdmin/active，且显式 PolicyAuthority 配置允许此映射。
- `revoke`：当前 Owner 或明确配置的审核人；允许旧客户端 revision 发起撤回。
- API Key/机器主体不能审批。配置零值、缺 ContractRef、缺身份均拒绝。提出者与审核人分离由原 M2 强制，审核前再次检查提出者仍是目标空间有效 Owner。

`ConfigurePortalPublication` 仅用于明确的隔离装配；生产 constructor 不调用启用配置。ResponsibleTeam/Contact、KB 创建人不作为授权事实。多个 Owner 时保存真正操作的 user_id。

## 管理员与用户操作流程

1. Owner 在“空间信息”的企业公共知识范围区域打开目标 KB，核对整库范围，确认普通内容和来源，填写依据，决定是否允许可信新增普通文档继承，再提交。
2. Portal 管理员在原系统 Portal 管理页打开相同 KB，查看实际提出者、状态、资源版本和历史，独立审核。未启用 PolicyAuthority 时只有查看界面。
3. 系统以同一事务执行空间、KB、全部文档的原 M2 CAS/审计；仅 proposal 不产生公共 Grant。提交和审核绑定文档 epoch、Chunk、集合及当前版本，不以时间戳代替完整版本。
4. 普通用户在原 MCP 接入页创建“统一个人 MCP（只读）”，显式检查企业公共检索能力，保留本人私人授权范围；复制一次性 Secret 和统一单入口配置，执行连接检查。
5. 客户端使用自己的一把 user_mcp Key，联合检索多个合格公共空间和本人的私人知识。无需加入目标空间、无需每空间配置团队 MCP。
6. Owner 可撤回公共 KB；Portal 取消发布也立即影响新请求；个人 Key 可通过原管理页停用/轮换。公共收回不代替对私人授权的独立判断。

新组件 `PortalKnowledgeReview.vue` 实际挂到 `TenantInfo.vue` 和 `SystemPortalSettings.vue`。提供当前范围、逐 KB 首次确认/审核/撤回、提出者和审核时间、最近 50 条 KB 审计记录。新 REST 复用现有 JWT/RBAC 与 `/system/admin/portal`，全部对 API Key 默认拒绝。

所有变更使用服务端身份；浏览器不能提交 ActorID、批准状态或任意资源归属。资源不在目标 KB、版本变化、Owner 失效、缺策略、未知/受限来源均拒绝。HTTP 409 必须重新取得版本，不能重放旧审批覆盖收权。

## 自动进入与后续继承

已发布、整库明确批准的普通 KB 在每次 Resolve 时自动进入统一 MCP，不维护员工×KB 的复制授权表。私有范围独立合并，显式指定 ID 只能收窄。

新增普通文档复用 `EnterpriseAccessRepository.InheritNewOrdinary`，必须有可信服务端分类、有效 KB/空间批准、明确 inheritance 委托、当前 epoch 和准确旧集合重建；事务保存新文档策略、KB revision 和系统继承审计。外部/未知/混合/敏感、已有 deny、批量并发集合不明确时 HOLD。正常上传/同步的可信分类装配不能用客户端“普通”字段代替；没有可信分类就保持待确认，不能据此宣称全部写入来源已经自动继承。

## 大 KB、模型与只读统一服务

当前公共整库 SQL 路径安全支持 128、257、512、864、1001 合成文档，原私有显式 allow-set 上限仍为 256；不将空集合解释成全库。上限 2048 的快照预算超出时 HOLD，不通过分批截断扩大授权。真实 PostgreSQL/ParadeDB、精确向量、关键词、Hybrid、多 KB、grep、canary/外部输入 spy 均在续作候选回归。

生产只读已核实 48 KB 的配置维度均为 1024。续作人工客户端夹具也使用原 ModelService/ModelRepository、Embedding HTTP 客户端、store registry 与 1024 维索引；上游是明确标注的合成服务。真实模型语义质量、中文问英文和生产网络时延没有被这些合成结果验证。

实际路由和 80/443/APP:8080 复用说明、隔离入口限制见 [54](54_UNIFIED_MCP_NETWORK_AND_READONLY.md)。没有增加 Go MCP 生产服务、监听端口、Key 类型或知识库类型。

## 续作测试与验收

本次核心完整 Linux 候选：`f455a84dfad9098ba39631908c3fbd5153e98ea7cc07d757a1f146c57b45754d`。人工客户端候选：`de9e5bb80b8deab2563fd6f2adca253bdb17516942834e21fd5c66b96996d680`。候选由未提交源文件逐文件 SHA256 标识，不能用原 Git HEAD 代替。

最终测试、真实页面和客户端结果、完整命令/退出码与当前源码一致性见 [续作结果报告](56_DYNAMIC_PORTAL_ACCEPTANCE.md)。旧 f8d390 的 PASS 不能自动继承到新代码。本批中间编译失败、Windows WinError 1314、未完成客户端项独立保留。

隔离研发不再以独立在职接口为前提。生产启用仍须独立确认账号资格适用范围、正式 Owner/SystemAdmin 映射、首批知识安全范围、网络通道、实际模型和性能、回滚与发布批准；这是上线门禁，不是新增 HR 研发任务。
