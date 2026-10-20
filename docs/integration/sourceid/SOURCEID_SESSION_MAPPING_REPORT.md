# OIDC 与 SourceID SessionIndex 会话映射报告

状态：**BLOCKED_EXTERNAL / NOT_TESTED**。没有真实会话数据、SID官方OIDC映射回复或两设备相关性证据。

已知证据：企业飞书OIDC全文未定义可用的tgt/SessionIndex对应字段；[SourceID登出文档](https://sourceid.ruishan.cc/linkid/authentication/introduction/logout.html)描述传统CAS ST、OAuth tgt与通知SessionIndex的关系，不证明企业OIDC使用相同字段。JWT的jti、sub、auth_time或任意名为sid的字段均未确认等价。

当前实现：明确 `SOURCEID_CLAIMS_SOURCE` 为 id_token或subject一致的userinfo，从 `SOURCEID_SESSION_CLAIM` 读取非空字符串；与工号、状态使用同一选定业务来源。保存 `SHA256(issuer + NUL + index)`，本地随机session ID随refresh/空间切换传递。通知按配置issuer+SessionIndex撤销关联会话和令牌，写幂等tombstone。最长7天，不自动清tombstone。

| 验证项 | 结果 | 门禁 |
|---|---|---|
| 登录是否返回可信tgt或等价字段 | NOT_TESTED | 在验签ID Token或sub一致Profile中确认path/type及语义 |
| 是否仅Token Response提供字段 | NOT_TESTED | 当前未支持此来源；若正式要求，最小适配和专项/云端验证 |
| 与通知SessionIndex同次会话相等 | NOT_TESTED | SID回复和受控D/E，保存相等bool/匿名标签，不保存原文 |
| 两独立设备index不同 | NOT_TESTED | G；若共享TGT则同index应一并撤销，不能承诺按浏览器隔离 |
| index稳定且不复用、长度/空白规则 | BLOCKED_EXTERNAL | SID签署生命周期契约；现parser会trim SessionIndex，需确认原值不依赖边界空白 |
| 刷新/空间切换/重启撤销 | 仅Mock/数据库CI通过，真实NOT_TESTED | F实际凭据拒绝与持久化证据 |
| 未知index/重复消息/重试 | 仅Mock通过，真实NOT_TESTED | H及正式送达规则 |

没有等价证明时不设置猜测path，不用工号/email/sub/userID替代，也不按用户撤销全部设备伪装精确登出。缺少字段会安全失败，保持入口关闭。字段语义不同或多来源需要正式契约后最小适配；不重构已验证认证模块。
