# P1 最小源码改动清单

相对已核验 main `4b385fd11e2f617eee491781067b2f69cb8aa1be`。生产版本尚未取证；此清单供最小补丁评审，禁止将 main 全量差异直接发布。

| 文件 | 用途 |
|---|---|
| `internal/mcpserver/personal.go` | 复用既有 PersonalServer，在 Handler 与工具输出/错误处接入边界 |
| `internal/mcpserver/personal_protocol.go`（新） | 精确路径、方法、Origin、JSON媒体/1MiB/结构及固定8工具参数白名单 |
| `internal/mcpserver/personal_output.go`（新） | 复用已有文本净化，递归去除URL与绝对路径；固定工具错误/panic响应 |
| `internal/mcpserver/usage.go` | 个人入口不持久化任意客户端名称/版本 |
| `internal/middleware/logger.go` | 个人入口提前标记载荷隐私；只记录固定元数据，忽略客户端Request-ID |
| `internal/middleware/personal_mcp_auth.go` | 原Key与身份认证逻辑保留，传递隐私上下文 |
| `internal/middleware/error_handler.go`、`recovery.go` | 个人入口错误与panic不反射内部正文 |
| `internal/utils/auth_log.go` | 统一识别个人MCP路径用于日志与错误保护 |
| `internal/logger/payload_privacy.go`（新）、`logger.go` | 私有载荷日志禁用及上下文复制继承 |
| `internal/logger/gorm_privacy.go`（新）、`internal/container/container.go` | GORM私有载荷不格式化/持久化SQL；其他请求遵循原日志行为 |
| `internal/logger/llm_logger.go` | 私有载荷不写LLM调试文件 |
| `internal/models/embedding/langfuse_wrapper.go` | 私有查询不导出embedding追踪正文 |

未修改路由注册文件、个人 Key 生成/持久化方案、统一知识权限和检索服务、数据库迁移、生产 Nginx/Compose/DNS/防火墙/证书。共享基础组件仅对明确标记的个人入口改变日志行为。

测试与部署材料：

- 新增 `personal_protocol_test.go`、`personal_output_test.go`、`payload_privacy_test.go`、`personal_log_privacy_test.go`、`embedding/personal_privacy_test.go`，验证固定只读边界、参数/媒体/大小、输出脱敏、错误/panic及真实日志/追踪接收器。
- `personal_eight_test.go`、`personal_targets_test.go` 只补充正确 JSON-RPC/Content-Type，使现有断言经过新协议边界；没有降低权限断言。
- `internal/router/isolated_p1_deployed_test.go` 使用显式 `knowhub_isolated_p1` build tag，连接固定专用测试 DSN，初始化合成身份/知识/Key，并提供写 handler 调用计数对照。正常生产构建不包含该文件。
- `docs/security/isolated-mcp/p1_*.py` 和 Nginx 模板仅操作指定测试目录及带所有权标签容器。测试证书、Key、原始日志和运行配置留在受控测试环境，不提交 Git。

完整文件摘要、最终运行制品 SHA256 及验证结果位于 [证据目录](evidence/p1-20261111/)。新增测试与文档均不构成生产发布脚本。
