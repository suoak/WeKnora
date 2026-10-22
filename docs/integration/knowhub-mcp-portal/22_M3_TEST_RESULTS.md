# M3 第三批测试与证据

**总门禁 HOLD。** 组件测试、真实 SQLite 驱动、数据库迁移和隔离 HTTP/SSE 夹具分别验收；任何局部 PASS 都不代表完整业务 REST/MCP/文件链安全。最终执行摘要、候选和逐文件 SHA256 见 [证据索引](evidence/batch3/delivery-index.json)及 [源码清单](evidence/batch3/source-manifest.json)。

## 环境与复现

继续使用原 worktree `E:\code\WeKnora-worktrees\knowhub-mcp-portal-20261020`、原分支 `audit/knowhub-mcp-portal-20261020`。Git HEAD 仍是 `be308d0058db4e647cd5103ae27b39da3a20eace`；它不标识本批未提交源码。候选 SHA256 绑定全部覆盖文件及模块清单，测试结束再检查文件一致性，不能只引用 HEAD。

Linux 为此前允许的隔离测试机。Docker `--network none`，测试 PostgreSQL tmpfs、任务专属 Unix socket；SQLite 临时合成库。没有连接生产。构建 Go 1.26.7 / Debian Bookworm，运行 Python MCP/service 二进制使用受控 Python 3.11.16、tomllib、packaging 和 ensurepip。镜像 ID、头文件 SHA256、二进制 SHA256、原始命令及退出码都随各运行目录保存。服务测试对 example.com/api.e2b.app 使用固定 DNS fixture，仍无出站网络；没有禁用 SSRF 断言。

正式复现脚本为 `tools/batch3_linux.py`：先复制到新的 `/home/aqyfzx/ai/knowhub-mcp-m3-YYYYMMDD-<6hex>/` 任务目录，再运行 `python3 <taskdir>/batch3_linux.py all`。按脚本布局准备 source、manifest、只读模块缓存和匹配的 SQLite 头文件；脚本不应在工作站或生产运行。`python-integration` 参数仅执行两个明确命名的兼容性命令，`postgres` 参数仅执行 PG 验证，不记录未选择测试为 PASS。默认 all 模式执行下列全部检查：

```text
go test -tags sqlite_fts5 -count=1 -v ./internal/application/access ./internal/application/repository -run 'TestDocumentM3|TestEnterpriseM2'
go test -tags sqlite_fts5,knowhub_m3_old_sqlite -count=1 -v ./internal/application/repository -run '^TestDocumentM3OldSQLiteBuildHOLD$'
go test -tags sqlite_fts5 -p 2 -count=1 ./internal/application/access ./internal/application/repository ./internal/application/repository/retriever/... ./internal/application/service/retriever ./internal/types ./internal/database ./internal/middleware ./internal/router ./internal/mcpserver
go test -tags sqlite_fts5 -race -p 2 -count=1 ./internal/application/access/... ./internal/application/repository ./internal/application/repository/retriever/sqlite ./internal/application/service/retriever ./internal/middleware ./internal/router
go vet -tags sqlite_fts5 ./internal/application/access/... ./internal/application/repository ./internal/application/repository/retriever/sqlite ./internal/application/service/retriever ./internal/types ./internal/database ./internal/middleware ./internal/router
python -m unittest discover -s mcp-server -p 'test_*.py' -v
go test -tags sqlite_fts5 -c ./internal/application/service
go test -tags sqlite_fts5 -c ./internal/agent/tools
# 上面两份完整测试二进制在 Python 3.11 容器内执行 -test.v -test.count=1。
go test -tags sqlite_fts5,knowhub_m3_integration -c ./internal/application/repository
# 在 MCP 依赖容器内执行 -test.run '^TestDocumentM3PythonHTTPCompatibility$' -test.v -test.count=1。
python3 docs/integration/knowhub-mcp-portal/tools/batch3_pg.py <isolated-task-dir>
# PG 工具运行实际 migrate runner，以及含真实首次授权/撤权竞态的 -race 测试。
```

真实 Docker 参数以 results.json 为准，不能只运行以上等价命令却省略 native headers、运行时或 synthetic DSN。真实检索测试单独放在 `sqlite_fts5` tag 文件中，因为 FTS5 是驱动编译条件；默认仓储测试不因此新增系统 sqlite-vec 头文件依赖。native 测试在显式 tag 命令中完整执行，没有删改授权断言来隐藏失败。

## 执行结果

最终结果由原始 exit_code 汇总在 [delivery-index](evidence/batch3/delivery-index.json)，逐次执行都保留，早期 FAIL 不覆盖。最终明细见本报告末尾。

已定位并区分：

- Windows 首次 native 编译缺 sqlite3.h；匹配模块头文件的构建与普通默认测试分开记录。
- Linux 旧系统头文件使 sqlite-vec 不支持 IN。正向构建使用与 Go SQLite 一致的头文件，负向旧构建必须在模型前明确 HOLD，不接受“返回空”作为安全成功。
- 初版 HTTP/SSE 夹具存在 repository/middleware 测试 import cycle、SDK 2.2 的 stream tuple / is_error 字段差异，已修正测试适配；未修改旧 MCP 模块。
- 旧 MCP SSE Key 撤销后后端拒绝，但 SDK 等待工具结果超时：该门禁必须保留 FAIL。普通读、敏感拒绝、Grant 撤销与旧 Key/full_access 场景与它分组执行，不能把超时记成拒绝 PASS。
- 上一批 pyproject 依赖缺失断言在 Python 3.11 的完整 service 套件中重新执行；只有真实退出码为 0 才收口，旧批失败原文保留。

## 门禁矩阵

| 门禁 | 实际覆盖 | 完整 M3 状态 |
| --- | --- | --- |
| D01 | 真实 SQLite 向量/FTS/双路 TopK=1、敏感/旧索引 canary、Rerank/LLM 回调 spy | PARTIAL；原 Hybrid/grep/Wiki/FAQ/graph 模型链未全面接通 |
| D02 | exact doc/operation、Chunk 归属、文本读取、broker 合成 hash/撤权/重绑定；隔离路由拒绝猜 ID/文件 | PARTIAL；现有 rich-response detail/list/batch/revision/neighbor/export 未逐路认证 |
| D03 | live Key/用户/Portal/文档/源状态、个人 Grant 撤销、当前 KB/Key scope 适配测试 | PARTIAL；完整应用缓存/所有共享路径未端到端 |
| D04 | 普通 Key、机器/full_access、伪造来源 Header；旧 Python HTTP/SSE 夹具的 mcp_blocked | PARTIAL；旧 SSE Key 撤销调用结束 FAIL；生产对应完整后端未认证 |
| D05 | 多源内存 DerivedText，单来源撤权整份缓存拒绝、unknown proof 拒绝 | HOLD；持久化 Wiki/FAQ/图谱/历史消息来源不完整 |
| D06 | SQLite 实际 SQL + 实际计划能力检查、空/超限 allow-set、错 tenant、scope 交集、未认证驱动拒绝 | PARTIAL；其余驱动仅阻断，不认证运行时预过滤 |
| C01 | 无效主体、过期/撤销 Key、停用用户、Key owner 匹配、旧会话后端拒绝 | PARTIAL；SSE 客户端结束语义 FAIL |
| C02 | 不信客户端身份/Header，grant 绑定 Caller/Key/目标/操作与授权上下文 | 组件验证；完整 Auth → 所有 Handler 待验证 |
| C03 | 新公共能力默认关闭、不授成员/admin、当前 scopes/成员/共享重验 | 组件验证；正式角色/企业资格 NEEDS_DECISION |
| C04 | 未认证工具/写路由和流式输出在隔离屏障拒绝 | PARTIAL；完整 Go MCP 的所有工具输入/调用链未认证 |
| P01 | 合成 A 非目标成员、明确批准 public 文本、管理/文件不随读 Grant 授予 | 组件/夹具验证；无真实公共启用 |
| P02 | 旧 retrieve Key、Tenant/Platform/full_access 不取得新企业读取 | 组件验证，无回填 |
| P03 | Portal archived、新请求重验；个人 restricted 读取不依赖 Portal 配置存在 | 组件验证 |
| P04 | 缺行/未知/非 local/历史 verified 拒绝；新资源需明确 review | 组件验证；不认证真实来源 ACL |
| S01 | 单真实 Tenant/KB 绑定；跨 KB/不支持驱动/模式拒绝 | PARTIAL；跨模型、多 KB 实际组合 NOT_TESTED |
| S02 | 无标注召回集、无 Recall/MRR/nDCG | NOT_TESTED |
| S03 | 存储/审计错误拒绝、流式/过大响应 HOLD、模型间撤权阻止后续输入 | PARTIAL；SSE 撤 Key 超时 FAIL；全驱动超时/部分失败未全测 |
| S04 | PG CAS/首次插入授权与撤权并发、无 timestamp 写入失效、模型间撤权、缓存来源重验 | PARTIAL；完整应用高并发、重连、Redis/索引竞态未全测 |
| S05 | 仅合成测试耗时，不作为服务性能指标 | NOT_TESTED |
| M01 | SQLite 正式 runner PASS；PG SQL 回放、真实 ACL/CAS/race、冻结生产源组合 runner PASS | 当前分支直接从 3033 的正式 runner **FAIL**：缺 3033 来源；不得据组合源 PASS 发布 |
| M02 | SDK 真实 HTTP 与 SSE 对隔离 Go 夹具 | PARTIAL/FAIL；不是三个真实客户端、不是完整业务后端 |

## 边界

没有全仓库 `go test ./...` 的通过声明；执行的是上述相关包的完整套件及明确标记的集成/负向构建。无真实外部模型、云 CDN 或源端 ACL 服务参与；不能把合成 verifier、内存资源 Store、模型回调 spy 当作生产认证。没有用 skip、改预期或删除断言将 FAIL 改写成 PASS。

## 最终实测汇总

| 执行 | 结果 / 退出码 | 原始证据 |
| --- | --- | --- |
| Linux 核心 M3/M2、相关完整包（含所有 retriever 包）、安全相关 race、vet | 各 0 / PASS | [完整命令](evidence/batch3/linux/a7f021/results.json) |
| 旧 SQLite 头文件负向构建 | 0 / 明确 HOLD、模型调用次数 0 | [负向构建](evidence/batch3/linux/a7f021/old-sqlite-capability-hold.log) |
| Python 原完整回归 | 53/53、0 / PASS | [Python](evidence/batch3/linux/a7f021/python-full.log) |
| 完整 service / agent tools 测试二进制，Python 3.11.16 | 各 0 / PASS；包含原 pyproject 缺依赖断言 | [service](evidence/batch3/linux/a7f021/service-python311.log)、[agent](evidence/batch3/linux/a7f021/agent-python311.log) |
| Windows 默认相关完整套件 + native SQLite 实际检索 | 各 0 / PASS，源码匹配 | [命令/环境/候选](evidence/batch3/windows/final-results.json) |
| 旧 MCP HTTP 与 SSE 普通/文档 ACL 分组 | 两组 PASS；HTTP Key 撤销 PASS | [实际 HTTP/SSE](evidence/batch3/linux/ce1cb5/python-http-sse-go-acl.log) |
| 同上 SSE Key 撤销 | **FAIL、Go 集成测试退出 1**；后端拒绝已确认，SDK 超过 10 秒未结束 | [同一原始日志](evidence/batch3/linux/ce1cb5/python-http-sse-go-acl.log) |
| PG 冻结 3033 空库 SQL 回放、新 DDL up/down/up、旧列签名 | SQL 检查 PASS | [PG 汇总](evidence/batch3/linux/7098d6/pg-results.json) |
| PG 真实读、主体 Grant、首次授权/撤销 20 次竞态、TRUNCATE/无 timestamp epoch、重装 incarnation，race | TestEnterpriseM2PostgresCASAndMigration PASS | [真实 PG](evidence/batch3/linux/7098d6/pg-live-cas.log) |
| PG 正式 migrate + 冻结生产源/3034/3035 组合 | ComposedMigrationRunner PASS，旧 Key 不变，历史无审批，3035 down/up | [组合来源逐文件 SHA256](evidence/batch3/linux/7098d6/composed-source-sha256.json) |
| PG 正式 migrate + 当前分支 migration source，从 3033 升级 | **FAIL**：缺 3033 source，PG 命令整体退出 1；断言未放宽 | [失败及独立 PASS 子测试](evidence/batch3/linux/7098d6/pg-live-cas.log) |

完整套件候选 `8e508ac122ea63df8279eb2c6e193a08e15401d6f688dbd3ad88c46a6e40512c`；PG 定向验证候选 `69202b97b1e20e1c3f2169057211df53b29b81138c8894d4647123afa2960521`。随后用真实持久化 Tenant full_access Key 强化 HTTP/SSE 夹具并定向复测，最终交付候选为 `531b9be978c07aed89814edf4d1951dc74d8b7e8d3d2786806449edd46cb85c0`。相对完整套件，差异只有 PG/HTTP 两份集成夹具和两份测试执行脚本；运行时源码指纹均为 `b071aec3ede5b70c033ae694ff4eab27369a4f942b15208f346db2a21b67dc49`，逐文件已比较一致，没有无意义重复已经通过的核心套件。各次快照及完整文件清单均保留。

第一批冻结 6 份原始证据、第二批 182 份 artifact 的原 SHA256 校验全通过，00～17 报告全部保留。此前所有失败和修正过程保存在 11 个独立运行目录，done.completed 只表示命令执行结束，不表示测试通过。此处保留两个明确 FAIL 和所有 NOT_TESTED/HOLD。

交付完整性校验见 [delivery-validation](evidence/batch3/delivery-validation.json)。源码、摘要和报告链接匹配；严格 gofmt 检查对一份数据库测试文件报告 CRLF 差异，规范化后与 gofmt 输出相同，记录 WARN 和原严格检查 FAIL，保留受测字节。该格式结果不计为授权或功能 PASS。
