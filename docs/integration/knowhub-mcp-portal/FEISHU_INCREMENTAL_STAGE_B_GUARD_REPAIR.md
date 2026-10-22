# 阶段 B 进度：RC3 Cursor 故障保护修复

**本报告仅交付已完成的Cursor故障保护。完整飞书安全增量仍未实现，不是“Wiki/Drive可恢复自动同步”的验收结论。阶段C完整联合部署尚未开始。**必要staging、共享索引安全发布及可信撤权闭环见阶段A，不用局部PASS替代。

## 版本与改动

基线RC3 `e62b17e2d305358467821aeff1cce63ce1d61254`；独立工作树 `E:/code/WeKnora-worktrees/feishu-safe-incremental-20261021`，分支 `fix/feishu-safe-incremental-20261021`。源码未提交。

| 项目 | 版本 |
|---|---|
| 修复前故障复现源码（含新增测试） | `76d378931bee942edbfe4543dd61efa48a54657219e32605235a84ddbce99f8c` |
| Cursor保护修复候选，5810文件 | `a54ccc33c1332f5e32f32b740866a571b32e768b7c159b917c63de92b1b2f271` |
| 本地完整源码及逐文件manifest | `E:/AIData/Codex/feishu-safe-incremental-20261021/after-a54ccc33c1332f5e/` |
| Linux源码/日志/构建 | `/home/aqyfzx/ai/WeKnora-release-build/feishu-safe-incremental-20261021/after-a54ccc33c1332f5e/` |
| 依赖/迁移/补丁/证据SHA | [delivery-manifest.json](evidence/feishu-incremental/delivery-manifest.json)及同目录`rc3-guard-only.patch` |
| Linux APP构建产物 | `results/WeKnora`，352765760字节，SHA256 `b80aa6f099aa5e7a699590f1fdc249e777d3ac7caffa8b96636d5ceb5d693f92`；[artifact.json](evidence/feishu-incremental/artifact.json) |

指纹规则：对打包文件按相对路径排序，以 `path + NUL + SHA256(raw bytes) + LF` 拼接后SHA256。包含完整RC3 tracked源码及两个新增Go测试；本轮报告/执行工具/日志单独记SHA不混进业务指纹。包内文件逐项回读校验后才运行测试；不是GitHub main clone。冻结V1仍是5468，联合最终候选 **未形成**。

| 源码文件 / 函数 | 已实现 | 安全边界 |
|---|---|---|
| `internal/datasource/connector/feishu/core/engine.go::runSync` | Checkpoint返回错误立即中止并返回nil Cursor，保留errors.Is链 | 适用于共用引擎的Wiki/Drive；DB故障、失去execution fence、取消均不能继续处理下一资源/删除 |
| `internal/application/service/datasource_service.go::applyFetchedItem` | Feishu删除查找、DeleteKnowledge、HardDelete任一失败返回dataSourceIngestError，使Emit失败锁存，Cursor不能确认 | 保留计数、错误分类及其他连接器原有行为；不新增删除权限，不把403判删除，不修改SyncDeletions |
| `internal/datasource/connector/feishu/core/engine_checkpoint_failure_test.go` | DB提交不可用/stale fence/context取消三项故障注入，断言没有后续list/fetch/emit | 真实共享引擎+合成NodeOps；不宣称真实PG故障/Worker原子切换 |
| `internal/application/service/feishu_incremental_delete_failure_test.go` | feishu/feishu_drive/lark/lark_drive × lookup/delete/hard-delete，断言错误链/不确认/计数 | 12项合成repository故障；不冒充可信云端删除或撤权端到端 |

实现源码只改13行新增/1行替换；不修改前端、MCP、OA、个人导入、Docx/Sheet/Bitable实现、配置默认值或停更屏障。无依赖或数据库迁移变更。

## 已执行测试与原始失败

Linux运行于打包服务器独立开发检查容器，Go1.26.7，源码只读，network=none；仅开发缓存可写。没有启动新业务APP或连接生产库。完整argv/exit/耗时位于下列results.json，不能仅凭本表认定无环境缺口。

| 检查 | 实际命令摘要 | 结果 / 证据 |
|---|---|---|
| 修复前Checkpoint | `go test -count=1 -v ./internal/datasource/connector/feishu/core -run TestIncrementalCheckpointFailureStopsSharedEngine` | **FAIL exit1**；[原始日志](evidence/feishu-incremental/linux-before/results/new-core-contract.log)，原代码返回非nilCursor、nil错误且处理后续资源 |
| 修复前删除 | `go test -tags sqlite_fts5 -p 2 -count=1 -v ./internal/application/service -run TestIncrementalFeishuDeletionFailureDoesNotConfirmCursor` | **FAIL exit1**；[原始日志](evidence/feishu-incremental/linux-before/results/new-service-contract.log)，12项失败返回nil |
| 同样新增测试修复后 | 同上，修复候选a54ccc | **PASS exit0/0**；[results.json](evidence/feishu-incremental/linux-after/results/results.json) |
| 连接器相关回归（带-run过滤） | `go test -tags sqlite_fts5 -p 2 -count=1 -v ./internal/datasource/... -run ...` | **PASS exit0**；同目录feishu-all.log。名称虽为feishu-all，实际范围是记录在argv的过滤范围，不能解释为全套 |
| 同步/替换/执行fence相关 | `go test -tags sqlite_fts5 -p 2 -count=1 -v ./internal/application/service -run 'Feishu\|DataSource\|ProcessSync\|SyncExecution\|Incremental\|ReplaceKnowledge'` | **PASS exit0**；service-fencing.log |
| race | `go test -race -tags sqlite_fts5 -p 2 -count=1 ./internal/datasource/connector/feishu/... ./internal/application/service -run ...` | **PASS exit0**；race.log，具体范围以argv为准 |
| vet | `go vet -tags sqlite_fts5 ./internal/datasource/connector/feishu/... ./internal/application/service` | **PASS exit0**；vet.log |
| APP构建 | `go build -tags sqlite_fts5 -o /results/WeKnora ./cmd/server` | **PASS exit0**；app-build.log。未构建新APP/前端镜像或部署，不冒充AnyDoc原生联合验收 |
| 未过滤完整连接器/调度器与service（首次） | `go test -tags sqlite_fts5 -p 2 -count=1 -v ./internal/datasource/connector/feishu/... ./internal/datasource`；同参数`./internal/application/service` | **FAIL exit1/1**；[首次完整结果](evidence/feishu-incremental/linux-after/full-regression/results.json)，network=none无法DNS解析open.feishu.cn/example.com/api.e2b.app，导致URL校验及相关断言失败；原始记录保留 |
| 同源码、同完整套件，受控解析重验 | 同上；仅测试容器增加三个`--add-host <name>:8.8.8.8`，仍network=none | **命令PASS exit0/0，但有SKIP**；[重验完整结果](evidence/feishu-incremental/linux-after/full-regression-dns/results.json)。未改源码/断言、未关SSRF，固定公共地址仅用于解析验证，容器无外网连接 |
| Windows最初复现 | Go1.26.8同Checkpoint测试 | **FAIL exit1（环境链接）**：pg_query C对象缺`__stack_chk_fail`，尚未执行断言；before-checkpoint-windows.log。Linux原始断言FAIL与修复后PASS另外保存，不覆盖Windows结果 |

未删、跳过或放宽新增拒绝断言。原有其他测试中的SKIP应保留并统计；完整affected suites另存full-regression，不以聚焦测试替代其结果。

实际完整重验统计：[full-suite-scope.json](evidence/feishu-incremental/full-suite-scope.json)。Feishu/datasource有293个PASS标记、3个SKIP；service有3296个PASS标记、90个SKIP，标记计数包含子测试，不等于顶层测试数量。PG/Asynq、真实云端、真实parser等缺少相应环境的项目按原测试自行SKIP，本轮未增加跳过条件；它们仍是NOT_TESTED，不能将完整命令exit0描述为这些业务门禁全部PASS。

主机前后只读比较另存noninterference.json：308个基线容器中307个同名镜像/状态/端口/配置未变化，包括V1五容器及原有服务；一个无Compose项目的临时Go开发镜像容器随后不存在。Docker历史事件已无法查到该条，归属保留UNKNOWN，不笼统写全部308零变化。没有执行stop/rm/down或共享服务配置变更。

## 仍未完成的实际安全门禁

- 旧来源变化的内容仍返回ErrFeishuStagingRequired。没有私有候选Chunk/Embedding产物、可验证receipt及同KID的原子可见发布。
- 首次新增的异步dispatch接纳不等于索引完成；Cursor需进一步绑定真实完成收据。
- stale解析Worker在检查后、索引写入/按KID清理前仍可能发生竞态；本次execution失败传播不解决全部副作用。
- 列表消失与可信删除/撤权未完成可靠区分；即时拒绝覆盖REST/MCP/引用/模型输入仍需实现验证。
- PostgreSQL/ParadeDB故障注入、源版本成功切换、旧版保留、并发旧Worker、稳定KID积压重放、真实Cursor恢复和真实飞书云端均 **NOT_TESTED**，不能记成PASS。
- 新联合候选的MCP/Codex、3033→3035、备份恢复、性能及正式SID/模型未执行；不能继承5468验收。

## 独立回滚与发布边界

此Cursor保护候选不需要迁移，可以独立基于RC3构建，且不含大量MCP改动。若以后获批部署，先保留原镜像、部署配置及生产停更状态；回退APP/Worker镜像并继续维持已批准停更，数据库/来源/Cursor不变，不执行3034/3035 down。旧Worker排空及共享服务不受影响仍是必要部署条件。

**当前不建议把该包作为“恢复增量同步热修复”发布。**它没有实现完整修复目标，也没有执行阶段C；只可作为后续最小安全闭环的已测基础。没有生产写入、解除停更或commit/push/merge/release。
