# SourceID 身份与会话设计

## 身份

独立 `external_identities` 保留 provider/issuer/sub/employee_no/user_id 和登录时间。唯一键分别为 provider+issuer+subject、provider+issuer+employee_no、provider+issuer+user_id。
这是当前适配的保守约束：同一 issuer 下每个本地用户只绑定一个员工自然人和一个 sub，不支持静默 sub 轮换或岗位多身份。真实 SID 多身份规则确认前不能取消约束。

审核登记存在 `source_id_enrollments`，含 reviewer、review reference。首次绑定先锁登记行，再验证用户启用/未删除、身份不冲突。重复登录使用相同 User ID；邮箱变化不影响绑定。
审核登记必须保留，可撤销用户状态阻止登录。任何邮箱相同都不自动绑定；工号缺失、类型异常、未知映射或状态不符拒绝。

新增员工采用现有管理员开户 API 和原默认空间策略，再通过审核 CSV 登记。本版没有未知员工自动开户，避免无法辨别历史账号时重复创建；自动开户需真实身份生命周期规则和额外审批证据，尚未实现。

## 会话

SID 登录建立随机本地 session ID。数据库仅保存 `SHA256(issuer + NUL + SessionIndex)`，不保存外部会话原文。
Access/Refresh 的 JWT 与 token 行共同记录会话 ID。刷新、空间切换继承它，不依赖工号/邮箱。
校验 access、刷新及 sandbox token 复查都检查持久化会话和登出 tombstone；错误拒绝访问。
刷新事务锁会话并条件消费旧 refresh，防止并发重放；新令牌与消费在同一事务。

外部登出在事务内写入 tombstone，撤销该 issuer+SessionIndex 的会话及令牌。无匹配时也写 tombstone，防止先登出后 callback 复活。
重复通知幂等。其他外部 SessionIndex、本地设备和独立 API Key 不由外部登出处理。
本地 SID Logout 只撤销当前会话族，不调用 SID `/logout`。

SID 会话最长 7 天，刷新不会延长该期限。tombstone 不自动删除，避免外部会话标识重复使用造成复活；真实 ID 生命周期确定后再制定保留策略。

## OIDC 与前端

复用现有 Discovery、Token Exchange、JWKS/SSRF 客户端。SID 强制签名 ID Token、nonce、issuer/audience/expiry/issued-at/azp/sub 验证。sub 必须为字符串，nonce 和 UserInfo subject 精确比较，不通过空白归一化掩盖不一致；Token/UserInfo 响应有大小限制。
SID state 和 PKCE verifier 保存在 `oidc_transactions`；公开 state 中无 verifier。授权 URL/config/start 响应禁止缓存，回调消费状态只允许一次。PKCE S256 由显式配置决定，不猜测 SID 支持情况。
业务属性可明确选 `id_token` 或 `userinfo`；后一种要求 UserInfo sub 与验签后的 sub 一致且请求成功，不混用不同来源的属性。Token Response 的专有会话字段未实现，若真实协议要求此来源，保持 BLOCKED_EXTERNAL。

callback 把本地登录结果保存 60 秒，以 HttpOnly、SameSite cookie 保存随机兑换键；浏览器 URL 只出现 `#oidc_complete=1`。
POST result 要求自定义请求头以阻止普通跨站表单，结果单次消费后数据库清空内容和 verifier。响应设置 no-store。
前端复用原用户/成员/空间权限恢复，通过后台 url 接口读取固定回调配置；保留目标页和邀请 token 的暂存。请求失败留在登录页显示安全提示；门户直接链接仍可使用 start。

## 通知信任与解析

本地方案：身份验证入口网关只放行 SID 确认的来源，并清除外部伪造 `X-KnowHub-SourceID-Secret`，随后注入内部随机 secret；应用要求直接 socket peer 命中 CIDR 且 secret 恒时匹配，不信任 X-Forwarded-For。
该头是 KnowHub 网关约定，不能宣称 SourceID 原生发送。未落实网关/来源认证前不可启用生产。

仅接受 form-urlencoded POST body 单一 logoutRequest，不接受 query 参数；原始/压缩输出 64 KiB 上限、请求体 128 KiB。
XML 要求正确命名空间、版本、ID、IssueInstant 和单一直接子 SessionIndex；拒绝 DTD、XXE、过深、缺字段和多文档。
可显式选 raw XML 或 base64+raw DEFLATE；后者仅 Mock 覆盖，不代表正式算法已确认。
HTTP 200 空响应是本地候选契约；生产需确认。IssueInstant 只验证格式，未擅自添加时效窗口以免破坏 SID 重试，已注销 index 以 tombstone 幂等处理。

## 风险

已审批 HTTP callback 存在授权码截获、cookie/令牌交换明文风险；上线前安全团队需验收网络隔离，未来迁移 HTTPS 要重新审批 URI，不能静默修改。
新会话/事务表和现有 token 表含认证资料，应限制数据库访问和备份权限。认证 token repository 及 SID repository 禁止 SQL 参数日志。
PKCE/claims/信任配置不是外部证据；启用开关不能代替正式联调验收。
