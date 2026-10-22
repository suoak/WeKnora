> ?????2026-10-22???????????? runtime `2ac380a033af80f022d98a3b45cd5e30645b148d07cde8c76c46cce94787392b` ??? runtime `1d533edfe33957dc6b74960594ff7d12ab33e4cfe670f631d10074fc9e9bad1a` ????????21/21 exit0??????Go???race?vet?APP???????????PG/race/PG vet??exit0??????PG????vet exit0??? b789/1d733/1380/d321 ??????????????APP???????????????????????

# 阶段 B 开发交付与阶段 C 入口

日期：2026-10-22（北京时间）。**开发期质量门禁已通过；联合部署尚未执行；不代表生产同步恢复获准。**

## 候选与保护范围

| 对象 | 当前标识 |
|---|---|
| RC3 Git HEAD | e62b17e2d305358467821aeff1cce63ce1d61254 |
| 独立飞书工作树 | E:/code/WeKnora-worktrees/feishu-safe-incremental-20261021 |
| 独立运行源码指纹 | b78966cf1556cd581b5db4428f38ba273c284bea5cb2590111e06f22df495746 |
| 联合工作树 | E:/code/WeKnora-worktrees/knowhub-v1-feishu-integration-20261021 |
| 联合运行源码指纹 | 1d733295720560c773624954b41353889c5081bf0fb596dada7055da98ed2440 |
| 保留的 V1 冻结版本 | 5468da42c518cc83539040506c85b7964bdf2ae20826e641dbd030253316e231 |

运行指纹按排序后的 `path + NUL + file_sha256 + LF` 计算，排除本 MCP 集成文档目录；完整交付包将另有包含文档/工具的源码指纹及归档 SHA。所有源码未提交。V1 原工作树运行文件逐文件校验 0 差异，原冻结包和运行实例未覆盖。

## 已实现与已验证边界

- `service/feishu_legacy_staging.go::StageFeishuLegacyItem`：真实解析器、已有分块逻辑和模型 HTTP 客户端；候选文件按 attempt 隔离，Chunk、halfvec 和 BM25 存储在私有表，未发布候选不进入公开检索。
- `repository/feishu_legacy_atomic.go::Begin/Prepare/Publish/Receipt`：tenant/KB/datasource/external_id 唯一身份、稳定 Knowledge ID，revision、generation、nonce、lease；校验模型、KB、存储配置及真实候选后，在单个 PG 事务中更新公开 Knowledge/Chunk/向量/BM25、稳定文件绑定、容量计量和持久回执。真实 COMMIT 成功但 ACK 丢失可凭回执恢复，旧回执不能确认新游标。
- `3036` 数据库触发器及实际事务锁：围栏贯穿实际写入/删除，不仅预先检查。已验证未提交旧写入排空、租约接管、旧 Worker 迟到写入/清理、部分公开写入故障回滚、MVCC 可见性及模型/配额变化拒绝。
- `repository/feishu_legacy_quarantine.go`：来源不确定先持久 HOLD；可信内部撤回保留 revoked，清理失败不能回滚已提交的 source HOLD。缺失集合、403/404 不推断永久删除，429/5xx/超时不误删。恢复需完整来源对账，不能因单篇成功解除整个来源限制。
- `datasource_execution.go::WriteExecution`：游标在同一事务中校验发布收据；失败项不确认。保留原 checkpoint 失败即停止及删除失败不确认 Cursor 两处修复。
- `types/feishu_legacy_read.go`、PG 候选谓词、graph 门禁、模型和 HTTP/file 写出保护：已覆盖真实候选前拒绝、直接 Knowledge/Chunk、已知历史引用、Wiki 投影、graph 后端调用前检查、Chat/Rerank 调用前与流输出复验、本地文件和旧 HMAC URL 每次读取复验。注册 local backend 和 ResourceCatalog 复用原组件，不另建存储体系。
- 数据源控制面保持既有租户/角色/Key 权限。恢复操作只允许 KB 元数据例外；历史同步诊断走普通内容权限，HOLD 时不返回旧日志、游标、结果、错误正文及来源配置，不修改真实持久游标。

以上源码前缀为 `internal/application/`，另列 types/router/migrations 除外。逐文件 SHA 与精确测试命令在候选 Manifest 和原始 results.json；不以 Git HEAD 冒充未提交版本。

## 开发期检查结果

两个候选均已完成以下真实 Linux 检查，全部 exit 0：

| 检查 | 实际范围 |
|---|---|
| 完整 Go | `go test -tags sqlite_fts5 -p 2 -count=1 ./...` |
| 完整 service | 不过滤测试名的 `./internal/application/service` |
| 完整 Feishu | 不过滤测试名的 `./internal/datasource/connector/feishu/...` 与 datasource |
| 完整 database | 正式迁移与版本检查，真实隔离 PG 及 SQLite |
| 原子发布 | 真实 PG/ParadeDB，不是内存假索引；private BM25、实际向量、公开事务与故障注入 |
| 连接器状态机 | 实际 Wiki/Drive HTTP 连接器 → ProcessSync → 真实解析 → 合成 HTTP 1024 模型 → PG；非真实飞书云端 |
| 迁移 | 正式 3033 → 3034 → 3035 → 3036；旧 Key、Portal、知识保持；允许的空隔离库 down/up |
| race | PG 原子发布 race，Feishu/同步执行服务竞态回归 |
| vet / APP | Feishu 和服务静态检查，`sqlite_fts5,anydoc` 原生 APP 构建 |

独立证据：`evidence/feishu-incremental/linux-b2-acceptance-source/results/`。联合证据位于联合树 `evidence/feishu-incremental/linux-joint-acceptance-source/results/`。所有命令、环境、退出码、执行耗时和原始日志均保存；外部服务缺失导致的 SKIP 不得外推为生产 PASS。

受控测试镜像补齐 Python 3.11 ensurepip，真实 venv/pip 离线安装已验证；仅修改测试环境，未改生产 Python。正向公共域名解析采用隔离 hosts 夹具，容器没有真实云端访问；失败/私网 URL 安全断言保留。此前 DNS、ensurepip、Redis、迁移夹具版本及测试类型错误的原始 FAIL 保留，停止的过期测试候选记为未完成，不能记为 PASS。

## 迁移及独立交付

独立 RC3 修复不包含 V1 个人 MCP 功能移植；3034/3035 仅携带 V1 原始正式 SQL 以保持连续链，校验和不变，新增能力使用 3036。没有 Force、空迁移、临时迁移链或生产写入。可以形成独立 RC3 源码/APP 镜像候选，不必等待正式 SID/MCP 业务验收，但**能独立构建不等于获准上线或恢复同步**。

## 保留限制与正式门禁

1. 支持范围为共享 PostgreSQL/ParadeDB、document KB、平面分块及受控 local 存储。其他驱动、独立 vector store、parent-child、图片/音频/多模态及无法确认旧文件路径，拒绝或 HOLD；不自动放宽。
2. 当前可信撤回由内部接口及合成场景验证；真实云端删除、移动与撤权事件没有经单独授权的测试应用/文档验收。不能把应用账号可读当作所有员工授权。
3. 不完整或不可信历史衍生来源未完成完整 M3 治理；只能对已明确覆盖的读取面给出结论，不能声称未知历史引用全部安全。正式放开相关来源前需补齐实际覆盖或保持隔离。
4. 没有证明可即时撤销的对象直链/CDN缓存仍受限。新 APP 可拒绝旧未过期链接，但不能收回已下载/已缓存字节。
5. 私有候选及旧文件当前保留，不自动删除；需要正式容量预算、监测与后续受控回收。模型门禁有保守拒绝，可能影响含 HOLD 来源租户的可用性，应在集中环境观察。
6. 真实 SID、正式 Embedding、真实飞书云端和生产数据范围仍 NOT_TESTED/BLOCKED。不能因合成结果解除生产停更。

默认回滚关闭新增量并保留 3036 与加固读取；旧 APP 没有这些保护，不能直接接回用户入口。详见 [发布与恢复操作卡](FEISHU_INCREMENTAL_RELEASE_AND_RECOVERY_RUNBOOK.md)。下一步按既有清单冻结、同源构建并执行阶段 C，不另起架构批次。


## ??????????????2026-10-22?

??RC3??? b78966cf??????????????1d7332/d321???????MCP???????????????APP??Worker???????KID?BM25????????MCP??????????writer????????????V1???????Chunk???????????FAIL???????????

???????????`postgres/portal_scope.go::portalCanonicalIndexText`?????Knowledge/Chunk???????????Chunk SourceID???`postgres/repository.go`?????????????????????Chunk????????????????????????????/KB/?????????Key?????PG `router/portal_canonical_postgres_test.go::TestPortalCanonicalIndexWritersPostgres`????writer???????/?????????????????????????????PASS?
