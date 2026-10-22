# 第七批业务续作：个人 MCP 与正常发布操作闭环

继续专注 KnowHub；WorkMate 排查和修改停止。本次没有修改 AionUi/AionCore 或已安装 WorkMate，没有调用 WorkMate 会话。保留此前交付和全部原始证据。

## 已实现源码

`frontend/src/views/integrations/MCPAccessKeys.vue` 的个人入口改为单个创建对话框：填写名称、选择有效期，直接创建个人 Key 并展示连接配置。公共 Portal 范围动态解析，不在创建表单中选择空间或逐 KB 配置。私有范围使用既有服务端 scope-options 中当前有效的个人授权空间，默认包含全部这些空间的动态 KB 范围。

根据用户最终要求，新个人 Key **默认永不过期**。界面仍允许主动选择 90 天或指定日期；选择 90 天会显式提交未来到期时间，避免被新默认值解释为无限期。服务器 `handler.userMCPExpiry` 同样以未指定时间为无限期。原 Key 不回填、不续期；PATCH 未指定有效期、轮换未指定有效期继续保留原日期；过期日期及互斥参数仍拒绝。只改变新建 user_mcp 的默认值，不改变 Tenant/Platform 凭据。

个人创建固定使用 `retrieve + enterprise_retrieve`，不带管理或写入能力。后端仍核实真实用户和私有范围；服务端鉴权失败不会改用管理员身份。已有高级接入编辑、撤销、轮换功能保留。

`personalMcpConfig.ts` 新增 Codex CLI TOML 模板，沿用此前实际 CLI 测试使用的 `bearer_token_env_var` 契约。模板只引用 `KNOWHUB_MCP_KEY` 环境变量，不嵌入 Secret。页面可以在 Generic MCP JSON 和 Codex CLI 配置间切换，仍只连接 `/api/v1/mcp/personal`。

## 正常业务写入路径验证

`router.TestPortalM7PublishedDynamicRealPostgres` 现使用实际 JWT、RBAC、中间件、Portal handler/service 和 KB handler/service：

1. 普通 Viewer 不能创建 KB 或发布 Portal。
2. Owner 通过既有 `POST /api/v1/knowledge-bases` 创建新 KB，服务器忽略请求中伪造的 tenant_id，绑定真实当前空间和 creator_id。
3. 普通 SystemAdmin 通过已有 Portal publish 路由发布空间，不增加 MCP 批准步骤。
4. 原个人 Key 自动发现新 Published 空间及其 KB；后续同空间再创建 KB 也直接发现。
5. 通过原 Portal unpublish 路由撤销公共检索，独立私有权限保留。
6. 新知识入库、索引、移出空间和删除的边界断言继续保留；本测试的解析/索引就绪仍为受控夹具，不扩大为实际异步解析 Worker 的验收。

`router.TestPortalM7PersonalKeyDefaultExpiryHTTP` 通过实际个人 Key HTTP 接口证明默认无限期、显式日期、PATCH/轮换保留日期，以及旧 Key 日期和能力不变。不是仅测试表单变量。

## 实际测试

候选 `31514a0c30a37f63…` 的 `business` 阶段全部退出 0：handler/router/mcpserver 完整受影响包、个人有效期 HTTP 和 expiry 单元测试的 race、受影响包 vet、真实 PostgreSQL 动态业务测试。原始命令和退出码见 `evidence/batch7/business/linux/`，逐文件候选见 `business/candidates/`。

前端新增加 Codex 配置契约测试，共 41 项，通过；最终代码的类型检查和构建均退出 0，见 `business/local/` 最新结果。浏览器端使用实际 Vue 页面、JWT、Key API 和 Go MCP，模拟范围仅限合成账号/文档、受控模型以及测试网络。

候选 `b979d1ab41fca8b8…` 的业务页面验收已实际通过：一个对话框创建默认无限期 Key；同时获得两个 Published 公共 KB 及两个不同私人空间（tenant 1、7）的 KB；五个只读工具实际调用；未授权文档、写工具拒绝；取消发布只移除对应公共知识；撤销 Key 返回 401。Codex TOML 模板内容和不含 Secret 也已通过 UI 核对。原始记录为 `business/browser/result-attempt2.json` 和对应服务端协议日志。测试 Key 已撤销。

首轮浏览器测试因 TDesign 隐藏的原生 radio input 无法直接 `.check()` 而失败，保留 `browser/result.json`。改为点击实际可见的 Codex 标签后全部原断言通过，没有减少或放宽授权断言；首轮遗留的合成 Key 也已通过真实 DELETE API 撤销。

逐文件 SHA256、新候选指纹、实际 Go 命令退出码、前端输入指纹及服务端协议摘要统一保存在 [最终验证记录](evidence/batch7/business/final-validation.json)。执行 `python tools/verify_batch7_business.py` 对当前 Go 输入、当前前端输入、实际浏览器结果与协议日志逐项比对，通过后才生成该记录。早先冻结报告仍对应其自己的候选，不被此记录覆盖。

## 生产影响和回滚

生产个人 MCP 仍默认关闭，没有发布、迁移、批量权限修改、历史知识公开、生产 Key 改期或轮换。没有 commit/push/merge/deploy。回滚本次创建默认值和 UI 应按新增清单逐文件回退；已经主动创建的无限期测试 Key 会在测试结束时撤销，不删除先前证据或数据库卷。

此前 [57 交付](57_SEVENTH_BATCH_DYNAMIC_DELIVERY.md) 代表其冻结候选；本次新源码不能复用它的最终 SHA256。完整 M3、真实模型质量、生产网络及其他此前 HOLD 不因本次页面和发布链验证而变成 PASS。
