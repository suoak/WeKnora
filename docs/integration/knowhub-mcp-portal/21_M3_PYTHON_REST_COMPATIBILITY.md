# M3 旧 Python MCP 与 REST 兼容验证

旧 `mcp-server/weknora_mcp_server.py` 没有修改。生产模块、运行配置和 Key 均未修改或切换。此处验证使用仓库原模块、真实 HTTP 与 SSE、合成凭据、真实 Go ACL/SQLite 驱动；后端是 **隔离的最小适配夹具，不是完整 KnowHub 业务应用**。不能据此宣称旧 REST 绕过已全部修复。

## 源码与真实路径

`document_python_integration_test.go` 带独立 `knowhub_m3_integration` tag；在装有 mcp 的 Python 3.11 容器中执行编译后的 Go 测试二进制。它启动真实 TCP Gin 后端，使用新 DocumentReadBoundary 和 DocumentReader、真实 SQLite FTS/vector。`tools/batch3_python_http.py` 启动未修改的 Python MCP，`MCP_AUTH_MODE=weknora_api_key`、无共享 WEKNORA_API_KEY，用 SDK 的 ClientSession 走 HTTP /mcp 与 legacy /sse。

| Python 实际调用 | 隔离夹具断言 |
| --- | --- |
| 入场验证 `/api/v1/auth/me` | 每个合成 Key 有独立身份；撤销后拒绝 |
| get_knowledge → `/knowledge/:id` | 普通已审内容可读；猜敏感 ID、未知 ID、mcp_blocked 拒绝 |
| list_chunks → `/chunks/:knowledge_id` | 授权正文可读；敏感父文档拒绝 |
| hybrid_search → KB 名称解析 + `/knowledge-bases/:id/hybrid-search` | 真实驱动仅返回授权文档，不含敏感 canary |
| wiki_read_page → `/knowledgebase/:kb/wiki/pages/:slug` | 未知历史来源拒绝，不认证旧 Wiki 结果 |
| 直接 preview、匿名文件、猜 Chunk ID、batch | 未认证路由拒绝，不执行原内容 handler |
| B 显式文档授权、撤销 Grant、撤销 Key | 已建立会话下一次工具 REST 调用也拒绝 |
| 旧 retrieve Key / full_access 机器身份 | 新公共能力不扩权；mcp_blocked 不因 full_access 放行 |

X-From-MCP=false、浏览器 UA、任意 X-Tenant-ID 都不作为可信来源；敏感拒绝在后端 ACL/隔离屏障执行，Python 只是透明传递认证与 REST 错误。SDK 断连/403 只有明确授权错误才接受，不能把超时/缺依赖当作拒绝 PASS。原始输出见 [22](22_M3_TEST_RESULTS.md)。

实际结果分组：HTTP 与 SSE 的普通读取、敏感/未知/MCP 禁止、Grant 撤销、旧 Key 和 full_access 场景分别执行。HTTP Key 撤销通过；SDK 2.2 隐藏 HTTP 状态时，必须另有 Go observer 记录本次新的后端拒绝，不能接受普通内部错误。**SSE Key 撤销 FAIL**：observer 确认后端已拒绝，但 call_tool 10 秒内没有结束，测试退出 1，未把超时视为权限拒绝通过。此问题须单独完成旧协议取消/错误结束语义的源码诊断和受控候选修复，生产 Python 保持原状。

## 完整应用仍需修复的调用图

1. `routes_knowledge.go` → KnowledgeHandler Get/List/Batch/Search → knowledgeService/knowledgeRepository：当前公共组件未逐个接入这些 rich-response Handler，标题/标签/summary/附件 metadata 也必须授权后读取。
2. ChunkHandler → chunkService GetChunkByIDOnly/List/Revisions/Parent/Neighbor → repository：不能只验证父 KB，必须核实 Chunk 的真实 doc/tenant、当前版本，跨文档 neighbor 不得继承初始授权。
3. KnowledgeBaseHandler search/hybrid → hybridSearchCandidates / resolveStoreGroups → driver → context enrichment / HybridSearchWithRerank：必须在每个候选/邻接/外部模型边界接入相同守卫。
4. WikiPageHandler → wikiPageService GetPageBySlug/GetRevision/GetIndexView/GetGraph/Search → repo；FAQ 直接 list/detail/export/search；图谱外部存储：没有完整来源依赖前全部 HOLD。
5. Download/Preview、presignedFileHandler、shortlink、KB/message 代理及原始文件：要覆盖匿名前置路由、真实附件归属、缓存/引用与对象直连；全员检索不等于文件下载。
6. Go MCP retrieve/grep/agent 与旧 Python 同一 REST 面：新隔离 Router 未认证所有相关路由，所以拒绝；尚未在完整 Go MCP 调用链证明安全，不进入 M4。

静态路由清单见 [read-routes](evidence/batch3/read-routes.log)，该清单不代表每个路由已执行端到端测试。禁止给未核对 Handler 配置通用 always-allow certification。

## 下一步验收

必须用真实生产对应版本在隔离环境启动完整后端，复用正常 Auth/DB/业务 service/handlers，同一合成场景逐路验证 detail/list/batch、计数、Chunk ID/revision/neighbor、Wiki/FAQ/图谱、文件及缓存，加入 HTTP 模型 spy 和 Key scope/成员/共享撤销。当前完整应用 D02/D04/D05/M02 仍 HOLD/NOT_TESTED，不能把夹具 PASS 覆盖这些门禁。

回滚仅关闭/移除可选隔离注入及合成测试夹具；不删除旧 Python MCP，不放宽断言，不恢复历史匿名公开能力。生产影响无。

## 旧模块字节指纹

本批没有改写旧模块。Windows 工作树文件保留 CRLF，实际夹具读取的原始 SHA256 为 `92c989fd88930be3d3d407ff19b701303104ce8c7b9d0ee33a80a705789f75cc`；LF 规范化后为 `c0d2b07a68a0f1501bec7434abc2b3ad9e4b75f82e0be40aa18263f367db0a55`，与冻结 Git/生产基线相同，仅换行差异。两个摘要均保存，未把字节差异隐藏成完全相同的文件。
