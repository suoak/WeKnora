# Protocol Probe 验证报告

最新补充：2026-10-13，SHA `a4fdb8052df13ab2f1286b6b22b91a497337ad77` 的 [CI 38353155370](https://github.com/suoak/WeKnora/actions/runs/38353155370) 全部通过；已审批 callback 的专用浏览器→SSH→独立 Probe 现场实测 PASS，见 [SOURCEID_BROWSER_CALLBACK_PROBE_REPORT.md](SOURCEID_BROWSER_CALLBACK_PROBE_REPORT.md)。真实 SID 仍 NOT_TESTED，当前阻塞为正式 Metadata。下面 c855 的 CI 及现场数据保留为历史验证基线。

2026-10-13。代码/云端验证：PASS。真实 SID 验证：NOT_TESTED / BLOCKED_EXTERNAL。READY_FOR_RELEASE=false。

验证 SHA：c855338b9530d2a7c6d14ce8485b4474cde0d1e8。

[GitHub Actions 38327545586](https://github.com/suoak/WeKnora/actions/runs/38327545586)：linux、postgres、frontend 均 success。核对下载后的证据工件 head.txt 与该 SHA 一致，focused/regression JSONL 无 fail。

| 检查 | 结果 | 证据 |
|---|---|---|
| 格式/diff/go vet/构建 | PASS | Linux job；server、审核映射工具及独立 probe 均构建 |
| 认证专项 race | PASS | 67 个顶层测试、123 个含子用例通过事件；失败0 |
| 认证/MCP 回归 | PASS | 3146 个顶层测试、4648 个含子用例通过事件；失败0 |
| PG17/前端 | PASS | 各 job success；未连接生产数据库或真实 SID |
| 单次 Mock 协议闭环 | PASS | TestOIDCProtocolProbe：授权、post/basic、PKCE、验签、nonce、UserInfo；无仓库依赖 |
| 错签/issuer/aud/expiry/nonce/sub | PASS | 安全拒绝；不绑定、不签发本地 JWT |
| HTTP/重定向/错误 | PASS | Token/UserInfo 失败及 Token/JWKS/UserInfo 重定向拒绝；原始错误正文不输出 |
| 未确认 Metadata/业务 callback/offline_access | PASS | 配置拒绝测试；不拼接发现地址或示例路径 |
| 错误/重复 state、超时/重放 | PASS | 不能兑换 code；消费后关闭、Secret/state/verifier 引用清除 |
| 脱敏输出 | PASS | synthetic Secret、code、nonce、state、verifier、员工/邮箱等值不在报告或响应；无 Set-Cookie |
| Linux 独立进程及结束 | PASS | stdin pipe→Mock TLS SID→callback→结构报告→exit0；端口连接失败证明监听关闭；只留下公开 Metadata/CA 临时测试文件 |
| 真实企业登录/Token/UserInfo | NOT_TESTED | 正式 Metadata 尚未提供，真实 probe 未启动，Secret 未注入 |
| 测试机运行兼容 | PASS（2026-10-13 现场补充） | 已部署原 CI 二进制；私有运行库和公共字典补齐后，18183 实际监听、错误 state 拒绝、停止关闭通过；见实际预检查报告 |
| 正式 callback/临时转发 | NOT_TESTED / BLOCKED_EXTERNAL | 本地隔离代理 synthetic 日志门禁通过；正式入口仍需 SID 登记、网关批准和全链路日志验证 |

“没有用户/成员写入”通过实现无数据库/仓库依赖及成功测试证明：probe 仅直接复用协议方法，不调用业务 callback、LoginWithOIDC、loginSourceID 或 GenerateTokens。原业务默认 HTTP client 保持原行为，probe 单独禁止重定向；全量相关回归通过。

Windows 本地测试因既有 MinGW 缺 rstrtmgr 链接库未通过，未改全局工具链或禁用 TLS/签名。按用户授权采用 Linux 云端验证。首轮 38326548190 的测试 import 编译失败已修正，不用该失败运行作为通过证据。

上述 CI 开发验证阶段未 SSH 部署 Probe。2026-10-13 已在测试机补充实际部署和预检查，见 [SOURCEID_PROTOCOL_PROBE_ACTUAL_PRECHECK.md](SOURCEID_PROTOCOL_PROBE_ACTUAL_PRECHECK.md) 及其脱敏证据。二进制仍为本报告验证 SHA，未修改认证代码；未修改 80/8080、执行业务登录/绑定、调用 refresh/backchannel logout、修改生产数据库或合并主分支。既有独立业务测试实例保持原部署，业务 SID 开关未开启，现场 Probe 和临时代理均已关闭。真实协议报告继续 NOT_TESTED，正式 Metadata 与已批准 callback 及链路日志门禁确认后才执行单账号测试。
