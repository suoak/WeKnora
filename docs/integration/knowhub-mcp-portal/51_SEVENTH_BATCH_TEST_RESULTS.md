> **Historical batch-7 checkpoint:** See [55: final dynamic Portal policy](55_DYNAMIC_PORTAL_PUBLIC_REQUIREMENTS.md) and [56: current acceptance](56_DYNAMIC_PORTAL_ACCEPTANCE.md). Old approval requirements and test passes do not define the final product or validate the new candidate.

# 第七批测试、指纹与未通过门禁

## 候选与保全

最终 Linux 候选 SHA256：`f8d390328770aa0120a7e7621fa3aefcca28ac931db80eb12024d7280d12b645`。

Git 基线 `e62b17e2d305358467821aeff1cce63ce1d61254`，分支 `audit/knowhub-mcp-b4-rc3-20261021`。本批没有 commit；基线不能代表未提交源码。候选标识是排序后逐文件 SHA256 映射的 SHA256，归档内容在上传前重新读回验证。最终候选含 5958 个源文件（不含 evidence）；完整映射和归档哈希见 [候选 manifest](evidence/batch7/candidates/f8d390328770aa0120a7e7621fa3aefcca28ac931db80eb12024d7280d12b645.json)。

开始前 6343 文件快照保存在 `E:/AIData/Codex/knowhub-b7-preserved-20261021.tar.gz`，另有 [快照 manifest](evidence/batch7/prior-source-snapshot.json)。结束时重新核对旧归档、00～45 报告、旧工具及第一至六批原始证据。没有删除旧 FAIL。新增/修改文件按相对前六批快照统计，不能将全部未提交文件都归为第七批。

远端以 Docker 只读挂载核对最终及两份真实客户端候选的全部源文件，5958/5955/5957 个文件无差异，见 [读回结果](evidence/batch7/remote-source-readback.json)。最终无 race 检索运行后再核对一次。最终交付的报告、离线校验器单独更新，不影响冻结的运行时、测试和执行工具。

## 环境与原始命令

隔离测试机 `172.30.232.42`；任务 `knowhub-mcp-b7-20261021-102200`，专用卷 `knowhub-mcp-b7-20261021-102200-data`。未清理服务器历史文件。源快照、日志、PG 隔离数据均保留；没有接触生产数据库写入。

| 项目 | 实际环境 |
| --- | --- |
| Linux Go | Go 1.26.7，`knowhub-build/golang:1.26-bookworm-ustc`，CGO/SQLite FTS5，模块缓存只读 |
| 数据库 | `paradedb/paradedb:v0.22.2-pg17`，PG 17.9、pg_search 0.22.2、pgvector 0.8.1；正式项目迁移器初始化隔离 0→3035 |
| Linux Python | 3.11.16、MCP SDK 2.2.0，已有受控镜像；service/Agent 测试二进制在此执行以满足 tomllib |
| Windows | PowerShell；Python 3.12.11、SDK 2.3.0；前端复用已有 node_modules，不安装/升级依赖 |
| 网络 | 构建/数据库/常规测试容器 `--network none`；外部模型使用 loopback 合成 spy；真实客户端夹具仅测试主机 loopback/SSH 本机转发 |

执行器：[batch7_linux.py](tools/batch7_linux.py)、[上传/收集](tools/batch7_upload.py)、[本机测试](tools/batch7_local.py)、[真实 CLI](tools/batch7_cli_clients.py)、[WorkMate](tools/batch7_workmate_client.cjs)。完整 Docker argv、环境、开始结束 UTC 与 exit code 均位于对应 results.json，不能只依赖下列摘要。

主要 Go 命令（Docker 环境与挂载见原记录）：

```text
go test -race -tags sqlite_fts5,knowhub_m6_postgres -p 2 -count=1 -v ./internal/router -timeout 18m -run 'TestPortalM6RealPostgresPersonal|TestPortalM7RealPostgresScale'
go test -tags sqlite_fts5 -p 2 -count=1 ./internal/application/access/... ./internal/application/repository ./internal/application/repository/retriever/... ./internal/database ./internal/types ./internal/middleware ./internal/router ./internal/handler ./internal/container ./internal/mcpserver
go test -tags sqlite_fts5 -race -p 2 -count=1 ./internal/application/access/... ./internal/application/repository ./internal/application/service ./internal/router ./internal/handler -run 'TestPortalM6|TestPortalM7|TestSourceIDM7|TestEnterpriseSnapshotM7|TestDocumentM5|TestDocumentM3|TestEnterpriseM2'
go vet -tags sqlite_fts5 ./internal/application/access/... ./internal/application/repository ./internal/application/service ./internal/router ./internal/handler ./internal/container
python -m unittest discover -s mcp-server -p 'test_*.py' -v
```

完整 service 和 Agent 包分别 `go test -tags sqlite_fts5 -c` 后运行 `-test.v -test.count=1`，不使用 `-run` 缩减。旧 Python 真实 REST 用例编译 `sqlite_fts5,knowhub_m5_integration` 并执行 `TestDocumentM5PythonActualREST`。本批没有声称运行整个仓库所有包的 `go test ./...`；完整范围是本次受影响包及上述完整 service/Agent。

## 最终 Linux 结果

[完整结果及命令](evidence/batch7/linux/results-f8d390328770aa01-all/results.json)、[完成标记](evidence/batch7/linux/results-f8d390328770aa01-all/done.json)。所有 17 条记录 exit 0：

| 检查 | 结果 | 原始日志 |
| --- | --- | --- |
| 真实 PostgreSQL + race、原 M6 驱动/HTTP/canary/model spy 和新增 M7 规模 | PASS，exit 0 | [postgres-personal.log](evidence/batch7/linux/results-f8d390328770aa01-all/postgres-personal.log) |
| 20 份真实 EXPLAIN ANALYZE/BUFFERS | PASS，exit 0 | [查询计划](evidence/batch7/linux/results-f8d390328770aa01-all/postgres-explain-plans.log) |
| 受影响 Go 核心包 | PASS，exit 0 | [go-core.log](evidence/batch7/linux/results-f8d390328770aa01-all/go-core.log) |
| 授权、策略/CAS、身份、服务及 HTTP race | PASS，exit 0 | [race.log](evidence/batch7/linux/results-f8d390328770aa01-all/race.log) |
| 静态 vet | PASS，exit 0 | [vet.log](evidence/batch7/linux/results-f8d390328770aa01-all/vet.log) |
| Python transport/安全完整 55 项 | PASS，exit 0 | [python-full.log](evidence/batch7/linux/results-f8d390328770aa01-all/python-full.log) |
| 旧 Python 实际 REST + SDK | PASS，exit 0 | [router-python-sdk.log](evidence/batch7/linux/results-f8d390328770aa01-all/router-python-sdk.log) |
| 完整 application/service | PASS，exit 0 | [service-full.log](evidence/batch7/linux/results-f8d390328770aa01-all/service-full.log) |
| 完整 agent/tools | PASS，exit 0 | [agent-full.log](evidence/batch7/linux/results-f8d390328770aa01-all/agent-full.log) |

附带初始化、Go 版本、PG 启停及二进制编译记录也为 exit 0。依赖缓存首次复制曾遇到文件存在错误；保留 setup 日志，不将复制视为成功证明。实际构建及上述套件结果独立验证。

## 大 KB 普通运行与性能

同一冻结候选额外执行去掉 `-race` 的真实 PostgreSQL 用例，独立新 PG 数据目录，保留正式 0→3035 初始化。最终结果见 [普通执行命令](evidence/batch7/linux/results-f8d390328770aa01-integration/results.json)、[完成标记](evidence/batch7/linux/results-f8d390328770aa01-integration/done.json)、[日志](evidence/batch7/linux/results-f8d390328770aa01-integration/postgres-personal.log)。

规模为 128/257/512/864/1001，每篇一个 Chunk，1024 维。每模式 6 次，另有三 KB/两个 owner 的 Hybrid 6 次、1001 篇的四并发、取消和 deny。完整授权集合不截断；尾文档非相同向量确保真实排序断言。P95 是六样本最大值，不能外推到生产高分位 SLO。

最终数字、单库与多库 P50/P95、授权解析成本和执行计划摘要见 [performance-summary.json](evidence/batch7/performance-summary.json) 与 [表格](evidence/batch7/performance-summary.md)。race 数据仅作功能观测。公共精确向量使用 MATERIALIZED 授权集合，不宣称 HNSW 加速。摘要解释有限 Hybrid/grep 候选窗口，不声称无限候选全局 RRF 或全库正则召回。

## 前端与 Windows

[本机前端结果](evidence/batch7/local/20261021-105028-frontend-results.json)：39 项测试、`vue-tsc --build`、Vite build 均 exit 0；每项附输入文件 SHA256，最后再次与当前前端源码匹配。输出保存在工作树外。没有将组件级测试当作浏览器中完整批准或 Key 创建流程验收。

[Windows Python 结果](evidence/batch7/local/20261021-120148-windows-python-results.json)：55 项中 1 项 ERROR，exit 1；`os.symlink` 的 WinError 1314，继续 **FAIL_ENVIRONMENT**。未跳过、mock 或降低相关断言；Linux 同一 Python 源码 55 项通过，不覆盖 Windows 失败。

## 真实客户端与撤销

客户端版本、16 场景及未测项见 [49](49_REAL_CLIENT_ACCEPTANCE.md)。

- 正向、另一员工、Portal 撤回运行在 `9c1abdaecde67b422efee7378641a4823035dc9372579477c178381c61eb7599`；Codex 实际 tools/call，三次 CLI exit 0。
- 最终撤销运行在 `b52aeb73812f0e282865ac0a6ac86fb2d0afc7cd9eb65c236e751d5797a5f33f`；[HTTP 时序](evidence/batch7/clients/revocation-http.json) 为 A 初始化 200、请求撤销后轮询 200→401、独立 C 200。控制标志写入不等于事务已提交；没有单独记录精确提交时刻，不能把这个首轮询标为撤销已生效后的成功读取，也不能据此宣称提交后传播延迟上界。未返回文档正文。
- [Codex 撤销结果](evidence/batch7/clients/codex-revoked-result.json) exit 1、约 4.65 秒结束；[日志](evidence/batch7/clients/codex-revoked.log) 明确 `HTTP 401`，是预期的有界鉴权拒绝。夹具正常退出，独立凭据仍有效，排除服务退出导致的 502 假阳性。
- 这些客户端候选的应用 Go 运行时代码与最终候选逐文件一致；夹具和新增断言差异单独以 manifest 保留，不宣称所有文件相同。
- WorkMate 自身真实 transport initialize/list 五工具通过，tools/call **NOT_TESTED**；Claude 返回 `Not logged in`，**BLOCKED_CLIENT_AUTH**。

## 保留的失败及适用范围

旧批次 FAIL 全部保留。本批中间结果也不覆盖：

| 候选前缀 | 原失败 | 处理/最终范围 |
| --- | --- | --- |
| 369fec64a0b8d6b0 | 大 KB 清理夹具 FK；空 Chunk nil/空数组导致旧版本哈希不一致；完整 core/race FAIL | 保留原输出；修复版本兼容和夹具顺序，最终完整套件重新通过 |
| d8599e | 确认包测试硬编码旧 revision | 改为读取真实最新 revision，再验证 CAS/原子性，未删授权断言 |
| 4904f9 / 9e4511 | 客户端服务默认超时、等待人工客户端超过夹具时限 | 原 FAIL 保留；专用有界人工夹具重跑 |
| 9c1abdae | 反复执行撤销，第二次 NotFound 导致夹具退出；撤销客户端 502 | 正向调用单独有协议证据；整体夹具不算 PASS；b52 修复一次控制动作后重新以 401 验证 |
| 早期 Codex / WorkMate | 客户端取消调用；桌面启动超过首次 60 秒 | 不算工具通过；分别调整实际只读配置与有界启动等待，保留原记录 |
| 生产只读 inventory attempt 1/2 | 聚合 SQL 语法 42601 | 仅只读失败，无写入；最终只读事务查询成功 |
| Windows 最终 | symlink WinError 1314 | 仍 FAIL，不用 Linux PASS 替代 |

## 容量、源码与交付校验

每秒记录根盘、数据盘及 inode；约每 15 秒采样任务卷大小。门禁为根盘余量 8 GiB、数据盘 40 GiB、inode 100000、任务卷 30 GiB；不足停止本批容器。最终观测最小余量和任务卷采样高水位由 [交付校验](evidence/batch7/delivery-validation.json) 汇总，**不是瞬时精确峰值**。未通过删除历史文件腾空间。

运行 `python docs/integration/knowhub-mcp-portal/tools/verify_batch7_delivery.py <上述完整候选SHA>`，校验旧快照、原证据、当前测试输入、前端输入、远端读回、原始 exit、客户端证据、容量、报告链接及日志无明文合成 Token。

逐文件本批差异见 [change-inventory.json](evidence/batch7/change-inventory.json)，最终报告/证据 SHA256 见 [artifact-sha256.json](evidence/batch7/artifact-sha256.json)。报告更新在测试候选之后，交付校验将“测试输入指纹”和“含最终文档的交付指纹”分别记录，不能混淆。

## 尚未通过

**企业公共 MVP 正式验收 HOLD；完整混合敏感 M3 HOLD。** 正式员工在职/机器账号契约和企业发布审核职责仍 NEEDS_DECISION。没有自动赋权 SystemAdmin/Owner、没有历史 48 KB 批准；生产公共可读为 0。未知来源不是已确认敏感，也不是默认普通。

尚缺正式 Provider 与角色端到端、可信新增普通文档写入装配、完整审核记录 UI、真实模型跨语言质量和生产多 Chunk/负载、三客户端完整场景、Wiki/FAQ/Graph/文件 Broker/旧匿名 URL/CDN/来源 ACL 闭环。独立性能结果不授权放宽这些门禁。

本批没有新迁移；3034/3035 与之前保持字节一致。最终隔离 0→3035 通过不代替生产 3033→3035 迁移验收或变更批准。生产 Python MCP、Key、成员关系、Portal 数据及数据库均未修改；没有提交、推送、合并或部署。
