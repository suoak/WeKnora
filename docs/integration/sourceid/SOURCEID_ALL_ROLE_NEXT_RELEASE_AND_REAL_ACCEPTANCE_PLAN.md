# 全角色 OA 关联：下一次发布与真实验收步骤

> 后续更新：APP-only发布已获批准并于2026-10-16完成，默认APP现在为035f35e1d，Frontend1ba24ef5f、DB3027及企业门禁不变。见[实际部署报告](SOURCEID_ALL_ROLE_APP_DEPLOYMENT_REPORT.md)。旧库复用和真实OA放行仍未授权；下文保留原分阶段方案。

日期：2026-10-16。当前状态：代码/CI/旧库隔离验证完成，**默认服务未升级、未切库、未放行企业登录**。

## 1. 已完成的前提

- 功能提交 `035f35e1d388a2f02615d35cf826bbdcc22e56b0`：所有既有角色首次自动关联，保留账号权限；无新迁移。
- CI四作业成功，候选image `sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286`。
- 原79库完整迁移至3027、账号/ACL一致，候选APP合成管理员登录、文件读取、viewer跨空间拒绝及库内向量召回通过。
- 当前默认APP431226c3b、Frontend1ba24ef5f、当前DB3027、oauth_form和企业门禁保持不变。

详见 [全角色实现](SOURCEID_ALL_ROLE_AUTO_BINDING_IMPLEMENTATION_REPORT.md)、[隔离迁移验收](SOURCEID_LEGACY_79_TO_3027_ISOLATED_MIGRATION_REPORT.md)。

## 2. 独立申请的发布范围

### A. 只升级默认APP代码

收到单独授权后，重新核对现场和候选OCI revision，备份受保护配置与当前库，保留原APP镜像。保持当前Compose文件顺序，在末尾增加**仅覆盖APP image ID**的pin，不能覆盖Frontend；使用固定镜像、no-deps/no-build/pull-never仅重建APP。

Frontend继续1ba24ef5f，数据库继续3027，PostgreSQL、Redis、DocReader和数据卷不动，无迁移。oauth_form、协议/身份/会话门禁不变。验收健康、本地认证、OIDC配置、MCP、飞书与callback失败行为。失败只恢复原APP image pin；不误回raw，不降数据库。

### B. 复用原数据

必须另行批准，不能把A的代码发布当数据库切换授权。原库副本带有合成邮箱、身份、对照资源和已失效测试凭据，不适合直接作为默认库。

从受保护原79快照恢复新的独立目标，完整迁移并复核原user_id/tenant/角色/ACL与文件，再制定正式模型/连接凭据、会话失效和外部任务配置。保留当前3027验收库及原79库，不合并同邮箱的当前viewer与原管理员，不覆盖原卷。正式切库前列清停机窗口、原/新DB及文件配置、写入冻结点、回滚及新写入处理；未经批准不执行。

## 3. 真实OA放行前仍需确认

已确认RJGH稳定唯一不回收、RJEMAIL可信、SID授权时拒绝无效员工，不重复索要这些事实。

剩余SID边界仅三项：停用传播上限D、已有SSO下再次authorize是否重查准入、未消费授权码最长有效期与停用失效边界C。不得假定Token200等于持续在职。

内部需批准覆盖所有账号角色的自动关联政策、`local_approved + authorization`会话/准入政策及明确期限、无Refresh续期、撤销责任人、邮箱历史归属维护规则和验收流量隔离。既有10m候选并未因此获得批准；本轮Mock使用2m不构成真实策略。

启用配置需填写真正批准的政策引用及期限，不使用本轮synthetic引用：

| 配置 | 启用前要求 |
| --- | --- |
| SOURCEID_PROTOCOL_ONLY | 当前true；批准后方可false |
| SOURCEID_CONTRACT_CONFIRMED | 当前false；确认完整启用条件后方可true |
| SOURCEID_EMAIL_BINDING_ENABLE / EMAIL_CONTRACT_CONFIRMED | 当前false；可信邮箱契约已有输入，真实变更仍需批准 |
| SOURCEID_AUTO_BINDING_ENABLE / POLICY_REF | 当前false；采用覆盖全部角色的正式策略及真实引用 |
| SOURCEID_SESSION_MODE | 当前默认external；批准后才用local_approved |
| SOURCEID_ADMISSION_MODE / AUTHORIZATION_RECHECK_CONFIRMED | 使用真实确认的authorization及SSO复核边界 |
| LOCAL_POLICY_REF / ADMISSION_POLICY_REF及四项期限 | 安全负责人正式批准，不抄Mock值 |
| SOURCEID_LOCAL_REFRESH_ENABLE | authorization首期false，续期端点必须实测拒绝 |
| SOURCEID_LOGOUT_ENABLE | 继续false，不宣称SID统一登出完成 |

继续严格RS256、Issuer/Audience/nonce/时间和UserInfo sub校验；历史偶发signature失败保留诊断，不作为重试或放宽依据。

## 4. 单次真实业务验收

先确定受批准的目标数据库：当前验收库的普通viewer，与原库管理员是不同user_id；验证“原管理员与原知识库”须使用获批的原数据目标。账号和ACL私有基线只保存在受保护渠道。

1. 备份现场，按批准措施隔离所有实际入口并分别验证允许/禁止来源；自动绑定开关为全局，不能用“只测一人”替代隔离。
2. 从已有受控凭据流程加载有效SID Secret，不能把合成CA/Mock端点/loopback白名单带入真实运行。
3. 独立获批后仅一笔新code事务：OA→SID授权→真实Handler→Token→ID Token→UserInfo→邮箱匹配→RJGH绑定→本地会话。
4. 核对Portal真实渲染、auth/me200、原user_id/tenant/管理标记与成员角色/ACL一致、知识库读及检索。管理员按原管理权限验收；普通账号验证实际存在且无间接授权的跨空间资源拒绝。不新增或提升权限。
5. 验证退出后旧Access401、禁止Refresh续期、到期重新授权及定向撤销；不将KnowHub退出等同SID退出。
6. 任一严格校验失败即停，不重复登录；Token成功但签名失败不能标记SSO成功。
7. 收尾恢复原门禁，保存脱敏诊断及私有一致性结果。若已真实建立绑定，应按正式审核撤销流程处理，并保留审计，不能用删库回滚。

## 5. 回滚边界

纯代码发布没有schema变化，回滚使用431226c3b固定APP镜像，保持Frontend/DB/oauth_form。真实放行后若需回滚，先定向撤销本轮企业Session和派生Token，并按审批处理新绑定；旧版会优先识别已批准绑定，不能假设回退角色限制就自动撤销授权。

原数据复用失败只回到批准的原DB/文件配置，保护两个数据库及卷；发生新业务写入后必须先审查差异，不能直接覆盖数据。隔离预演本身已收尾，无需回滚当前默认服务。

尚未完成的验收：真实OA全角色业务登录、旧库副本浏览器渲染专项、获批模型的自然语言embedding/重排与完整RAG质量。当前证据支持申请下一阶段，不替代以上验证。
