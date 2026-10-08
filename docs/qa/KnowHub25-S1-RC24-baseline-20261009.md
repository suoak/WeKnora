# KnowHub25 S1：RC24 稳定基线提交验收

S1 本地前端 Gate **PASS**。仅整理隔离 QA 入口与镜像上下文；不单独部署 RC24-rc2。KnowHub25 全栈集成和打包机 Gate 尚未完成。

## 正式入口及隔离边界

在 frontend 运行 `npm run test:agent-editor`。RC24_RUN_DIR 可指定工作区外的新证据目录，每次使用独立目录，不覆盖旧证据。runner 使用独占锁、串行阶段和阶段前后源码哈希，不启动 Vite watcher。

Node22.22.1、npm10.9.4、Vite7.3.6、Vue3.5.34、TypeScript6.0.3、Playwright1.64.0、Windows headed Edge154.0.4258.62；workers=1、原动画、标准关闭断言。未 force click、扩大关闭 timeout、关闭动画或强制卸载产品组件。

受控 QA 构建要求 RC24_QA_BUILD=1 与 rc24-qa mode，才生成组件测试 HTML。qa-component.ts 要求回环地址和夹具假凭据，不注入登录信息。Playwright 以隔离数据截获 API；静态服务仅绑定127.0.0.1，未截获 API/MCP/files 返回503，无真实后端代理。

旧 component.html/component.ts、免登录手动入口和诊断服务保留在工作区，排除提交。正式 Vite 配置未修改，仍仅 main/embed。Docker 上下文排除 tests、test-results、playwright-report、单元/组件测试文件；运行镜像只复制 dist。正式 dist 无 editorHarness、rc24Export、假凭据、诊断入口。未实际构建镜像，不把静态审查当成 Docker 实验。

## 本轮重新执行结果

| Gate | 执行和结果 | gate-v2 日志 |
| --- | --- | --- |
| 全部单元/组件逻辑、Vue AST、i18n runtime | node --import tsx --test，253文件；1562 PASS、0 FAIL、1可选 SKIP | unit.log |
| Vue/TS | vue-tsc --build PASS | type-check.log |
| 隔离 QA 构建 | vite build --mode rc24-qa --config tests/agent-editor/frozen-build.config.ts PASS；不是最终 Release Build | qa-build.log |
| 完整 Edge | final-gate.config.ts，39/39，0 flaky/skipped | edge-39.log、edge-39/results.json |
| 正式生产构建 | VITE_IS_DOCKER=true vite build --outDir 外部 release-dist PASS | release-build.log |
| 正式产物 Edge | close-cycle/navigation/editor/knowledge-gate，25/25；独立 harness 用例在39项中验收 | edge-25.log、edge-25/results.json |
| dist | 236文件、867引用、0缺失；五语言、无测试入口 PASS | release-dist-check.json |
| 冻结/diff | 各阶段源码无变化，git diff --check PASS | *-source-check.json |

覆盖创建和编辑全部菜单，模型/工具/MCP/技能正常、空、失败、权限状态；四/六导航、不同模式、无知识库、旧/停用模型保留；文档计数/筛选/状态/只读。保存仅一次 PUT，失败保留输入，显式重试成功；22项模型、ReRank、工具、MCP、Skills、提示词和检索配置保留。同页真实卡片菜单关闭重开与独立新实例持久化分开验证。以上均使用隔离 API，不等于生产接口验收。

既有可选 BrowserTaskPreview 人工交接 SKIP 不属于 RC24 必须 Gate。文档接口失败仍渲染既有空状态，错误提示/重试 UX 的历史 P2 限制未修复，不宣称其体验完善。

五语言每语言7084 key，累计233 key修复记录保留；六个排序 key、28个可达历史候选全部修复，历史 baseline=[]。共同缺失和新增缺项使审计失败。SettingsModalShell 未修改，RAF归因 UNKNOWN；QA关闭节点移除耗时四导航3221.0ms、六导航2251.2ms，标准断言通过，不意味着性能根因已解决。

## 冻结证据及备份

本轮 `E:/AIData/CodexTemp/knowhub25-S1-20261009-004951`，最终结果 `gate-v2/`。首次 runner 就绪探测误用 `/`，被静态服务器路径保护拒绝，尚未启动浏览器回归；失败 gate/ 保留。修正到 /index.html 后完整重跑，无产品或断言修改。

`gate-v2/frozen-source.json` SHA-256：`63a0f729ca2763675986fc55c0b0975dbbf255bda20e0b03441e5583b7ef77fb`，1028文件。所有 frontend/src 与已验收 v4 哈希一致；正式 dist 236文件也与 v4 完全一致。

本轮增加360文件保护副本及 manifest-before.json、git status/branch/HEAD/diff/remotes。历史39文件备份与357文件保护快照哈希无变化。全部旧文件、报告、原始证据和推广资料保留，不 clean/reset/覆盖历史备份。

白名单敏感格式扫描未发现真实私钥/token；local-fixture-token 是合成夹具值，仅用于隔离 QA，该扫描不是任意秘密的完备证明。package-lock 仅增加 @playwright/test、playwright、playwright-core 三个 dev-only 项，无运行依赖升级或镜像源/Dockerfile修改。

Node24打包机、Linux/Go1.26.7/AnyDoc、全栈API、隔离数据库 migration Gate 后续独立执行。未 push、main merge、tag、构建/发布镜像或操作生产。提交 SHA 与 manifest 在后续 A1 报告和外部提交记录中固定。

## 显式提交白名单

产品25文件；回归20文件加本报告。所有原始 trace/DOM/截图/profile、旧诊断配置、其它历史报告留作未跟踪证据，绝不整体提交目录。

### product

- `.gitignore`
- `frontend/src/components/AgentEmbedChannelPanel.vue`
- `frontend/src/components/ModelSelector.vue`
- `frontend/src/composables/useTenantModelReadiness.ts`
- `frontend/src/i18n/localeKeyAudit.test.ts`
- `frontend/src/i18n/localeKeyAudit.ts`
- `frontend/src/stores/chatResources.ts`
- `frontend/src/stores/editorResources.ts`
- `frontend/src/views/agent/AgentEditorModal.test.mjs`
- `frontend/src/views/agent/AgentEditorModal.vue`
- `frontend/src/views/agent/AgentEditorPanel.vue`
- `frontend/src/views/platform/index.vue`
- `frontend/src/i18n/locales/en-US.ts`
- `frontend/src/i18n/locales/ja-JP.ts`
- `frontend/src/i18n/locales/ko-KR.ts`
- `frontend/src/i18n/locales/ru-RU.ts`
- `frontend/src/i18n/locales/zh-CN.ts`
- `frontend/tests/quality/applyRc24Translations.ts`
- `frontend/tests/quality/i18nHistoricalBaseline.json`
- `frontend/tests/quality/i18nStaticAudit.test.ts`
- `frontend/tests/quality/i18nStaticAudit.ts`
- `frontend/tests/quality/promptNavigation.test.mjs`
- `frontend/tests/quality/rc24-translations.json`
- `frontend/tests/quality/vueVisibilityAudit.mjs`
- `frontend/tests/quality/vueVisibilityAudit.test.mjs`

### tests

- `frontend/.dockerignore`
- `frontend/package.json`
- `frontend/package-lock.json`
- `frontend/src/views/chat/components/SandboxTerminal.test.mjs`
- `frontend/src/views/integrations/cliIntegration.test.ts`
- `frontend/tests/agent-editor/close-cycle.spec.ts`
- `frontend/tests/agent-editor/component.spec.ts`
- `frontend/tests/agent-editor/editor.spec.ts`
- `frontend/tests/agent-editor/final-gate.config.ts`
- `frontend/tests/agent-editor/fixtures.ts`
- `frontend/tests/agent-editor/frozen-build.config.ts`
- `frontend/tests/agent-editor/knowledge-gate.spec.ts`
- `frontend/tests/agent-editor/navigation.spec.ts`
- `frontend/tests/agent-editor/save-business.spec.ts`
- `frontend/tests/agent-editor/save-data.ts`
- `frontend/tests/agent-editor/stable-server.mjs`
- `frontend/tests/agent-editor/qa-component.ts`
- `frontend/tests/agent-editor/qa-isolation.test.mjs`
- `frontend/tests/agent-editor/run-gate.mjs`
- `frontend/tests/agent-editor/check-dist.mjs`
