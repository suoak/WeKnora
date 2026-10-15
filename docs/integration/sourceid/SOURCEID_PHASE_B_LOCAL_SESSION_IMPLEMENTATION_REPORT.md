# 阶段 B 本地企业会话实现报告

当前为默认关闭的开发版本，未部署打包机或生产。代码提交 `431226c3b1bd5338ec06c1bd0520e637d5fed398`；[CI](https://github.com/suoak/WeKnora/actions/runs/38717758880) 四个 job 全部通过，包括真实 Vue 与认证 Handler 的 Mock 浏览器验收、认证/MCP/飞书回归、PostgreSQL 并发及迁移链、服务器与前端构建。结果见 [验收报告](SOURCEID_OA_LOGIN_E2E_ACCEPTANCE_REPORT.md)；不能将开发完成等同真实放行。

## 代码位置

| 模块 | 位置 | 行为 |
| --- | --- | --- |
| 配置门禁 | internal/config/sourceid_local_session.go、config.go | 默认 external；显式政策引用和期限；授权准入必须禁用 Refresh |
| 准入接口 | internal/types/interfaces/sourceid_admission.go | ALLOW/DENY/UNKNOWN、时效、复核语义 |
| 授权证明/准入 | internal/application/service/sourceid.go、sourceid_local_session.go、user.go | 消费 state 后、Token/ID Token/nonce/UserInfo 通过后产生内存证明；不凭 Token 200 放行 |
| 本地会话仓库 | internal/application/repository/sourceid_local_session.go | 审核绑定关联、空外部索引、准入/空闲/绝对限制、撤销后拒绝 |
| JWT/Refresh | internal/application/service/user.go、repository/sourceid.go | 会话约束、单次 Refresh、重放撤销、前驱消费、过期不续 |
| 新迁移 | migrations/{versioned,sqlite}/003027_sourceid_local_session.*.sql | 仅新增元数据表；down 先撤销局部 Session/Token，保留原账号及权限 |
| 合成验收 | service/sourceid_local_session_test.go、repository/sourceid_local_session_test.go、handler/sourceid_local_browser_test.go、frontend/tests/oa-login/sourceid-local-e2e.mjs | 服务/仓库/浏览器分层断言 |

正式 `authorization` 适配器已实现但门禁未启用。其他两种准入方式仅为测试 adapter，不是已发布 SID 接口。所有时长字段为空时拒绝；本轮没有设置正式有效期或读取真实凭据。

## 迁移及部署门禁

用户提供当前打包机版本 2fcbcaaed、数据库 3026；本轮不连接服务器复核或执行迁移。待单独授权部署时：核对版本/clean 状态，备份数据库与配置，保留 app/frontend 旧镜像，先在克隆库验证 003024→003025→003026→003027 完整链。不能跳过飞书 003025。

003027 不重建数据库/数据卷，也不更改邮箱、角色、权限或已有外部会话。应用回滚可以保留新增表，但必须先关闭企业登录并撤销本地企业 Session/派生 Token，避免旧应用忽略新增的空闲、准入和 Refresh 限制。确需 down 必须先归档审计和元数据；down 脚本也会先撤销本地企业 Token/Session。原数据库或数据卷不得删除。

审批后的应用回滚顺序：关闭入口及新增模式→备份并归档→在事务中按 `source_id_local_sessions.id` 定向撤销→恢复旧 app/frontend 镜像→复核本地登录、端口和非 SID 功能。以下 SQL 仅为待审批操作模板，本轮未执行：

```sql
BEGIN;
UPDATE auth_tokens SET is_revoked = TRUE
WHERE session_id IN (SELECT id FROM source_id_local_sessions);
UPDATE source_id_sessions SET revoked = TRUE
WHERE id IN (SELECT id FROM source_id_local_sessions);
COMMIT;
```

真实环境保持 SOURCEID_PROTOCOL_ONLY=true、SOURCEID_CONTRACT_CONFIRMED=false、SOURCEID_AUTO_BINDING_ENABLE=false、SOURCEID_LOGOUT_ENABLE=false，新增本地模式不启用。没有访问 10.51.134.114。

## ID Token 偶发签名失败

未放宽 RS256、issuer、audience、时间或 nonce。现有 verifier 每次抓取受信任 JWKS，没有跨请求应用缓存；无 kid 仅接受唯一可用 RSA key，JWT 带 kid 不匹配时拒绝，不回退。已有合成测试覆盖节点不同 RSA key、无 kid 多 key、轮换、时间和 nonce 分类。

历史 signature 分类不能由客户端追溯确定为哪一个签名节点，也不能归因于时钟；时间错误有独立类别。SID 需用原脱敏时间/Trace 复核签名节点与 `/oidc/jwks` 的 RSA 材料一致性。本轮不读取真实 Token、不重复登录，不因偶发失败加宽验签。

本轮补充两次正常 TLS 校验的公开 JWKS 只读检查（Windows，2026-10-15 17:49:57 北京时间）：均为 HTTP 200，只有一个 RSA 公钥，均无非空 kid，两次 RSA 公钥材料相等。响应 HTTP Date 与本机时间相符。仅保存数量、类型和比较布尔值，见 [机器证据](evidence/phase-b-local-jwks-readonly-20261015.json)。这不能证明 11:24 的签名节点、过去的公钥、打包机时钟或全部 SID 节点一致；历史根因仍为 UNKNOWN。

`at_hash` 检查当前未实现，不能标记为 PASS；本次没有为了签名故障新增或放宽认证协议行为。既有 11:24 脱敏记录没有保留可追溯的具体失败子类别，不补造解析、签名或时间结论。
