# 真实批次 P0 诊断

应用版本 `bf5980b281244b7ce87d61d9af2bd69b411f3b82`，2026-10-25 隔离站 `test` 账号。先只读检查 PostgreSQL、Redis 归档任务及两路应用日志，再执行修复；未读取到聊天输出的文档正文、凭据或敏感链接。下列标识为原 ID 的 SHA-256 前 12 位。

| 批次 | 条目 | 导入 | 候选状态 | 已观察重试 | 正式 Knowledge / Chunk / 向量 |
| --- | --- | --- | --- | --- | --- |
| 89e9b3e0c05c | 3910d0e328c5 | 575034e7aa73 | recovery | 9 | 0 / 0 / 0 |
| 002abcce1bcd | 05484e5c0022 | fdb0268083e0 | recovery | 38 | 0 / 0 / 0 |
| 4d5f533801f3 | c37d3d796379 | fdb0268083e0 | recovery | 38（同一导入） | 0 / 0 / 0 |

三个批次均已持久化并被 Reconcile 领取，条目为 dispatched；两个底层资源均解析为 Docx。用户连接与目标访问检查已完成，实际 Feishu 元数据请求 HTTP 200，创建导出、查询导出均 HTTP 200，正文导出下载 HTTP 206，约 36,947 字节。随后未能提交任何 manifest 收执，尚未进入正式知识解析、Embedding、Chunk/向量/BM25、发布及 Wiki 阶段。

明确根因：两路 `WeKnora` 进程实际 UID/GID 为 `1000:1000`，但共享 staging 卷根目录为 `0:0:0755`。Redis 两个归档 document:process 任务的 protobuf ErrorMsg（field 7）均为 staging 路径上的 mkdir permission denied。读取容器默认 root exec 的 `test -w` 会产生错误的可写判断；必须核对实际进程用户。存储错误而非飞书拒绝下载，是本次首个阻塞点。

配套缺陷：候选 fetch/commit 错误统一写成 `candidate_fetch_or_commit_unconfirmed`，重试只限制单次退避、没有总上限；条目只看导入状态而未展示 attempt 错误；reconcileBatchUpdates 定期更新条目时间，即使没有下游进展；管理页面没有自动刷新及真实阶段，形成长期“导入中”。

调度链路：app/worker2 均运行 dispatch recovery loop，正常每分钟一轮，单轮 45 秒；ReconcileBatches 每轮最多两个条目。候选失败后 outbox 为 recovery，任务已归档，周期恢复重复尝试。并非未领取或模型网关卡住。

针对根因的即时修复仅将本隔离项目 staging 卷根目录设置为 `1000:1000:0700`：核对两路容器相同镜像、相同具备项目标签的命名卷后执行，非递归、不修改数据库、不重置任务/租约、不替换 Knowledge ID。保留现有所有私人 KB 和任务，随后观察原始任务自动恢复结果。

接下来的代码修复：kb_id 必填且仅使用明确选择的已有目标；启动时验证 staging 实际可写；候选错误分类与有限重试；真实下游阶段/成功推进时间与保守停滞判断；前端自动刷新和检索入口。真实业务验证结果在完成后追加，不能以合成资料替代。

目录修复后的原始任务恢复观察：两个真实导入均使用原先预留的 Knowledge 标识完成解析及发布（脱敏标识分别为 `3663d886f314`、`070925df4104`）；每篇均为 parse completed、enabled、summary completed、6 个 Chunk、6 条 1024 维向量。对应 personal_index_receipt 为 verified，正式文档 outbox 为 processed，原候选 outbox 为 completed；attempt 经受控来源交接成为 obsolete，历史重试计数保留为 10/39，无需重建任务或更换 Knowledge ID。三个批次中的两个条目引用同一个去重后的导入。此观察证明原始真实任务恢复，尚不等同于 Portal/MCP 检索验收通过。

2026-10-25 本轮产品约束覆盖早期第 10 节说明：快速导入必须指定已有知识库，不再调用 EnsurePrivateKnowledgeBase；已有自动创建的私人 KB、Knowledge、任务、索引和文件全部保留。清理需先完成引用核对，再单独批准；本轮不执行清理。
