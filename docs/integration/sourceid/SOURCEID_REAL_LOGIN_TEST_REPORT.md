# 真实 SID OIDC 协议验证报告

日期：2026-10-13。真实协议结果：**NOT_TESTED / BLOCKED_EXTERNAL**。本轮目标为单次只读协议闭环，不是 KnowHub 业务账号登录；用户绑定、权限迁移、生产 callback 架构及真实统一登出均不在当前范围。

## 已完成的环境证据

- 独立项目 knowhub-sourceid-p6 已启动，测试入口 127.0.0.1:18182；app health HTTP200，前端及 API 代理检查通过。
- 新测试数据库 schema3024、dirty=false，SID 五表和 session_id 列存在，用户0。现有运行容器 ID 未变，没有覆盖主机 80/8080 服务。
- 应用源码为 8ae91bc4439dcc93be11844ada5ec1479f2730bc；正确 runtime 镜像 ID sha256:b563fd65e09b4d0fc427cc89d6f2c7e336ef80cc0eac67a97cc6ddf0f3a35592，工件摘要已校验。
- 独立 app 到已知 SID 域名的 DNS/TCP443/TLSv1.3 通过；未请求未知发现或业务端点。
- SID 四开关关闭、SID Secret 未注入。临时日志保存/APM/debug 隔离不等同源码日志专项已通过。

## 真实结果

| 项目 | 状态 | 当前事实 |
|---|---|---|
| 正式 Issuer | BLOCKED_EXTERNAL | 仅确认 SID Base URL；不能将它作为已确认 issuer |
| Authorization/Token/JWKS/UserInfo/client auth | BLOCKED_EXTERNAL | SID 尚未提供正式 Metadata；禁止拼接示例路径 |
| SID authorize 跳转 | NOT_TESTED | 尚未生成或打开真实授权流程 |
| callback 接收 code | NOT_TESTED | 已审批 URL 尚未接入独立只读诊断入口 |
| code 兑换及 Token endpoint 实际行为 | NOT_TESTED | 未向正式 Token endpoint 发请求 |
| Token 格式/字段/类型 | NOT_TESTED | 没有取得真实 Token 响应；不保存完整响应 |
| ID Token 签名/issuer/audience/nonce/有效期 | NOT_TESTED | 没有真实 ID Token，不能用 Mock 或示例作为证据 |
| ID Token 字段结构 | NOT_TESTED | 仅在验证成功后输出字段名、类型和校验结果 |
| UserInfo 请求及字段结构 | NOT_TESTED | 没有真实返回；必须与 ID Token.sub 一致 |
| 工号字段候选 | NOT_TESTED | 未观察实际字段；旧 OAuth attributes.GH/login/gid 不算 OIDC 证据 |
| SessionIndex/sid 对应关系 | OUT_OF_SCOPE / UNCONFIRMED | 不做真实登出，仅允许观察候选字段是否存在 |
| 用户绑定/权限迁移/生产部署 | OUT_OF_SCOPE / NOT_EXECUTED | 本轮不执行 |

## 尚需实施

现有业务 callback 在 Token/UserInfo 验证后进入账号解析和本地令牌生成，不适用于本轮无绑定目标。独立只读诊断入口已实现为 cmd/sourceid-probe，云端验证状态和操作说明见 [Probe 实现](SOURCEID_PROTOCOL_PROBE.md)；尚未启动真实入口。不能关闭 SourceID 校验切换到通用 OIDC 自动开户路径。

临时入口候选见 [callback 转发方案](SOURCEID_TEMP_CALLBACK_FORWARDING.md)：优先由可独立配置的外层网关精确转发；若域名直接指向现有 80/8080，则需审批专用测试 callback。两者均未执行，不改 hosts，不直接修改现有服务。

待正式 Metadata、只读诊断及临时入口齐备后，操作员交互输入 Secret 到诊断进程内存，用户只在正式 SID 页面输入密码；回调自动兑换 code，不人工复制授权码，不保存密码、Secret、Token、Cookie 或 HAR。完成或失败后停止诊断进程，业务 SID 及 backchannel 保持关闭。

账号审核映射、员工字段正式语义和 SessionIndex 契约不再阻塞本轮只读协议观察；它们仍是未来业务账号登录/统一登出的独立门禁。READY_FOR_RELEASE=false。
