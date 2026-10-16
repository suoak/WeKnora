# OA 全角色首次自动关联实现与安全评审

> 发布状态更新：此功能随后获批准完成APP-only发布，默认APP已为035f35e1d，真实身份门禁仍关闭。见[部署报告](SOURCEID_ALL_ROLE_APP_DEPLOYMENT_REPORT.md)。下文NOT_DEPLOYED指初次实现验收时点，非最新运行状态。

日期：2026-10-16。结论：**IMPLEMENTED / SYNTHETIC_PASS / CI_PASS / NOT_DEPLOYED_TO_DEFAULT_SERVICE**。

本次产品输入：所有既有账号均可首次自动关联，不按 viewer、contributor、owner、租户/组织/系统/跨租户管理员角色拒绝；保留原账号与权限。实际部署和真实企业绑定仍需单独授权。

## 1. 独立功能提交

`035f35e1d388a2f02615d35cf826bbdcc22e56b0`，已推送 `feature/knowhub-sourceid-oidc`。仅包含：

- `internal/application/repository/sourceid_binding.go`：移除 `ordinarySourceIDAutoAccount` 及首次自动批准时的角色过滤。
- `internal/application/repository/sourceid_auto_binding_test.go`：补充全角色保留、冲突和撤销测试，调整旧角色拒绝用例，PostgreSQL 并发用例覆盖已有系统/跨租户管理员与 owner。

未新增迁移、API、注册路径、个人空间或配置开关。未修改 authorize、Token、Basic、state、nonce、ID Token/JWKS、员工准入、本地会话及租户授权代码。未纳入工作区其他 MCP、翻译或实验性改动。

## 2. 保留的安全规则

自动入口仍要求非空政策引用，并由业务服务在严格 OIDC、可信 UserInfo、员工准入和企业身份门禁通过后调用。仓库不会自行证明 UserInfo 来源；不能把仓库方法当公开身份验证 API。

首次候选仍要求规范化邮箱唯一匹配已有 active 账号。审核事务仍锁定 review 和原用户，重新核查当前邮箱、唯一命中、issuer/RJGH、sub、user_id 的冲突，依靠既有唯一约束保证并发一致性。角色不参与拒绝或写入；没有增加账号权限。

已批准的绑定优先于后续邮箱；邮箱变化不会转移身份。撤销不能自动重新绑定；被人工 reopen 的记录仍需再次审核。审核、审计、会话和派生令牌撤销保留。

## 3. 合成与 CI 验证

| 验证 | 实际结果 |
| --- | --- |
| viewer、contributor、owner、租户管理员、组织管理员、系统管理员、跨租户管理员首次自动关联 | PASS |
| 既有无成员关系的历史账号 | 关联层 PASS；不授予新空间权限，后续资源授权仍独立检查 |
| 再次登录及邮箱变化 | 同一 user_id，PASS |
| 原用户完整记录、tenant_id、管理标记、成员/组织角色及合成 ACL 原值不变 | PASS |
| 缺失/重复邮箱、inactive、RJGH/sub/账号冲突 | 拒绝，PASS |
| 撤销后自动重绑、人工 reopen 后自动批准 | 拒绝，PASS |
| PostgreSQL 8 个并发首次登录，唯一身份/登记/审核/批准审计 | PASS，CI PostgreSQL 作业执行 |
| 原管理员在旧库迁移副本中的真实业务 HTTP 合成认证 | PASS，见隔离报告 |

本地 Go1.26.8 目标仓库测试通过（2.497s）；本地未配置 PostgreSQL 的用例会跳过，不能用本地结果代替 PostgreSQL 实测。CI [38828530863](https://github.com/suoak/WeKnora/actions/runs/38828530863) 对上述完整 SHA 的 `linux`、`postgres`、`frontend`、`browser` 四个作业全部成功；包含 race、认证/MCP/飞书回归、前端构建及真实 Vue + Mock SID 浏览器验收。[CI 收据](evidence/allroles-ci-20261016.json)。

浏览器 CI 使用合成夹具；旧数据库副本则通过真实 frontend 反向代理和 APP Handler 执行 HTTP 事务。不能将副本 `/portal` 静态路由200写成已在该副本完成浏览器渲染验收，不能宣称已解决历史 STATUS_BREAKPOINT。

## 4. 高权限账号安全评审结论

**角色拒绝已移除；身份所有权不能由角色放开代替。**

- 已知 issuer/RJGH/sub/user_id 绑定不一致、另一身份已占用原账号、邮箱重复、候选批准前邮箱改变，仍拒绝；合成冲突用例已验证。
- SID 证明“当前邮箱属于当前员工”，不自动证明“历史 KnowHub 邮箱账号始终属于该员工”。若企业曾把邮箱重新分配，而旧 KnowHub 账号仍 active、从未有可核查身份绑定，现有首次邮箱算法无法独立发现这段历史。不能宣称已经排除此风险，也不能推测其发生。
- 正式启用政策应覆盖所有角色，并明确企业邮箱回收与 KnowHub 账号停用/归属维护职责。若有明确归属冲突必须先修复数据；不按角色一律要求人工绑定，也不通过猜测员工姓名判断归属。
- 高权限账号一旦错误关联，影响范围更大；因此仍需对全角色自动关联政策做正式安全批准和审计。此项为启用审批，不重新引入代码层面的管理员角色拒绝。
- 本轮只产生隔离副本中的合成 SID 身份，未写入真实员工绑定，未打开默认服务任何企业业务门禁。

## 5. 候选及发布边界

基于已提交源码归档，使用现有 Go1.26.7 离线缓存与固定运行库构建候选：

- APP 候选 `knowhub-allroles/app:035f35e1d`
- image ID：`sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286`
- OCI revision：`035f35e1d388a2f02615d35cf826bbdcc22e56b0`
- 固定父运行镜像：`sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1`

父镜像仅新增本次已编译二进制和 OCI 标签，未从运行容器读取或复制环境凭据，沿用原3027迁移文件。[候选收据](evidence/allroles-candidate-image-20261016.json)。该候选只用于本轮隔离副本，默认 APP 未升级。

下一步见 [部署和真实验收方案](SOURCEID_ALL_ROLE_NEXT_RELEASE_AND_REAL_ACCEPTANCE_PLAN.md)。
