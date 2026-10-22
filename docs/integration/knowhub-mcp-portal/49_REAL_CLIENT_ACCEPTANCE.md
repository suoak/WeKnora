> **Historical batch-7 checkpoint:** See [55: final dynamic Portal policy](55_DYNAMIC_PORTAL_PUBLIC_REQUIREMENTS.md) and [56: current acceptance](56_DYNAMIC_PORTAL_ACCEPTANCE.md). Old approval requirements and test passes do not define the final product or validate the new candidate.

# 第七批：真实客户端验收

## 方法与边界

使用已安装的 **Codex CLI 0.146.0、CSBU WorkMate 2.4.0、Claude Code 2.1.168**。隔离 Go MCP 服务通过真实现有 `user_mcp` service 创建短期合成凭据、实际 PostgreSQL 及原 Auth；临时服务只监听测试机 loopback，经固定主机密钥的 SSH 转发至本机 loopback。没有生产 Token、正文或用户输入外发。

客户端配置放在 `E:/AIData/Codex/knowhub-b7-clients`，不修改日常客户端配置；临时 Secret 不进入仓库和证据日志。Go 夹具 `TestPortalM7ClientFixture` 是显式 build-tag 的人工客户端服务，不属于正常测试。`protocol.jsonl` 只记时间、RPC ID、method、工具名、HTTP 状态、字节数和耗时，不记 Authorization 或正文。

Codex 使用真实 `exec`，只允许指令中的 KnowHub 只读工具，禁止 shell/编辑及替代数据路径。配置使用环境变量 Bearer、`required=true` 和对本合成只读服务的调用批准策略；服务端授权不变。初次仅初始化后客户端取消工具的日志保留，没有将 exit 0 误算成功。配置依据：[OpenAI 官方 MCP 文档](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)。

## 实际结果

| 客户端 | Initialize / tools/list | 实际 tools/call | 结论 |
| --- | --- | --- | --- |
| Codex CLI 0.146.0 | PASS | 公共/私人联合检索、全部五工具、直接敏感 ID/未授权 KB 拒绝、另一员工隔离、Portal 撤回、Key 撤销 401 | 对这些具体合成场景 PASS；不是全部 16 场景通过 |
| WorkMate 2.4.0 | PASS：已安装桌面程序自身后端返回五工具 | NOT_TESTED | **PARTIAL/HOLD**，不能标记完整客户端通过 |
| Claude Code 2.1.168 | 未完成，客户端返回 `Not logged in` | 未执行 | **BLOCKED_CLIENT_AUTH**，没有复用/导出其他账户凭据替代登录 |

WorkMate 通过 Playwright 启动已安装程序、独立 userData、关闭自动更新，使用真实桌面 renderer 调用其自身 `/api/mcp/test-connection`；这是产品实际 MCP transport，非 SDK 单元测试。没有完成 UI 导入配置、聊天 Agent 选择与工具执行，也没有把直接 HTTP 手工请求宣称为 WorkMate 工具调用。独立测试配置没有外部模型授权，尚不能覆盖其完整业务流程。

## Codex 具体协议证据

`clients/codex-positive.log` 中实际工具结果包括 tenant 1 的 `public-two`、tenant 99 的 `public-kb` 和 A 独有 `private-home`；搜索报告 3 KB、0 failures。`grep_chunks`、`list_documents`、`read_document` 成功。指定 `sensitive` 或 `private-kb` 返回 `resource denied or unavailable`。

`clients/codex-other.log` 使用 C 自己的真实 user_mcp，只有两个公共 KB；读取 `private-home-doc` 被拒绝。

`clients/codex-withdrawn.log` 撤回 tenant 99 后列表和检索不含该空间；直接 `public-doc` 被拒绝，而 `private-home-doc` 仍可读取。

Key 撤销第一次只证明了夹具退出导致 502，保持 FAIL 原记录；修复为只撤销一次，不修改任何服务端鉴权断言。最终真实 HTTP 401，独立 Key 仍初始化成功；Codex 约 4.65 秒以明确鉴权错误退出（预期 exit 1）。控制标志发出后先轮询到 200，随后为 401；标志写入不等于事务提交，日志未单独记录精确提交时刻，不能据此量化提交后的撤销传播延迟。完整时序、候选和夹具退出码列于 [51](51_SEVENTH_BATCH_TEST_RESULTS.md)。

原始证据：[主客户端协议事件](evidence/batch7/client-protocol-main.jsonl)、[Codex 正向](evidence/batch7/clients/codex-positive.log)、[另一员工](evidence/batch7/clients/codex-other.log)、[Portal 撤回](evidence/batch7/clients/codex-withdrawn.log)、[WorkMate](evidence/batch7/clients/workmate-result.json)、[Claude Code](evidence/batch7/clients/claude.log)。候选 SHA 和 argv 位于对应 result.json，不能用 Git HEAD 代替。

## 第七批 16 场景范围

| 场景 | 本批实测/剩余 |
| --- | --- |
| 1 创建 Key；2 Initialize；3 五工具 | Go 现有 service 创建；Codex 与 WorkMate真实初始化/list；UI端到端创建仍未验收 |
| 4 两个公共空间；5 私人 KB；6 另一员工 | Codex 实际调用通过 |
| 7 指定未授权 KB | Codex 实际拒绝 |
| 8 Portal 撤回 | Codex 实际拒绝，私人独立保留 |
| 9 Key 撤销 | 最终实际 401 + Codex 有界错误通过；第一次 502 保留 FAIL |
| 10 中文问英文 | **NOT_TESTED**：1024 维 mock 不能代表真实跨语言模型质量 |
| 11 同名 KB 来源 | 原 owner/KB/Document 绑定测试通过；真实客户端同名场景尚未覆盖 |
| 12 部分失败 | Go 实际 driver/HTTP 合成测试有结果；真实客户端故障呈现尚未覆盖 |
| 13 大 KB | 真实 PG/模型注册组件规模测试，尚未通过三客户端大库联调 |
| 14 文件/受限衍生 | 五工具不暴露 Wiki/文件；真实历史匿名链接收回仍 HOLD |
| 15 并发身份 | 实际 HTTP/检索并发及 A/C 客户端分别验证；三客户端全并发未验收 |
| 16 旧 Python/团队 MCP | 新候选相关回归；未改生产进程，不能称生产客户端回归 |

完整企业 MVP 客户端门禁仍 HOLD。所有失败、未登录和未完成项独立于 Go/SDK PASS 保留。

## 停止与回滚

结束时仅停止本批临时服务和转发，保留源快照、隔离数据库及日志，不删除历史数据。生产 Go 入口仍 disabled，生产 Python MCP 和团队 MCP 不切换。
