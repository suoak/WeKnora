# MCP 接入页面视觉与交互改造

开发基线：`origin/main` 的 `962c7c888`，分支 `feat/mcp-access-experience-20261029`。原工作区 `E:/code/WeKnora` 的未提交改动保留；实际候选在独立 worktree 开发。

## 现有体系审查与方案

检查了首页 `PortalHome.vue`、空间知识库 `KnowledgeBaseList.vue` / `KnowledgeBase.vue`、`AgentList.vue`、`SystemAdminHome.vue`、`Settings.vue`、`platform/index.vue`、`menu.vue`、现有 `MCPAccessKeys.vue`，以及全局 `theme.css` 和 TDesign overrides。另参考上游 [WeKnora AgentList](https://github.com/Tencent/WeKnora/blob/main/frontend/src/views/agent/AgentList.vue)，上游参考不覆盖 KnowHub 本地 Token。

当前 KnowHub 使用系统字体、14px 基础字号、18px 页面标题、4px 间距网格、6/8/10/12px 圆角及全局绿色品牌变量。暗色模式由 `theme-mode` 和主题 Token 控制。平台路由容器为 flex 列、`min-height:0`、`overflow:hidden`，子页面需要自己提供可收缩滚动区域。原 MCP 页存在多层累积样式、独立色值、渐变及首屏地址；个人凭证列表会无限增长。

变更方案：

1. 首屏“我的个人 MCP Key”：每页最多 5 条紧凑卡片，有效凭证优先。5 把及以内直接展示全部，仅保留标题数量/状态摘要，隐藏搜索、筛选、分页；超过 5 把时可搜索，多种真实状态时使用原生轻量 TDesign Tabs，隐藏零数量状态。每条独立展示名称、状态、范围、有效期、最近使用、管理和连接按钮。移除独立客户端区、全局和弹窗中的 Key 下拉框。
2. 有效 Key 的“连接 AI 工具”打开绑定该 Key 的客户端选择弹窗：Claude Code、Codex、GitHub Copilot（VS Code）、VS Code、WorkBuddy、Pi、其他。选择后直接生成配置，不再选 Key。独立 connectionKey 与管理详情分离；延迟 reveal 响应受请求版本和 Key ID 双重校验。所有客户端统一“复制完整配置（含密钥）”，点击后自动读取当前 Key 的真实 Secret。预览默认遮蔽；关闭及读取失败清理明文。Claude Code 使用用户级 ~/.claude.json；Codex 使用用户级 ~/.codex/config.toml 的 http_headers；Copilot/VS Code 指引 MCP: Open User Configuration，使用用户级 servers / Authorization，均无需环境变量、安全输入占位或另贴密钥。无可恢复 Secret 的 Key 明确标为占位模板而非完整配置。复制成功且内容含真实密钥时提示仅保存可信个人配置、禁止提交仓库。服务地址缺失、过期、停用时禁止直接连接或复制。
3. 创建沿用名称、有效期、默认/指定范围表单，名称示例“个人知识检索”；成功后直接进入新 Key 的客户端选择。同一把 Key 可切换多个客户端。默认范围随本人授权更新，历史固定范围不自动扩展；当前空间不等于完整授权范围。
4. “历史接入”仅在原始历史集合非空时渲染，默认折叠，与个人列表互斥，不重复计数。多状态时使用轻量 Tabs；个人/历史分页分别按当前结果超过 5 条才显示，搜索/筛选/数据变化时重置或修正页码。详情、范围修改、轮换、停用、重新接入与兼容模板保留。hash-only 历史凭证仍支持填入原值；个人 hash-only Key 可复制占位模板或在管理中轮换，不将未验证的手工 Secret 自动绑定到个人 Key。
5. 不在普通员工入口推荐 Cursor、WorkMate；保留其历史标签、配置生成函数、后端枚举和凭证。历史编辑页显示原客户端，保留原 client_type。
6. 页面复用现有平台导航；内容区提供独立纵向滚动，窄屏按钮换行。弹窗复用 `cssViewportSize()` 适配应用全局缩放，限定宽高、滚动正文，保留关闭按钮。

## 配置依据与状态

| 客户端 | 配置与说明 | 本次真实客户端检索 |
| --- | --- | --- |
| Claude Code | 用户级 `~/.claude.json`、`mcpServers`、HTTP Bearer；[官方文档](https://code.claude.com/docs/en/mcp) | 原生 CLI 协议通过；完整检索待用户客户端验收 |
| Codex | 用户级 `~/.codex/config.toml`、`http_headers.Authorization` 完整配置；其他页面原有环境变量 helper 保留；[OpenAI 官方文档](https://developers.openai.com/codex/mcp) | 原生 CLI 配置解析通过 |
| Copilot / VS Code | `MCP: Open User Configuration`，用户级 `servers`、直接 Bearer；支持范围明确为 VS Code；[官方文档](https://code.visualstudio.com/docs/agents/reference/mcp-configuration) | 浏览器绑定 Key 的完整剪贴板内容通过 |
| WorkBuddy | `mcpServers`、`streamableHttp`；[官方文档](https://open.workbuddy.cn/docs/connector) | 待员工登录验收 |
| Pi | 官方 `earendil-works/pi` v1.4.0，包 `@earendil-works/pi-coding-agent@1.4.0`，Node ≥22.19.0；用户级 `~/.pi/agent/mcp.json`、URL / headers / description；`pi mcp list` 与 `/mcp`；[固定版本官方文档](https://github.com/earendil-works/pi/blob/v1.4.0/packages/coding-agent/docs/mcp.md) | 待员工登录验收 |
| 其他客户端 | 通用 HTTP MCP JSON，要求按目标客户端文档调整 | 参考 |

Pi 官方 v1.4.0 原生支持 stdio 与 Streamable HTTP，不需要安装第三方 MCP 扩展。已在本机以 `--ignore-scripts` 安装固定版本并实际执行版本及 MCP CLI 帮助。包注册信息与官方仓库一致；此检查不等于 Bearer 联调或实际知识检索。

统一个人配置继续使用 `/api/v1/mcp/personal`，不添加 `X-Tenant-ID`。历史配置保留原地址与空间请求头。没有修改 Go MCP、Portal 知识规则、私人权限、Python MCP、服务架构或数据库。

页面区分：凭证有效、协议鉴权与只读工具发现通过、客户端实际知识检索通过。所有新客户端模板标为参考/待验证。协议按钮不宣称真实 AI 客户端已连接。

## 验证与发布门禁

本轮补充：列表规则覆盖 0、1、2、5、6 把 Key、混合状态、零历史、19 条历史、搜索空结果及筛选/刷新导致页数减少。相关单元测试 52 项通过。真实 Claude Code CLI 与 Pi v1.4.0 使用隔离配置连接本地合成 MCP 服务，均完成 initialize / tools/list，服务端核对 Bearer 正确；Codex CLI 验证静态 http_headers 完整配置可解析且不需要环境变量。这些是实际客户端的配置/协议测试，不替代员工客户端端到端验收。复制测试覆盖不可恢复 Secret、读取失败、HTTP 兼容、所有客户端完整配置含当前 Key 的真实 Secret。

地址检查：测试站 MCP_PUBLIC_URL 已持久化配置，用户刷新后确认告警消失。通过专用生产 SSH 连接核对，线上地址已正确配置为 http://10.51.134.114/api/v1/mcp/personal，后端健康；测试机上停用的旧容器不作为生产依据。用户明确要求“只有 HTTP 就行，先不用管 HTTPS”，本轮据此继续现有 HTTP，不引入 HTTPS 门禁，也不把测试地址硬编码到生产。

最终 Key 中心交互的补充验证：相关单元测试 47/47 通过；Edge 三项用例全部通过。夹具包含 8 把个人 Key（动态默认、指定、历史固定、有效、过期、停用）和独立的 19 条旧版客户端凭证。覆盖个人搜索/筛选/分页、有效优先、历史去重/分页/过期筛选、七客户端只绑定发起的 Key、真实剪贴板、Secret 默认不显示且不自动读取、延迟 A Key reveal 不覆盖 B Key、新建 Key 直接进入该 Key 的客户端选择、关闭后无持久化明文、缺失服务地址禁用连接、协议状态与客户端检索状态区分，以及响应式/明暗主题/应用缩放。真实员工权限隔离和客户端端到端检索仍按用户要求由其登录测试站验收，未据合成协议用例标记任何客户端已连接。

本地记录：相关单元测试 47/47 通过；类型检查通过；Edge Playwright 两项通过（七个客户端配置与实际剪贴板内容、14 条历史记录分页/筛选、加密 Secret reveal 与关闭清理、动态范围创建、协议状态、响应式和主题）。全量 `npm test` 首轮 1593 通过、1 跳过、3 失败：两个 shell 路径问题在配置 Git bash / WEKNORA_TEST_POSIX_SHELL 后分别复测通过；代理 dev 测试因本机 5173 端口被其他进程绑定而 EACCES，保留失败记录并等待 Linux CI，未修改产品代码来绕过。Windows 剪贴板 CRLF 在断言中规范化为 LF，配置内容保持原值。

候选检查记录将在测试环境部署后补齐。浏览器截图来自实际 Edge 渲染、合成 API 数据；不是设计效果图，也不代表真实账号权限验收。浏览器用例覆盖七入口、格式差异、Secret 清理、协议状态、创建载荷、历史筛选分页、亮暗主题、窄屏以及应用 CSS 缩放 100% / 125% / 150%。CSS 缩放与缩小有效视口不代替用户在目标浏览器中的工具栏缩放验收。

2026-10-29 用户确认使用 `172.30.232.42:80/8080` 的现有 KnowHub v2 测试站，并由用户登录验证。真实员工账号、权限隔离、客户端 Bearer 与知识检索由该测试站验收完成后记录；此前不合并 main、不发布生产。测试部署仅变更前端，保留回滚镜像与配置，APP 和数据服务保持原状。
