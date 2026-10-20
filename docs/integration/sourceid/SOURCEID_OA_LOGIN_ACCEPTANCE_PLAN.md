# OA 登录完整验收方案

2026-10-15。产品目标：打开 KnowHub → 点击 OA 登录 → 公司认证 → 进入原账号；正常路径不展示绑定/员工编号/租户映射页面。本文区分 Mock、真实协议和真实业务放行，不将三者混为 PASS。

## 门禁与前置条件

- 已确认 RJEMAIL 是 SID 已验证并属于当前员工的企业邮箱。仍须确认 RJGH 稳定性与变更规则、员工状态字段/允许值、可信会话与 SessionIndex 契约。
- 真实业务放行须另行批准：SOURCEID_CONTRACT_CONFIRMED=true，SOURCEID_PROTOCOL_ONLY=false；对自动绑定还需要 SOURCEID_EMAIL_BINDING_ENABLE=true、SOURCEID_EMAIL_CONTRACT_CONFIRMED=true、SOURCEID_AUTO_BINDING_ENABLE=true 以及非空 SOURCEID_AUTO_BINDING_POLICY_REF。这些配置当前不启用。
- 仅采用 attributes.RJGH/attributes.RJEMAIL 和已验证 sub 的 UserInfo；状态与会话字段不得猜测。Basic/raw、无 PKCE、openid、严格 state/nonce/RS256/Issuer/Audience/有效期不变。
- 真实员工测试须单独授权；只访问打包机 172.30.232.42，不访问生产主机。域名 callback 和前端需指向同一个测试实例并绕过代理；不复用 code，不自动重试 SID 请求。
- 绑定前只读记录测试用户的 user_id、tenant_id、空间成员角色、知识库访问集合及本地登录基线；敏感员工资料不进 Git/诊断包。

## 合成验收矩阵

| 场景与步骤 | 预期 | 证据/通过标准 |
| --- | --- | --- |
| 普通活跃账号首次 OA：可信邮箱唯一匹配、策略批准、状态正常 | 自动绑定 RJGH 并签发原用户 Session；不创建用户/成员 | 原 user_id/tenant_id 保持，一条 approved review、一条身份/enrollment/审批历史 |
| 第二次登录，同 RJGH/sub，邮箱缺失或变化 | 按已有绑定进入同一账号 | 不新增/改绑 identity，UserID 相同 |
| 首次无匹配 | 安全拒绝/人工处理，无自动开户 | users 数量不增加，无 Session/JWT |
| 规范化后重复邮箱 | 拒绝，不选第一条 | 无身份/JWT；大小写/空白处理按契约，别名不折叠 |
| RJGH/sub/user 冲突 | 拒绝，不覆盖任何原绑定 | 唯一约束/事务回滚验证 |
| 员工停用、未知状态、缺状态 | 在绑定写入前拒绝 | 仓库未访问或无写入、无 Session/JWT |
| 管理员/全空间/空间 owner/admin/组织管理员 | 不自动批准，形成待审或拒绝 | 只有受权管理员审批才生效；角色资料未知也不视为普通账号 |
| 并发首次登录同一身份 | 每笔至多一次 Token，数据库只产生一份绑定/审批历史 | 临时 PostgreSQL 八并发，唯一约束与行锁，无重复用户 |
| 撤销后登录/Refresh/SwitchTenant | 阻止该企业绑定与派生会话继续用 | revoked 墓碑、会话/令牌撤销；本地登录不受企业会话撤销影响 |
| 显式重新审核 | 回 pending，禁止自动重新批准 | 新审核依据、同原用户；不同用户迁移另行审批 |
| 首次候选邮箱变化后审批 | 拒绝旧快照；需重新审核 | 无不完整 enrollment/identity |
| 契约或自动策略未确认 | 仍在安全门禁停止 | protocol_only/contract_false/default_off 回归通过 |
| Token/ID Token/UserInfo 失败 | 立即停止 | 既有诊断及新固定校验错误类别；不保存 Token/code/个人资料 |
| 失败回调前端 | 清除回调标记后回登录页，显示通用易懂提示 | 不提前进入 Portal，不把业务 API 401 标为 SID Token 401；不宣称 STATUS_BREAKPOINT 已根治 |

## 正式契约确认后的单次真实验收（未执行）

1. 管理员核对批准策略、目标普通测试账号、原权限基线与迁移结果；确认无真实自动绑定开关误启。
2. 单独授权后，测试员工手动点击 OA；后端自动消费一次新 code，验证 ID Token 和 UserInfo，再走策略。
3. 正常账号直接进入原空间，不出现绑定确认页；核对 /auth/me 的脱敏状态与原 user_id/tenant_id 是否保持，并实测原有知识库/空间授权。
4. 证明只建立既有账号关联，没有 User/tenant/member 新建；本地登录、MCP/API Key 保持。管理员账号不作为首次自动绑定测试对象。
5. 故障时按阶段分类：Token 401、ID Token、UserInfo、身份/策略、Session/JWT、前端状态；未知校验失败不得放宽。真实故障测试不批量登录，不伪造员工资料。
6. 单次验证后按批准的运行策略关闭临时开关/诊断；本轮没有授权上述真实业务验收。

## 部署与回滚验收

新代码可以在打包机保持门禁部署，但新增 003026 迁移前须协调工作区其他功能的 003025 迁移顺序，备份配置与测试库。无自动删除/重建数据库或 down -v。新开关默认 false，protocol_only=true、contract_confirmed=false、backchannel=false。

回滚先关闭企业身份放行与自动绑定，再恢复旧 app/frontend；保留新增审核/审计表。不要直接执行 down 删除审批历史。真实错绑撤销按管理员审核工具精确处理，保留 User/tenant/权限及本地账号。

阶段 A 两笔真实协议 PASS 不等于本方案业务验收 PASS。真实业务绑定、权限继承和完整统一登出均尚未测试。
