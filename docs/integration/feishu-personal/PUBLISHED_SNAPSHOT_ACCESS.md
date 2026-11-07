# 个人飞书已发布知识的独立读取

## 产品边界

成功发布的副本由 KnowHub 的空间、私人知识库和 KB 授权管理。源端 OAuth 只授权获取新内容和发布新版本。源端断开、过期、删除或权限撤销不会撤回已经发布的本地资产。撤回必须通过 KnowHub 原有的知识管理权限和操作执行。

本轮只开发和部署测试环境。生产只读核对版本，不修改生产内容、队列、Hold、revoked、暂停状态或同步发布策略。

## 原有问题

`exactPublishedReadContext` 原先调用 `prepareExactPersonalSource(..., true)`，每次读取都重新核验原导入者账号、当前 OAuth、远程版本及 view/export/copy 权限。`verifyExactPublishedDatabase` 另要求当前 Grant Epoch 与历史发布代际相同。后台 Reconcile 也把已完成的发布与未完成来源任务一起检查，存在将源端授权异常传播到发布状态的风险。

因此本地已经有文件、Chunk、Embedding 和正式回执，仍可能因原导入者断开授权而不可读。这是源端授权与知识库授权的耦合。

## 新读取链路

```mermaid
flowchart LR
    U[当前读取者或用户 MCP Key] --> A[当前账号、空间和 KB 权限]
    A --> R[唯一 published Receipt]
    R --> L[本地版本、文件和绑定]
    L --> D[Knowledge、Chunk、Embedding 指纹]
    D --> S[仅当前文档的只读 Scope]
    S --> O[正文、分块、文件或检索]
    O --> V[输出前再次核验]
```

这条链路不调用 `UserToken`、`prepareExactPersonalSource`、`resolveResource` 或飞书权限 API。

`prepareExactPublishedSnapshot` 保留以下本地证明：

1. v1 回执对应的 Import、Attempt、Tenant、KB、Knowledge、CandidateVersion、Generation 和历史源身份一致。
2. Attempt 已 indexed，nonce 已消费，没有活动 Lease；Resource 当前活动版本准确绑定已发布版本。
3. Manifest 与保留的原文件一致，大小、SHA256、MD5 均通过；资源仍是持久化、未删除的 local provider 文件。
4. StoredResource 与唯一 ResourceBinding 对应原 Knowledge；文件读取后再次核对注册表和绑定。
5. Knowledge、Chunk、Embedding 数量及数据库指纹与正式发布回执一致。
6. 回执仍唯一且为 published，文档仍启用且未删除。

文件读取使用服务端本地 provider，上下文保留当前读取者身份及取消信号，不放入原导入者 UserToken。physical path 只能来自已校验注册表，并受 local provider 的存储根目录保护。

当前 Grant Epoch 表与 OAuth 连接不再参与已发布读取。回执中的历史 GrantEpoch、AppConfigVersion、ConnectionIdentity 仍与原 Attempt/Import 一致，不能伪造或改写历史来源。

## KnowHub 权限

每个读取边界重查当前用户有效性、空间状态、当前成员身份和角色，以及 `access.ResolveKB` 的真实 KB/共享/私人知识库权限。执行租户不能被当成读取者所属空间。

用户 MCP Key 保留 WebUser 所有者身份要求，并分别核验数据库中的 Key 所有者、有效期、撤销状态、retrieve capability、实时空间 Scope 和 KB Scope。租户 Key、平台 Key、缺失用户身份或失效 Key 不能通过此路径获得个人文档内容。

`WithExactPublishedDocumentRead` 和 `ReadExactPublishedPage` 继续使用原有单文档只读能力。其 Commit 始终拒绝。已持有 A 文档能力的上下文不能将它扩展为 B 文档能力或跨 KB 查询。

## 多文档和模型输入

检索先分开 receipted 个人文档和普通本地上传文档。个人文档逐个进入原检索器，独立核验回执；损坏文件不提供任何内容，其他合法文档可以继续。没有回执或尚未发布的个人文件不会退回普通文档路径。

多 KB 请求只在存在正式个人文档范围时拆分，分别核验每个 KB，最后再次核验权限与实际返回的发布副本。没有个人回执的请求保留原有路径。

RAG 的 `publishedReadBundle` 是请求内的 repository 适配器：父块和候选 Chunk 按所属文档分别读取，不合并权限，也不授予 KB 级读取。Chat 与 Rerank 的实际输入继续与 canonical 数据及渲染模板核对，再签发原有模型输入证明。未知历史、媒体或未验证外部来源仍不被此适配器承认。

Document ACL 的既有隔离试点不能被当成常规生产开关。单文档本地回执读取接入原有输出重验；未认证的试点文件、流式、Portal 或 MCP 路由仍必须完成其自身认证，不能因本次改造而绕开全局路由隔离。

## 同步与后台维护

导入、受控重试、重新绑定、SQL Claim 和发布前源端检查保持原样。没有改动 `feishu_sync_publication_policy.go`。

Reconcile 对已经消费的 published 回执单独处理。源端授权失败只标记 Import 的同步状态需要处理，不修改已发布 Attempt、Generation、活动版本、Receipt、Chunk 或 Embedding。完整性损坏继续拒绝读取，并单独报告。

查询状态增加两个只读响应字段（`gorm:"-"`）：`publication_status` 与 `source_sync_status`。界面区分“当前已发布可用”和“后续同步需要处理”。普通读者无需重新连接原导入者的飞书。

## 兼容和迁移

复用 `personal_exact_receipt_v1`，不新增授权表，不改变 Receipt 格式，不需要数据库迁移。合法历史 published v1 回执自动使用新读取行为；缺失、staging、错误版本、损坏或撤回记录保持拒绝。既有 Knowledge ID、Chunk、Embedding、游标和审计均不重建。

回滚为替换测试镜像和前端版本，不需要逆向数据迁移。若将来上线后回滚到旧代码，旧代码会重新依赖源端授权，可能恢复原来的不可读行为；因此必须保留部署版本和实际验收记录。

## 验证与发布门槛

专项测试使用原生解析、真实 PostgreSQL 索引和原子发布产生回执，再执行源端失效、权限撤销、文件/绑定/指纹破坏、并发读取撤权、用户 MCP Key 和多文档测试。模拟的飞书/模型端点必须明确标注，不能代替真实 OAuth 验收。

真实验收另要求两个测试账号、三篇合成飞书文档、真实登录/断开连接、Web/Agent/Go MCP/Python MCP，以及 ACL 开关两种运行方式。必须保留读取阶段飞书远程调用为零、原 ID 和指纹未变化、读者撤权后拒绝的证据。

截至开发期间的只读核查，原测试环境唯一连接的 Access Token 和 Refresh Token 均已过期；真实 OAuth 验收需专用测试身份重新登录授权。未完成的项目必须保留为待验收，不能据自动化测试宣告生产发布就绪。
