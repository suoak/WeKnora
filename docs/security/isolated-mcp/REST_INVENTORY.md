# P0 文件与内容写入接口清单

范围：当前工作区 Go HTTP 路由和 handler 静态检查；生产版本待比对。除特别注明外路径前缀为 `/api/v1`，`:id` 等为参数。**隔离区 8443 对本表全部路径默认拒绝，包括只读文件路径。** 现有鉴权仅用于描述现状，不作为隔离入口策略。

## 直接 multipart 接收点（非测试 handler 中 FormFile/MultipartForm 全量检索）

| 方法、路径 | handler | 当前保护/用途 |
|---|---|---|
| POST /knowledge-bases/:id/knowledge/file | KnowledgeHandler.CreateKnowledgeFromFile | ingest/full-access + KB 所有者或 Admin + KB write；限体后解析文件；可用于普通文档/FAQ 文件 |
| POST /sessions/:session_id/attachments | session.UploadTemporaryDocument | chat/full-access + 会话归属；附件写入，不能因为不是知识库文件而放过 |
| POST /skills/catalog | SkillHandler.RegisterCatalog | full-access + Admin；ZIP 或 JSON source |
| POST /sandbox-configs/:id/skills | SandboxSkillHandler.Upload | full-access + Admin；技能包上传及安装流程 |
| POST /models/:id/debug | ModelHandler.DebugModel | models 路由权限 + Admin；可选 file、多模态调试，可转发到模型服务 |
| POST /initialization/multimodal/test | InitializationHandler.TestMultimodalFunction | manage_models/full-access + Admin；image 字段 |

依据：`internal/handler/knowledge.go`、`handler/session/temporary_document.go`、`handler/skill_catalog.go`、`handler/sandbox_skill.go`、`handler/model.go`、`handler/initialization.go`。`handler/upload_limit.go` 是限体公共辅助函数；`*_test.go` 中 FormFile 不是额外生产接口。未仅按扩展名判断安全性。

## JSON、URL、编辑、任务及间接写入

| 方法、路径/族 | 风险 |
|---|---|
| POST /knowledge-bases/:id/knowledge/url | 下载 URL 文件/网页；SSRF 防护不等于禁止跨域传文件 |
| POST /knowledge-bases/:id/knowledge/manual | 手工文本/Markdown 创建，可编码附件 |
| PUT /knowledge/manual/:id、PUT /knowledge/:id | 手工正文和知识信息更新 |
| PUT /knowledge/image/:id/:chunk_id | 图片信息/Chunk 内容关联写入 |
| POST /knowledge/move、/batch-delete、/batch-reparse；PUT /knowledge/tags | 批量写操作、任务和元数据载荷 |
| POST /knowledge/:id/reparse、/:id/cancel-parse、/:id/regenerate-summary、/knowledge/folder；DELETE /knowledge/:id | 文档任务、目录移动、删除 |
| PUT /knowledge-bases/:id/knowledge/folders；DELETE /knowledge-bases/:id/knowledge | 文件夹改名、清库 |
| PUT /chunks/:knowledge_id/:id；POST /chunks/:knowledge_id/:id/revert | Chunk 正文及回滚写入 |
| DELETE /chunks/:knowledge_id[/:id] | Chunk 删除 |
| PUT/DELETE /chunks/by-id/:id/questions；POST .../questions/regenerate | 生成问题写入/任务 |
| POST /knowledge-bases/:id/faq/entries、/entry | FAQ 内容/批量导入 |
| PUT /knowledge-bases/:id/faq/entries/:entry_id、/entries/fields、/entries/tags | FAQ 正文与元数据写入 |
| POST /knowledge-bases/:id/faq/entries/:entry_id/similar-questions；DELETE .../entries | FAQ 变更 |
| PUT /knowledge-bases/:id/faq/import/last-result/display | 导入状态写入 |
| POST /knowledgebase/:kb_id/wiki/pages、/folders | Wiki 正文/文件夹创建；注意 knowledgebase 为单数 |
| PUT /knowledgebase/:kb_id/wiki/pages/*slug、/move-page、/folders/:folder_id | Wiki 更新、移动 |
| DELETE /knowledgebase/:kb_id/wiki/pages/*slug、/folders/:folder_id | Wiki 删除 |
| POST /knowledgebase/:kb_id/wiki/revert、/rebuild-links、/auto-fix；PUT .../issues/:issue_id/status | 回滚、修复、异步派生写入 |
| POST /knowledge-bases/:id/feishu | 个人飞书 URL 导入；部分路由为 JWT 入口，也必须拒绝 |
| POST/PUT/DELETE /datasource[/:id]；POST /datasource/:id/sync、/pause、/resume | 数据源拉取/同步任务；还须拒绝验证凭据、资源解析、凭据更新等管理动作 |
| POST /knowledge-bases、/knowledge-bases/copy、/:id/duplicate；PUT/DELETE /knowledge-bases/:id | 库创建/复制/元数据修改/删除 |
| POST/PUT/DELETE /knowledge-bases/:id/tags 等 | 标签/描述也能携带文本 |
| POST /skills/catalog（JSON source）、/:id/install | 远程源/本地源导入及安装，禁止以 JSON 冒充安全查询 |
| 会话创建/更新/消息注入/问答、Agent、MCP 服务配置、沙箱 terminal/desktop/browser | 会持久化输入或间接调工具；全部不在隔离白名单 |

MCP `add_document` 同时覆盖 URL 与 manual；`update_document` 替换文本；`ask` 写 session/message 并执行 Agent。即使 tools/list 隐藏，也必须用 tools/call 构造上述名称做拒绝验收。

## 文件读取、代理与对象存储

| 路径/机制 | 现状与隔离决定 |
|---|---|
| GET /files?file_path=...（根路径，无 /api/v1） | 统一存储代理；不对隔离区开放，不允许客户端任意路径 |
| GET /api/v1/knowledge-bases/:id/files | KB 绑定授权读取；8443 不开放 |
| GET /api/v1/sessions/:id/messages/:message_id/files | 消息绑定资源代理；8443 不开放 |
| GET /api/v1/knowledge/:id/download；POST /api/v1/knowledge-bases/:id/knowledge/batch-download | 原始附件下载；本轮仅允许授权文本阅读，不开放附件通道 |
| GET /api/v1/files/presigned | 签名验证访问，不以普通登录鉴权；必须拒绝 |
| GET /api/v1/files/presigned-preview | 管理诊断；必须拒绝 |
| GET/HEAD /r/:token（根路径） | 短期 capability URL；必须拒绝，日志不能保留 token 路径 |
| 存储原生直链/匿名桶/签名 GET 或 PUT URL | 需实查桶策略和网络；隔离出口不能直达写入口，MCP 不返回上传签名/匿名资源授权 |
| 泛 REST/HTTP 代理、redirect、CONNECT、WebSocket | 当前 Go MCP 目录没有注册泛 HTTP 转发工具；禁止新增到隔离集，也不开放其他服务的代理族 |

静态索引记录 `internal/router` 非测试文件中匹配到的显式 GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS/Any/Group 调用、apiKeyRoute 字面量和 Handle 表达式，共 581 条、13 个源文件，保留 group 片段和来源行号；**不冒充展开后的运行时路由表**，变量路径或复杂构造可能需要人工展开。未把全部路由逐一证明安全：固定入口白名单保证本表以外新增 REST 也默认拒绝。P1 在实际测试构建中导出 gin Routes 并和生产版本核对，用于补足动态拼接和条件注册。
