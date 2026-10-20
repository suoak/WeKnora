# 普通员工测试账号与权限夹具准备方案

> 2026-10-16 后续更新：用户已另行批准指定邮箱写入；限定单账号CLI已实现、合成验收并实际准备1个viewer，未开放企业门禁。下文为准备前dry-run快照，当前结果见[实现与实际准备报告](SOURCEID_SINGLE_ACCOUNT_PREPARER_IMPLEMENTATION_REPORT.md)。

日期：2026-10-16。状态：**只读 dry-run 完成；真实写入未批准；写入准备器尚未实现**。

## 1. 现场 dry-run 结果

北京时间 **00:13:31**，仅检查打包机172.30.232.42。APP431226c3b、Frontend1ba24ef5f、DB3027 clean、现有企业门禁及容器均未改变。证据：[只读 dry-run 收据](evidence/ordinary-account-preparation-dry-run-20261016.json)。

| 条件 | 实际结果 |
| --- | --- |
| 已有普通账号 | 0；用户1、合成owner1，不能复用为普通账号 |
| contributor/viewer | 各0 |
| 活跃知识库 | 0（历史总数9，不能作为有效测试内容） |
| 注册模式 | invite_only，不能通过邀请注册新账号 |
| 系统管理员 | 0；不提升owner权限以使用管理员创建接口 |
| 员工邮箱输入/匹配 | NOT_SUPPLIED / NOT_EXECUTED；未接收真实邮箱，不能声称实际邮箱匹配已完成 |
| 身份/会话写入 | 未执行；现有身份、审核、企业会话计数均0 |

结论：**现有公开流程不能在当前全部限制下直接创建该普通用户。** 这不是要求重做OA登录，而是限定测试数据准备的缺口。

## 2. 已有业务流程审查

| 流程与源码 | 能力与限制 | 本次选择 |
| --- | --- | --- |
| TenantMemberHandler.AddMember，internal/handler/tenant_member.go:190 | owner可添加已注册用户为contributor/viewer；GetUserByEmail找不到即拒绝，不创建用户 | 如果后续已有合格普通用户，直接复用；当前没有 |
| AuthHandler.RegisterByInvite，internal/handler/auth_register_by_invite.go:125 | 使用TenantProvisioningTenantless加入已有空间；但只允许self_serve/invite_register，当前invite_only拒绝 | 不改全局策略，不调用 |
| SystemHandler创建账号，internal/handler/system.go:1645 | 需系统管理员；provisioning取全局策略，不是每请求强制tenantless | 不提升权限，不调用 |
| userService.Register，internal/application/service/user.go:140 | 显式tenantless不创建个人空间；默认create_personal会建立owner | 仅作为获批单账号准备器可复用的业务能力 |

原有部分错误日志会带邮箱或数据库错误；准备器不得简单将完整HTTP错误、SQL错误或现有日志管线转存。真实写入前须完成日志隔离/脱敏验证，不能因字段名叫SanitizeForLog就认为已脱除个人信息。

## 3. 最小限定单账号准备器设计（待批准实现/执行）

不是新注册接口、不是公开管理API，不改变运行中认证配置。建议本地受控一次性命令，默认dry-run，没有批准不得使用apply；业务服务与仓库复用既有能力，新增的仅是测试准备编排与约束。

- 输入：通过保护终端或操作系统受保护输入提供企业邮箱、目标已有空间、viewer/contributor、真实数据准备批准引用；不使用命令行明文、URL、普通文件、聊天、CI或Git。员工本人确认邮箱来源与账号归属。
- 不需要SID Secret、Token、RJGH；不预建身份绑定。OA首次验收才由已验证UserInfo建立绑定。
- 默认角色建议 **viewer**，满足最小读取验收；若需编辑，单独批准contributor，不默认提高权限。
- 先查规范化邮箱唯一命中、用户active、所有空间/组织角色、高权限与绑定/撤销冲突。已有合格用户只加入目标空间；不覆盖已有home tenant或成员关系。
- 新建最多1用户、1目标空间active成员关系，显式TenantProvisioningTenantless，再设置目标home tenant。不得创建个人空间、owner/admin关系、系统管理权限或身份映射。
- 保证上述写入在同一受控事务中完成；现有分散服务调用不自动等于同事务。实现前核对事务注入能力，必要时增加仅供准备器使用的最小事务编排，不修改登录主流程。
- 并发锁及邮箱唯一性复核必须在事务内；提交前重新核对全局模式/门禁/目标空间及用户数量基线。冲突即回滚，不能靠重复调用重试。
- 数据库身份使用限时最小权限、仅指向打包机测试库；不读取业务环境全量配置、不开启SQL参数日志。标准输出仅阶段、布尔值、计数、安全错误枚举。原始错误、随机密码及员工字段均不输出。
- 新账号密码应由运行进程安全随机生成并按现有业务规则哈希，不进入报告；如要验证本地密码登录，由员工另走批准的安全设置流程，不回传明文。
- 公开dry-run输出最多：email_match_count、ordinary_eligible、workspace_valid、role、planned_user/member/tenant_changes、conflict_category、write_approved。身份明细仅保护基线保存，不输出邮箱或其指纹。

本轮实际dry-run仅完成无员工输入的环境和能力检查；上述写入准备器还是 **DESIGN_ONLY**。不能将文档当作已运行的账号创建程序。

## 4. 知识库与禁止访问对照资源

1. 已有空间owner通过现有业务流程准备一个有效知识库及非空纯合成文档，例如“验收问题A的答案是合成值A”；记录摄取完成和实际读回断言，不能只创建空壳知识库。
2. viewer读取允许、写入拒绝；contributor按实际批准的写权限验证。以真实HTTP授权结果校验，不只看界面按钮。
3. **不能在同一空间随便建第二个知识库就声称它应禁止读取。** 当前internal/application/access/knowledgebase.go的ResolveKB对同tenant资源有空间内授权路径；具体写权限另受路由/角色约束。
4. 禁止读取对照应为确实存在、位于该用户没有成员关系的另一测试空间、且没有组织/Agent共享授权的合成知识库。owner/受控准备者确认其存在，普通用户访问应403或按防枚举策略404。不存在的随机ID不能证明越权防护。
5. 当前只存在一个空间，尚无该对照夹具。优先复用后续获批的已有隔离测试空间；如无，另批一个专用资源隔离空间与合成知识库夹具。这不是给员工创建个人owner空间，也不得提升该员工权限。本轮未创建，不能把该验收项标为就绪。

## 5. 受保护权限基线与成功断言

准备完成后保护清单保存：原user_id、home/active tenant_id、所有成员角色/状态、组织管理角色、允许KB及其ACL/共享来源、禁止KB存在性及无授权依据。清单限指定验收与撤销操作人访问；普通报告只输出原值相同布尔值。

真实OA后：user_id相同、tenant/角色/ACL相同，允许内容实际可读，禁止资源拒绝，身份绑定仅新增批准映射，未新增重复用户或权限；退出后原Session/Access拒绝。

准备器单账号数据事务失败直接回滚。已提交夹具撤销/清理按明确ID另批，不按邮箱模糊删除、不降权旧owner、不删原数据库或卷。已经发生OA绑定时先撤销会话与审核绑定，不能直接删除用户绕过审计。

## 6. 需要用户批准的最小项目

| 决策 | 当前状态 |
| --- | --- |
| 一个测试员工邮箱与账号归属，保护渠道输入 | 待指定，不在聊天提供 |
| 既有目标空间及viewer（推荐）或contributor | 待空间负责人批准 |
| 限定单账号准备器实现及真实数据写入 | 待独立批准；本轮无写入授权 |
| 一个允许KB与非空合成内容 | 待批准数据范围及清理人 |
| 已有跨空间禁止资源，或最小额外隔离夹具 | 当前缺失，需批准准备 |
| 保护基线、审计访问、保留期限及撤销操作人 | 待指定 |

账号准备与会话/流量安全批准、单次真实登录批准是分开的动作。就绪顺序见 [会话与单次验收审批单](SOURCEID_FIRST_LOGIN_SECURITY_DECISION.md)。
