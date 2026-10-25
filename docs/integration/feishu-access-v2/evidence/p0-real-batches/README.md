# P0 原始真实批次证据

所有业务 ID 均为 SHA-256 前 12 位。报告不含文档正文、文档链接、查询词、凭据或授权码。

- `before.json`：修复前的真实 test 批次、attempt、outbox、归档队列及实际进程身份。
- `staging-fix.json`：仅修复隔离卷根目录属主和权限的执行收据，没有重置任务或修改业务行。
- `after-bm25.json`：原始任务自动恢复后的 Knowledge、Chunk、1024 维向量、成功回执和 BM25 查询计数。
- `approved-metadata.json`：两条新增验收链接的真实个人授权元数据请求；Sheet、Bitable 均 HTTP 200 / code 0，不代表正文入库完成。
- `mcp-before.json`：原始真实文档的个人 Go MCP keyword 成功、semantic/hybrid 未命中，以及实际 Python MCP Hybrid 工具已命中的基线。Go 失败原因是内置模型绑定校验；部署后需要另外复测。

新目标为用户明确选择的 test001。该库没有私人标记；用户另行明确允许两篇 Sheet/Bitable 按该库现有范围共享。后端仍须记录资源版本批准并走原有安全入库流程，不因会话批准而直接插入知识、回执或审批数据。
