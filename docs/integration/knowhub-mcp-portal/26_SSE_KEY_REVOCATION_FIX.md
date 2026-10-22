# 第四批：旧 SSE 撤销 Key 的根因与修复

本修复仅存在 RC3 新隔离候选的 Python 源码，未修改或切换生产 Python MCP。MCP_AUTH_MODE 使用 `weknora_api_key`，没有降低 `/auth/me` 的身份检查。

## 修复前真实复现

任务 `41e207`、候选 `4c788a66149e4b143d52479c6588ebde60de84e6ed6bbafe5ad95bea5f463711`：有效 Key 初始化、正常工具调用成功，保持真实 SSE 流，撤销 Key 后下次工具调用被服务端拒绝，但 SDK 2.2 等待 10 秒后测试失败，exit=1。证据：[修复前原始输出](evidence/batch4/linux/41e207/python-http-sse-go-acl.log)。第三批原 FAIL 同时保留。

## 已确认根因

旧 SSE 使用 GET `/sse` 维持服务端消息流，JSON-RPC 通过 `/sse/messages/?session_id=…` POST。认证中间件正确返回 POST 401，但这不是 GET SSE 上的 JSON-RPC 完成消息。SDK 2.2 和 2.3 的 `mcp/client/sse.py::post_writer` 捕获 `raise_for_status()` 异常后仅记录日志，未向等待中的 RPC 转发失败，GET 又仍然存活，导致 dispatcher pending。真实抓取和栈证据与这个调用关系一致，不把单纯服务端 401 当成 SDK PASS。

SDK 2.2 安装模块哈希及相关异常分支见 `evidence/batch4/sdk22-source.log`；Streamable HTTP 原本能传播有界 MCPError(-32603)，同样通过业务拒绝观察器佐证，不能凭通用异常认定鉴权成功。

## 修复源码

`mcp-server/weknora_mcp_server.py::MCPAuthMiddleware`：

1. 仅登记已认证 GET 流中服务端签发的 endpoint/session，索引为凭据及工作区单向摘要 + session_id；不记录明文 Key。
2. 未通过实时凭据验证的消息 POST 仍然返回 401；不调用 MCP app、工具或知识 REST。
3. 仅对完全匹配的旧认证流，从受限请求体提取 JSON-RPC id，发送 `-32001 Authentication failed` 错误，然后结束 SSE 流并通知应用断开。
4. 常规写流与鉴权错误使用同一异步锁，关闭后抑制写入；finally 清理服务端 channel。未知 session、不同主体/工作区、通知、非法 id 均不向其他流注入消息。
5. 请求体最多 64 KiB、读取 1 秒、错误输出最多 2 秒；断开或格式错误仍为 HTTP 401，绝不进入业务处理。

新增 `mcp-server/test_sse_auth_revocation.py` 验证跨凭据、跨 workspace、未知 session、通知/非法 id 不注入，正确请求关联 id，stream 退出后清理且错误不含 Key。既有 Python 断言完整保留。

## 协议与 SDK 验证

`tools/batch4_python_http.py` 使用真实 SDK、HTTP/SSE、Go ACL 合成后端，记录时间、HTTP 路径/状态、RPC id、SSE endpoint 摘要、消息 error code、EOF、SDK 异常，不记录认证 Header 或明文 Key。后端 `document_python_integration_test.go` 新增知识 REST 调用计数。

已验证 SDK 2.2 首次修复运行：SSE id=4 收到 -32001、EOF、POST 401，约 0.028 秒返回 MCPError；旧 session 再调用返回 -32000 Connection closed；撤销 Key 重连 401，另一有效 Key 新建会话可读。HTTP 约 0.005 秒返回有界错误。两种 transport 撤销后知识 REST 调用增量均为零；`/auth/me` 确实产生新的拒绝。

SDK 2.3 / Python 3.12 的真实 Windows TCP 同样通过，原始证据位于 `evidence/batch4/windows-sdk23-integration.log`，exit=0。最终 Linux 2.2 结果和完整回归以 [30](30_FOURTH_BATCH_TEST_RESULTS.md) 为准。

| 验收维度 | 已验证结论与边界 |
|---|---|
| Server auth 正确性 | PASS：撤销后重新验证失败，POST/重连拒绝，auth/me 不接受旧 Key |
| SSE 协议响应 | PASS：对应请求错误帧、EOF、POST 401；未让拒绝消息进入 MCP handler |
| SDK 错误传播 | PASS：2.2/2.3 有界 MCPError；关闭、清理及重新认证已测 |
| 业务数据不可读取 | PASS：本合成真实 REST 适配器，撤销后知识 REST 增量 0，未返回正文 |

这些 PASS 不认证生产 rich Handler 的文档 ACL。当前修复针对已测 SSE 端点格式与两个 SDK 版本；反向代理多 worker、断网慢读、其他 SDK 版本及生产拓扑仍 NOT_TESTED。会话与登记表必须同 worker；未知/不可匹配 session 仍拒绝 HTTP，不能宣称该拓扑也已通过有界 SDK 回归。生产接入前应固定会话路由并补真实代理测试。

回滚为撤去本地 Python 差异；会恢复已知旧 SSE 错误传播缺陷，因此不能以回滚替代安全验收。生产尚未部署，无生产回滚动作。
