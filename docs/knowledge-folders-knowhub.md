# KnowHub 多级空目录：实现、验收与发布

## 基线与上游取舍

2026-11-26 拉取远端后从 `suoak/WeKnora main` 的 `556657a92b7bd3359c36997ce9086996a20f29a0` 创建独立 worktree。原工作区的 SourceID、Confluence 和 MCP 未提交改动没有带入。上游 main 为 `3b36d23dcb729987f64b1a6e054354a90048b3ee`。核查时 fork 没有 open PR；本任务随后创建 [PR #272](https://github.com/suoak/WeKnora/pull/272)。合入前必须重新 fetch main 并核查迁移编号。

[上游 PR #5037](https://github.com/Tencent/WeKnora/pull/5037) 尚未合并，审查 head 为 `ab4c8fa56a631c67e0c563a7b74d9fc65a0f1929`，涉及 90 个文件。采用其轻量持久化设计并选择性移植、适配代码；未 cherry-pick 整个提交。

| 上游内容 | KnowHub 处理 |
| --- | --- |
| `knowledge_folders` 四字段表、复合主键 | 复用；不增加 folder ID / parent ID；不回填或改造既有文档 |
| 名称 UTF-8 字节、16 层、1024 字节校验 | 复用；新建与子树改名禁止静默截断 |
| 创建/空目录删除服务、错误映射、创建对话框和状态机 | 复用并适配现有权限和组件布局 |
| 目录树零计数合并、目录标记随子树改名合并 | 复用思路；保留 KnowHub 原有文档读范围过滤 |
| `existing_folder_anchor` | 不引入；省略 anchor 的历史上传及移动行为保持兼容 |
| 文件替换、source CAS、重新解析 claim、处理补偿 | 不引入；超出本次目录持久化范围，不能凭目录测试证明这些变更安全 |
| 并发控制 | 复用 tenant/KB 事务 advisory lock，仅用于显式目录操作；改名读取文档时加行锁；通用后台 Save 忽略 FolderPath，避免旧对象覆盖用户移动；不在远程解析/上传期间持锁 |
| 上游迁移 000136 / SQLite 000052 | 不使用原编号；两个方言均使用 KnowHub 新编号 003052 |
| 自动生成 agent/tool 元数据 | 不注册新 MCP 工具，不增加 Portal 或隔离环境的入口权限 |

[KnowHub #188](https://github.com/suoak/WeKnora/pull/188) 的合并提交是 `6695b47f3fa34c20cffaf47d45ee89020aaca76e`。保留目录移动的 deletion-metadata 投影、整批身份绑定检查和 Editor 操作授权。003048、003051 原 SQL 不修改，飞书内容验证、删除与同步 fencing 均保留。

## 行为与兼容性

`knowledges.folder_path` 继续表示文档位置。新表仅记录用户显式创建的目录；列表将这些路径作为零计数行加入原目录树，不制造文档、不发起解析、不生成 embedding，也不改变检索索引。

新建接口在现有父目录下创建一个目录。重复使用子目录入口可建立多层空目录；每一层均落库。不存在的父目录返回 404，显式或文档推导出的同名目录返回 409。名称限制与前端状态校验一致。

| 接口 | 语义 |
| --- | --- |
| `GET /api/v1/knowledge-bases/:id/knowledge/folders` | 合并显式空目录和原文档目录树；计数只统计文档 |
| `POST /api/v1/knowledge-bases/:id/knowledge/folders` | JSON `{"parent_path":"资料/设计","name":"草稿"}`；成功 201 |
| `PUT /api/v1/knowledge-bases/:id/knowledge/folders` | 既有 from/to 接口；同事务改名/移动目录标记与文档，目标存在则合并 |
| `DELETE /api/v1/knowledge-bases/:id/knowledge/folders/empty?path=...` | 只删除无文档、无子目录的显式目录；根目录不可删除 |

`moved_count` 仍然是文档数。空目录改名成功时可以是 0，前端依据 HTTP/API 成功结果显示成功。未知源目录沿用原改名接口的零文档 no-op 行为。历史单文件、目录上传、文件替换与数据源任务载荷未改变。旧上传请求仍可隐式产生目录；慢上传在显式空目录删除之后到达时，仍可能通过文档路径重新推导出该目录，这是保留的历史行为，不声称实现了 #5037 的 anchor 协议。

删除操作不递归删除任何文档。删除最后一份文档或将其移出后，显式目录仍保留；仅由文档推导的目录保持原来的生命周期。清空文档与软删除 KB 不主动销毁目录表中的历史标记；不可访问的 KB 标记不对其他 KB 暴露，便于恢复与数据保全。

目录写 API 使用既有 ingest/full-access、KB 所有者或管理员及 KB Editor 检查，服务再次消费精确操作 grant。只有 tenant ID、Viewer grant、其他 KB/tenant 的 grant 均不能写。文档范围读取继续只返回已授权文档形成的树，不额外暴露 KB 中空目录名称。未改变 MCP/Portal/飞书授权配置、检索入口或文档读取保护。

## 验证与证据

- 后端：类型校验、目录创建/重载/合并/改名/删除、LIKE 通配符转义、并发同名创建、目录标记与文档更新失败一起回滚；服务精确权限与错误码；真实 handler/service/repository HTTP 测试。
- PostgreSQL：`TestPersistentKnowledgeFoldersPostgres` 在专用 `feishu_atomic_test` 下创建新的随机数据库，运行正式完整迁移链，重新建立连接确认多层零文档目录，验证并发唯一性、所有文档字段除位置/时间外完全相同，以及目录表 down/up 后导出数据恢复。
- 飞书：既有真实 PostgreSQL derived-write 和 atomic-publication 测试继续运行；增加飞书受管文档在持久目录中移动、改名和非空删除拒绝检查。
- 前端：41 项目录状态、校验和树回归测试；`vue-tsc --build`；Playwright 操作实际 Vue 组件，通过真实 Go handler/service/repository 和磁盘 SQLite 数据库创建三层空目录、刷新、改名、删除叶子、选择器持久化、Viewer 无写入口。
- [专用 CI](https://github.com/suoak/WeKnora/actions/workflows/knowledge-folders.yml) 上传 `knowledge-folder-evidence`，包含后端日志、浏览器 API 日志、trace 和页面截图。截图使用合成数据；浏览器组件夹具不等同于统一测试环境全站验收。

首次 CI 的真实 PostgreSQL 目录生命周期测试已通过；同时暴露既有迁移测试写死最新版本 3051 的断言，以及测试夹具未接入路由角色检查。已据此修正断言和夹具；最终验收以 PR 最终 head 的完整结果为准。未通过的阶段不可记为通过。

## 迁移、备份和数据保全

新增 `migrations/{versioned,sqlite}/003052_knowledge_folders.{up,down}.sql`，更新 migration-map 的两方言 current_latest 和 logical_parity。既有迁移保持不变。003052 仅建表；不 UPDATE 原文档，不触碰 003048 飞书函数。保留复合主键、非空路径和 PostgreSQL UTF-8 字节长度约束。

统一测试环境为用户指定的 `172.30.232.42`。该主机有多套隔离项目，必须依据 80/8080 实际入口和 Compose 标签确定目标，不能根据目录名猜测正在使用的 APP/Worker/Frontend。

发布步骤：

1. final-head CI 全部通过后合入 main；再次核对 main SHA、003052 编号及构建 provenance。
2. 保存当前 Compose/override、镜像 digest、环境文件（私有目录 0600）、路由、迁移版本；记录正在运行的飞书 execution/lease、队列和来源状态。暂停新同步调度，等待在途任务完成；超过可控窗口则中止部署，不强杀、不清游标、不解除 HOLD。
3. 按当前实际架构停止所有写入进程（含内嵌 Worker），保留 Redis、文件卷与数据库。使用 `pg_dump -Fc` 做全库备份，并在新建的隔离数据库中 `pg_restore --exit-on-error`，核对文档/分块/数据源/同步状态摘要和行数。备份必须恢复验证后才可升级。
4. 以同一 main SHA 构建并部署 APP、Worker、Frontend；迁移从实际版本顺序运行至 3052，禁止 Force 或手改版本行。先确认迁移 clean、APP/Worker 健康，再开启写入与原同步调度。
5. 在测试环境页面和 API 验收空 KB 三层目录刷新保留、改名移动合并、非空删除拒绝、Viewer/MCP retrieve/Portal 写拒绝。比较既有文档与分块摘要，验证原文档检索和飞书同步任务可正常结束，记录截图和请求结果。
6. 提交上述证据及具体生产镜像、备份与切换窗口给用户审批。获批后才部署生产；不在生产先手改数据库或替换代码。

目录表保全：升级前尚无该表时记录这一事实；升级后任何回退前，额外用 `pg_dump -Fc --table=public.knowledge_folders` 导出，并记录行数、tenant/KB 范围和备份 SHA-256。目录名称可能敏感，备份不提交到 Git，也不放入公开 CI artifact。

2026-11-26 对测试入口的只读核查：80/8080 指向 `knowhub-mvp-p3` 的 APP/Frontend，该 APP 同时运行后台任务；没有其他正在运行的容器使用同一 `WeKnora-postgres/WeKnora` 数据库。数据库为 3037、clean，有 34 份有效文档、5,033 个有效分块、22 个有效 KB，受管飞书资源为 active 6 / HOLD 2。该目标不是紧邻的 3051 基线，必须先在备份副本上演练 3037→3052 全链升级与完整恢复；不能将 003052 的兼容性证明直接当作这次跨版本升级的证明。

本地实际验证：41 项前端目录测试、Vue 类型检查、生产 handler/service/repository 的 HTTP 测试通过；Playwright 两项端到端组件验收通过（使用真实磁盘数据库）。[三层空目录刷新截图](qa/knowledge-folders/nested-empty.png)、[Viewer 页面截图](qa/knowledge-folders/viewer.png) 均来自合成组件验收环境。统一测试环境的最终页面/API、文档摘要、检索与飞书任务验收仍须在完成上述发布步骤后单独记录。

## 回滚

首选回滚 APP/Worker/Frontend 到保存的兼容镜像组合，数据库保持 3052 并保留新表。旧代码继续使用 `folder_path`，忽略新增表，因此新建空目录数据不会丢失；它们会在再次升级后恢复展示。不要为代码回滚盲目执行 down。

上述保留 schema 的代码回滚只适用于已经验证兼容的紧邻版本组合。当前测试环境的历史 3037 运行时不能默认兼容 main 的其他新增迁移；在该环境回退时，需要使用已验证的完整数据库快照恢复到新库，保留失败库和独立目录表导出，再把 APP/Worker/Frontend 一起切回原镜像和数据库连接。

只有确认必须回退 schema 时，先停全部写入、验证全库备份及目录表独立备份可恢复，再执行 003052 down。down 会删除目录表，不能撤销已发生的文档目录移动；要求恢复文档原位置时应使用部署前完整备份恢复到新库并切换，保留失败库取证。再次 up 后用目录表备份恢复，先验证其 tenant/KB 身份和唯一性，再开放流量。严禁无备份 DROP、修改既有 migration checksum 或强制修改版本号。
