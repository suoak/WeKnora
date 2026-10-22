> **Superseded policy:** First-approval and real-time employment requirements in this historical report have been replaced. See [55: final dynamic Portal policy](55_DYNAMIC_PORTAL_PUBLIC_REQUIREMENTS.md). Historical test passes do not validate the new candidate.

# 第七批：发布规则与企业身份

本批承接第六批 MVP，不重写 Go MCP、个人 Key 或检索引擎。生产入口仍由 `NewDisabledPersonalServer` 装配；本报告中的实现不构成生产开放批准。

## 规则及事实来源

Portal Published 是公共发现的必要条件，空间 active、KB 有效、普通分类、批准版本、员工身份及 Key 显式能力同时成立才产生受限公共读取许可。Portal Published 与 accessible 保持独立；公共 Grant 不变为目标空间成员、管理、写入或文件下载权限。个人私有授权独立核验，不因公共发布撤销而自动消失。

首次历史确认复用 `enterprise_space_policies`、`kb_publication_policies`、`knowledge_access_policies` 和原审计表。未知、混合、敏感及外部来源不自动公开。未来普通资料的继承依赖 KB 上已独立批准的 `inherit_new_ordinary` 和可信来源规则，不再为每个 KB 创建团队 MCP。缺少分类器时不继承。

## OA/SID 核查及源码

现有 `ExternalIdentity` 是 SID 绑定；`SourceIDAccountOwnership` 是带策略、独立证据引用、有效期和状态的账号归属证明。它们都不是足以证明“当前在职且不是机器账号”的事实。现有 `SourceIDEmployeeAdmissionResolver` 已明确要求独立员工准入契约，源码中没有可直接替代它的正式适配器。

新增 `internal/application/repository/enterprise_employee.go` 的 `SourceIDEmployeeVerifier.IsEnterpriseEmployee`：

- 只查服务端持久化用户及唯一 SID issuer/subject/employee 绑定，不接受客户端身份 Header、邮箱后缀或工号自报。
- 用户 active、未删除；账号归属 active、策略匹配、证据完整、未过期且满足 freshness。
- 每次调用现有准入适配器，要求 `ALLOW`、批准契约、有效时间及可重新核验。请求阶段为 `enterprise_mcp_recheck`，不会伪造 `AuthorizationVerified` 或 OIDC claims。
- 零值、缺表、缺适配器或查询故障拒绝。SystemAdmin 与普通用户适用同一员工资格要求；没有平台管理员旁路。

真实个人 Key 的撤销、过期、owner、workspace membership 仍由既有 Auth、个人 Key service 和 Grant 再校验。此 Provider 不创建用户、角色或凭据，也不使历史 Key 获得 `enterprise_retrieve`。

测试 `TestSourceIDM7LiveEmployeeProof` 使用真实 SQL 绑定记录与受控准入适配器验证正常、停用、归属失效、未绑定、准入 DENY/UNKNOWN、缺适配器及管理员无额外资格。**正式 OA/SID 在线在职验证尚未通过，不能把此合成测试写成真实企业身份验收。**

## NEEDS_DECISION

| 事项 | 必需输入 | 未完成时的行为 |
| --- | --- | --- |
| 员工准入 | 正式 OA/SID 在职及机器账号判定契约、issuer/subject 绑定、时效、失权重查和不可用策略 | Provider 拒绝，生产 MCP 关闭 |
| 内容负责人 | 按业务责任授予的空间/KB 确认角色及授权来源 | 不以 Owner 身份推定有企业公开权 |
| 发布审核人 | 正式 Portal 发布管理流程中对企业内容公开的明确授权映射 | 批准接口不开放，管理 UI 仅清单 |
| 新普通来源 | 上传/同步后由谁证明 ordinary、来源及文档当前 epoch，撤销及重试语义 | 未接入上传/同步自动继承 |
| 用户级 deny | 对个人、公共及 Web 读取的适用语义 | 混合敏感 M3 保持 HOLD |

已通过异步问题请求上述身份和角色依据；不能用等待超时当作业务确认。上线须独立批准。

## 生产及回滚

未修改 SID/OA 登录、账号归属同步或生产 Python MCP。新增 verifier 不在生产 DI 中启用。回滚保持默认 disabled，移除本批新增 Provider 和调用装配即可；保留历批工作树、策略及审计证据，不回填 public、不轮换生产 Key。
