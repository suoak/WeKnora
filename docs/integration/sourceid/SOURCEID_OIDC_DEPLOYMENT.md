# 部署与真实 SID 联调

状态门禁：真实协议未确认时 `BLOCKED_EXTERNAL`；Mock 成功不能作为 `SID_INTEGRATION_VERIFIED`。

1. 固定交付 commit，完成 Linux 回归和 PostgreSQL 迁移测试。备份 users、auth_tokens、成员、共享、外部身份表；验证备份恢复。
2. 在隔离测试库执行本仓库 migration 机制的 `003024_sourceid_oidc.up.sql`。PostgreSQL/SQLite 均有 up/down。迁移新增表和 token session_id，不重建 users，不修改成员/知识库表。
3. 使用 sourceid-enroll dry-run 审核旧账号，再在授权测试库 apply。新员工预开户后登记。
4. 安全注入 Client Secret，固定已审批回调 URI，确认生产 issuer/Discovery/JWKS、client auth、scope、PKCE/nonce 和员工状态来源。先只启用 OIDC 的测试环境。
5. 证明 OIDC 会话字段与 XML SessionIndex 相同（同一用户两个独立会话的抓包证据必须脱敏）。若字段在 Token Response 等不支持的来源，先补适配和测试；不能退化用工号注销。
6. 配置经过验证的入口来源认证、网络 CIDR、网关注入内部 secret；禁止互联网直接访问应用后端绕过网关，网关覆盖而非转发该 header。
7. 确认通知 Content-Type、压缩算法、成功状态码、超时重试和会话 ID 生命周期。确认与实现一致后才设 SOURCEID_CONTRACT_CONFIRMED/SOURCEID_LOGOUT_ENABLE。
8. 检查两条网络路径：浏览器→SID，以及应用→Token/UserInfo/JWKS；另测 SID→批准的 backchannel POST。浏览器访问成功不能证明后者可达。
9. 用授权测试员工验证登录、原 User ID/全部角色/共享/跨空间偏好；SID 退出与会话超时；刷新后、切换空间后和重启后旧 access/refresh 均拒绝；另设备/API Key 继续有效。
10. 测试本地应急管理员登录、知识库检索/问答、管理员后台和 MCP/API Key。记录真实通过证据再审批生产发布。

新增接口：

| 接口 | 行为 |
|---|---|
| POST `/api/v1/auth/oidc/backchannel-logout` | 独立来源验证、SourceID XML 通知、持久化精确撤销 |
| POST `/api/v1/auth/oidc/result` | cookie 绑定的一次性登录结果兑换，不需要本地 JWT |

容器 Compose 已透传新增配置。固定共享 JWT_SECRET（安全随机值）保证多实例 state/JWT 一致；所有实例共享数据库。
前后端必须同版本切换，新一次性 cookie 兑换协议不能与旧 fragment 回调协议混用。灰度采用完整实例切换并清理旧前端缓存。
数据库故障不允许通过会话验证；需监控 503 通知失败、登出未匹配事件和人工审核冲突，日志不记录 token/XML/secret。
清理已消费或过期 oidc_transactions 时保留状态防重放到其最大有效期之后；登出 tombstone 未确定 SID index 生命周期前不自动删除。

HTTP 风险：已审批 callback 为 HTTP，授权码及 cookie 传输不能抵御链路窃听，PKCE 只能缓解部分攻击。当前保持审批 URI；迁移 HTTPS 应先获得 SID 新 URI 审批，然后整体更新入口、cookie Secure、反向代理与回调注册并重新联调。
本任务不部署生产、不执行真实 SID 登出。
