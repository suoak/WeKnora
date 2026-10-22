# 个人 MCP 页面文案与指定私人范围交付

## 变更范围

基于 main `3ef2e2fd441dcc98909e66a33fb03d56ac323aec`，在独立工作树 `E:/code/WeKnora-worktrees/knowhub-mcp-private-scope-20261022` 开发。原 V1、飞书修复及真实云端验收工作树和证据保留。

本次只修改个人 MCP 前端及相关测试，复用已存在的服务端固定范围与动态范围授权。未修改 Go 服务、Schema 3037、飞书同步状态机、生产开关或历史凭据。

| 要求 | 实现 |
| --- | --- |
| 保留导航及页面地址 | 仍为「MCP 接入」及 `/platform/mcp-access` |
| 隐去公开范围与工具数量说明 | 服务卡、空状态、创建对话框和 Key 标签不再显示 Portal/公共知识或八工具数量说明 |
| 默认范围 | 新建默认仍使用 `dynamic_all`，保留 `retrieve` + `enterprise_retrieve`，后台按现有规则合并动态公共范围和私人授权 |
| 指定范围 | 新建使用 `fixed` + `retrieve`，只保存实时 scope-options 中明确选择的私人 KB；不授予 `enterprise_retrieve` |
| 指定范围管理 | 可以显式修改所选私人 KB；保存不变更能力或范围模式；禁止空选择，不退化为全库 |
| 历史 Key | 不自动修改能力、范围模式、空间、KB 或有效期；已有固定企业能力凭据保留原语义 |
| Secret 与配置 | 保留本人加密 Secret 查看、轮换、撤销；Codex 使用环境变量引用；继续使用 `/api/v1/mcp/personal` |

指定范围不是文档 ACL。它只收窄到私人 KB；用户和 Key 的既有授权、资源有效性、来源 HOLD、撤权及只读校验仍由服务端执行。页面文字不公开说明公共知识，不代表后台默认授权规则发生变更。

## 对应源码

- `frontend/src/views/integrations/MCPAccessKeys.vue`：展示文案、默认/指定范围创建、指定范围修改、兼容旧 Key。
- `frontend/src/views/integrations/personalMcpPresentation.ts`：构造动态默认或精确私人范围 payload，拒绝空集、越权、过期共享引用；识别新版只读个人凭据展示。
- `frontend/src/views/integrations/personalMcpPresentation.test.ts`：覆盖能力分离、精确范围、无隐式扩权及页面文案。
- 既有 `internal/middleware/personal_mcp_auth.go`、`internal/application/access/portal_mcp.go`、个人 API Key service/handler 继续执行真实主体与资源范围限制，未改动。

## 候选与部署方式

运行源码 SHA256：`04ee87c89e7bccffffb6f4d4794f2a33dc1e2ae39af9b64ad7f7b10f1e7c14c7`。这是完整非 docs 文件清单及逐文件 SHA256 的排序 JSON 摘要，**不是 Git commit**。

完整源码包和逐文件清单位于本工作树 `evidence/private-scope/candidate.json` 指向的位置。冻结时包含当前 tracked 完整源码以及三份修改；后续交付文档和验证工具不计入运行源码指纹。上传后重新验证包 SHA256 和所有文件 SHA256。

仅更新打包服务器已有获准测试实例的 frontend 容器：`http://172.30.232.42/platform/mcp-access`。APP、Worker、PostgreSQL、Redis、Python MCP、共享网络及 Nginx 配置保持原部署；APP 继续使用已验收 `bf199466` 镜像。Go、依赖和迁移内容一致；新 worktree 的 CRLF/LF 字节差异，以及 main 已有的 Docker/PyPI 发布保护工作流差异，单独记入 `source-comparison.json`。本次不重新打包或应用迁移，不可描述为重新完成 V1 正式业务验收。

部署脚本保存原 Compose 到测试实例私有目录，再只替换 frontend 镜像。回退只需恢复上一 frontend 镜像；不回退 APP，不执行 down migration，不改变来源拒绝状态。

## 验证记录

原始命令、退出码、构建与浏览器结果保留在 `evidence/private-scope/`，最终结果见该目录 `retry-build-result.json`、`deploy-result.json`、`browser/result.json` 和 `github-ci-current.json`。

- Windows 聚焦测试：16 PASS、0 FAIL，exit 0。
- Windows `npm run type-check`：exit 0。首次新增测试的联合类型推断报错已修复，无测试断言删减。
- Linux `npm test`：1587 PASS、0 FAIL、1 项既有 SKIP，exit 0。首次因只读依赖缓存导致 Vite 临时目录不可写，2 FAIL；改用独立可写依赖副本后原样重跑通过，失败日志保留为 `frontend-full.log`。
- 真实既有 APP 指定范围试验：只列出所选私人 KB，私人正文允许，公共正文、其他私人正文、公共搜索和 Wiki 拒绝，空 selected 创建 HTTP 400，临时 Key 撤销 HTTP 200。见 `existing-server-selected-scope.json`。
- Linux 类型检查、构建、frontend 镜像构建：全部 exit 0。
- 实际入口部署：PASS，frontend 镜像 `sha256:047228ee1e8b2efd612bd456474414b43fe3c9a40b233a374a996bea7391e394`；APP、Python 容器 ID 保持不变，Schema `3037:false`。入口 index SHA256 `c53b7bc65ab5c89dc2bd2dc8c283e7c3d46a6f4e857f8380f94666a05c4d0410` 与冻结构建一致。
- 浏览器：2026-10-22 13:19:56–13:20:07 UTC，exit 0。默认 Key 实际发现两个公共空间和两个私人空间；指定 Key 只列所选 KB，允许其正文、拒绝公共和未选私人正文；修改指定范围即时生效；历史 Key 日期与范围逐项一致；390px 窄屏无横向溢出。两个临时 Key 已撤销，HTTP 200。
- 浏览器原始失败保留在 `browser/previous-*/`：首次全页文案断言误匹配历史 Key 名称中的 Portal，随后首次登录引导遮挡及 TDesign 原生 radio 被可见装饰层覆盖。修正脚本限定产品说明、正常关闭引导并点击可见标签；所有范围、正文、能力、历史 Key 和布局断言仍实际执行。
- 截图：`browser/desktop.png`、`create-default.png`、`create-selected.png`、`narrow.png`。均未展示明文 Secret。
- main 合并需等待 PR CI；最终状态以 GitHub PR 与本地 `github-merge.json` 为准。

浏览器试验使用合成本地测试员工，操作真实部署 SPA 和 APP；不代表真实 SID/OA、正式模型、真实飞书权限事件或生产知识覆盖验收。只撤销本轮测试新建 Key，不修改已有凭据。

## 发布边界

本次用户已明确授权部署打包服务器测试环境，并合入 main；生产没有部署授权。原有真实飞书权限撤销、Wiki 云端、正式模型及生产物理恢复等发布门禁单独保留，不因页面优化通过而自动解除。`Partial knowledge scope` 的来源 HOLD 安全拒绝不会被本次文案调整清除。
