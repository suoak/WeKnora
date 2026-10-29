# MCP 接入页面视觉与交互改造

开发基线：`origin/main` 的 `962c7c888`，分支 `feat/mcp-access-experience-20261029`。原工作区 `E:/code/WeKnora` 的未提交改动保留；实际候选在独立 worktree 开发。

## 现有体系审查与方案

检查了首页 `PortalHome.vue`、空间知识库 `KnowledgeBaseList.vue` / `KnowledgeBase.vue`、`AgentList.vue`、`SystemAdminHome.vue`、`Settings.vue`、`platform/index.vue`、`menu.vue`、现有 `MCPAccessKeys.vue`，以及全局 `theme.css` 和 TDesign overrides。另参考上游 [WeKnora AgentList](https://github.com/Tencent/WeKnora/blob/main/frontend/src/views/agent/AgentList.vue)，上游参考不覆盖 KnowHub 本地 Token。

当前 KnowHub 使用系统字体、14px 基础字号、18px 页面标题、4px 间距网格、6/8/10/12px 圆角及全局绿色品牌变量。暗色模式由 `theme-mode` 和主题 Token 控制。平台路由容器为 flex 列、`min-height:0`、`overflow:hidden`，子页面需要自己提供可收缩滚动区域。原 MCP 页存在多层累积样式、独立色值、渐变及首屏地址；个人凭证列表会无限增长。

变更方案：

1. 首屏“连接 AI 工具”：Claude Code、Codex、GitHub Copilot（VS Code）、VS Code、WorkBuddy、Pi、其他客户端七个入口。桌面端 4+3，较窄屏幕三列/两列；五个客户端使用本地官方静态图标（来源见 assets/mcp-clients/README.md），其余沿用 TDesign 图标。
2. “我的个人 MCP Key”：紧凑凭证卡片，默认选择可用统一检索凭证，允许切换和管理。移除重复“获取连接配置”按钮，统一从上方客户端卡片接入。明确动态范围随本人授权更新、历史固定范围不自动扩展，以及当前空间不等于完整授权范围。创建沿用现有名称、有效期、默认/指定知识范围表单。历史客户端凭证不被当成统一个人 Key 自动升级。
3. “历史接入”：默认折叠，含全部凭证；按全部/已启用/已过期/已停用筛选，每页 5 条，保留详情、范围修改、轮换、停用、重新接入与兼容配置。
4. 配置使用 TDesign 弹窗；展示客户端、已有 Key、授权范围、Secret、配置、复制、步骤和协议检查。本人 Secret 继续走现有加密 Secret reveal API，历史 hash-only Secret 可填入原值；关闭或切换 Key 后清理明文与验证状态。
5. 不在普通员工入口推荐 Cursor、WorkMate；保留其历史标签、配置生成函数、后端枚举和凭证。历史编辑页显示原客户端，保留原 client_type。
6. 页面复用现有平台导航；内容区提供独立纵向滚动，窄屏按钮换行。弹窗复用 `cssViewportSize()` 适配应用全局缩放，限定宽高、滚动正文，保留关闭按钮。

## 配置依据与状态

| 客户端 | 配置与说明 | 本次真实客户端检索 |
| --- | --- | --- |
| Claude Code | 项目 `.mcp.json`、`mcpServers`、HTTP Bearer；[官方文档](https://code.claude.com/docs/en/mcp) | 待员工登录验收 |
| Codex | 复用已存在的 `personalCodexConfig()`，`~/.codex/config.toml`、`bearer_token_env_var=KNOWHUB_MCP_KEY`；[OpenAI 官方文档](https://developers.openai.com/codex/mcp) | 待员工登录验收 |
| Copilot / VS Code | `.vscode/mcp.json`、`servers`、密码输入变量；支持范围明确为 VS Code；[官方文档](https://code.visualstudio.com/docs/agents/reference/mcp-configuration) | 待员工登录验收 |
| WorkBuddy | `mcpServers`、`streamableHttp`；[官方文档](https://open.workbuddy.cn/docs/connector) | 待员工登录验收 |
| Pi | 官方 `earendil-works/pi` v1.4.0，包 `@earendil-works/pi-coding-agent@1.4.0`，Node ≥22.19.0；用户级 `~/.pi/agent/mcp.json`、URL / headers / description；`pi mcp list` 与 `/mcp`；[固定版本官方文档](https://github.com/earendil-works/pi/blob/v1.4.0/packages/coding-agent/docs/mcp.md) | 待员工登录验收 |
| 其他客户端 | 通用 HTTP MCP JSON，要求按目标客户端文档调整 | 参考 |

Pi 官方 v1.4.0 原生支持 stdio 与 Streamable HTTP，不需要安装第三方 MCP 扩展。已在本机以 `--ignore-scripts` 安装固定版本并实际执行版本及 MCP CLI 帮助。包注册信息与官方仓库一致；此检查不等于 Bearer 联调或实际知识检索。

统一个人配置继续使用 `/api/v1/mcp/personal`，不添加 `X-Tenant-ID`。历史配置保留原地址与空间请求头。没有修改 Go MCP、Portal 知识规则、私人权限、Python MCP、服务架构或数据库。

页面区分：凭证有效、协议鉴权与只读工具发现通过、客户端实际知识检索通过。所有新客户端模板标为参考/待验证。协议按钮不宣称真实 AI 客户端已连接。

## 验证与发布门禁

本地记录：相关单元测试 47/47 通过；类型检查通过；Edge Playwright 两项通过（七个客户端配置与实际剪贴板内容、14 条历史记录分页/筛选、加密 Secret reveal 与关闭清理、动态范围创建、协议状态、响应式和主题）。全量 `npm test` 首轮 1593 通过、1 跳过、3 失败：两个 shell 路径问题在配置 Git bash / WEKNORA_TEST_POSIX_SHELL 后分别复测通过；代理 dev 测试因本机 5173 端口被其他进程绑定而 EACCES，保留失败记录并等待 Linux CI，未修改产品代码来绕过。Windows 剪贴板 CRLF 在断言中规范化为 LF，配置内容保持原值。

候选检查记录将在测试环境部署后补齐。浏览器截图来自实际 Edge 渲染、合成 API 数据；不是设计效果图，也不代表真实账号权限验收。浏览器用例覆盖七入口、格式差异、Secret 清理、协议状态、创建载荷、历史筛选分页、亮暗主题、窄屏以及应用 CSS 缩放 100% / 125% / 150%。CSS 缩放与缩小有效视口不代替用户在目标浏览器中的工具栏缩放验收。

2026-10-29 用户确认使用 `172.30.232.42:80/8080` 的现有 KnowHub v2 测试站，并由用户登录验证。真实员工账号、权限隔离、客户端 Bearer 与知识检索由该测试站验收完成后记录；此前不合并 main、不发布生产。测试部署仅变更前端，保留回滚镜像与配置，APP 和数据服务保持原状。
