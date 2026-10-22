# 第六批测试及候选证据

测试范围限隔离环境和合成数据。最终候选与完整命令/退出码见 [45](45_SIXTH_BATCH_DELIVERY.md) 及 `evidence/batch6/delivery-validation.json`。旧 FAIL 与全部中间候选均保留，不覆盖旧日志。

## 环境与容量

基线 RC3 `e62b17e2d305358467821aeff1cce63ce1d61254`，原独立 worktree/branch 不变。开始先验证第五批 157 个 artifacts 并保存 6,209 个原文件的完整快照，tar 每个 member 已重新计算 SHA256 与初始清单逐项一致。

容量门禁沿用第五批：根盘保留至少 8 GiB，数据盘至少 40 GiB、空闲 inode 至少 100,000，本批任务卷参考上限 30 GiB，持续观测，不足停止本批任务。开始根盘可用约 52.1 GiB、数据盘约 1.49 TiB，Docker Root 已在数据盘。本批新卷 `knowhub-mcp-b6-20261021-291847-data`，新源候选/Go cache/tmp/PGDATA/socket/二进制均在卷内；第五批卷只读作缓存复制来源，原卷和原文件未删除。

缓存初次复制 exit 1：复制期间目的缓存目录已由构建建立，部分 cp 报 File exists。不能把它写成“完整缓存复用 PASS”。构建使用新任务内的冷/部分缓存，由 Go 自身重编译/校验，未依赖该复制步骤的成功来认定源码通过；容量门禁始终有效。原日志 `volume-prepare.log` 保留。没有再次清理服务器文件。

PostgreSQL 首两次 Unix socket 启动失败：ParadeDB 初始化内部 psql 仍使用默认 socket；改为保留 `/var/run/postgresql` 同时提供本批 `/work/pgsocket`，网络 none、不发布端口。保留失败日志及退出码。该修复只修改测试 runner，未改生产拓扑或数据库。

固定 Go 1.26.7 镜像和 Python 3.11.16 / MCP SDK 2.2.0 镜像，PostgreSQL 17.9、pg_search 0.22.2、vector 0.8.1。正式迁移器空库 0→3035，未改 SQL/3033/3034/3035。回归不自动继承先前 PASS。

## 已实现测试

| 测试源码 | 真实覆盖 |
| --- | --- |
| `service/portal_mcp_test.go` | 动态 public/private scope、旧 Key、Portal 撤回、私有权限独立、额外私有 workspace scope、空 selected scope、scope 主体/Key 替换拒绝、unsupported driver、默认关闭 |
| `router/portal_mcp_postgres_test.go` | 正式 PG 迁移、实际 driver/两模型维度、关键词/向量/Hybrid/grep/多 KB/跨 Tenant；canonical owner/stale-body；模型 HTTP spy；部分失败；Portal/成员/Key 撤销；并发与查询期间撤权；实际个人 Go MCP HTTP initialize/list/call |
| `mcpserver/personal_test.go`、`router/routes_personal_mcp_test.go` | 默认 disabled、nil Provider/service、伪造 Header 不开启；disabled 路由不尝试 Auth/last_used 写入 |
| 既有 M5 `document_reads_test.go`、`document_python_integration_test.go` 等 | 新候选重跑实际 Knowledge/Chunk/REST、模型路径 HOLD、旧 Python HTTP/SSE + Auth、输出前撤权；不是用新个人入口替代旧读取面的保护 |

首次真实 PG 用例已证明关键词/向量/模型 wire 工作，但成员撤销后测试错误地期待空结果而非明确拒绝，触发 require 后未恢复夹具，影响后续子场景。修正为更强的“错误且无结果、零 driver 调用”，并补状态恢复；另单独验证关闭公共范围后真实空集合零查询。原 exit 1 保留，没有删授权断言或关闭服务端检查。

## 最终命令结构

实际命令数组、开始/结束时间和 exit code 在最终 results.json；不是 launcher 返回 0 就认定通过：

```text
go test -race -tags sqlite_fts5,knowhub_m6_postgres -p 2 -count=1 -v ./internal/router -run TestPortalM6RealPostgresPersonal
go test -tags sqlite_fts5 -p 2 -count=1 <related complete packages>
go test -tags sqlite_fts5 -race -p 2 -count=1 <related packages> -run 'TestPortalM6|TestDocumentM5|TestDocumentM3|TestEnterpriseM2'
go vet -tags sqlite_fts5 <related packages>
python -m unittest discover -s mcp-server -p 'test_*.py' -v
go test -tags sqlite_fts5,knowhub_m5_integration -c <router test binary>
router.test -test.v -test.count=1 -test.run TestDocumentM5PythonActualREST
service.test -test.v -test.count=1
agent.test -test.v -test.count=1
```

完整 Go 相关包包括 access、repository、所有已存在 driver 包、database、types、middleware、router、handler、container、mcpserver；service 和 agent/tools 编译后二进制在 Python 3.11 镜像完整执行（含 tomllib），并非仅跑聚焦用例。race 为指定授权场景和 PG 实际集成，不是全仓库 race。已有 driver 包单测通过不等于这些 driver 已纳入公共 MVP；新入口仍只允许已验证 PostgreSQL 路径。

## Windows

本批原样再运行完整命令，55 tests / **1 error，exit 1**：真实 `os.symlink` WinError 1314。仍 **FAIL / BLOCKED_OS_PRIVILEGE**，没有 skip/mock/删除断言/改系统权限。见 [原输出](evidence/batch6/windows-python-full.log)、[命令和 SHA](evidence/batch6/windows-commands.json)。第五批及更早的三个原 ERROR 全部保留；Linux symlink PASS 不覆盖 Windows FAIL。

## 尚未验证

生产开启、实际业务员工资格 Provider、正式角色赋权、生产迁移、实际生产模型/索引版本下规模性能、完整历史 KB 分类、4 个大 KB 过滤扩展、全部共享 agent 私人路径、Wiki/FAQ/graph 来源、文件/CDN/历史匿名链接撤权、多 Worker Python SSE、全客户端 UI 联调均未完成。本批任何 PASS 都限最终冻结候选和已列合成场景。

## 最终实测

完整 Linux 套件候选 `fe6f77e896ec82aa…` 的全部命令 exit 0；包含真实 PG 集成 race、相关 Go 完整套件、授权场景 race、vet、Linux Python 55 项、实际旧 Python HTTP/SSE SDK、完整 service/agent。原始命令见 [完整结果](evidence/batch6/linux/results-fe6f77e896ec82aa-all/results.json)。

随后仅在 `knowhub_m6_postgres` 构建标签的测试文件中增加 Tenant/KB deleted 两条状态变更断言，应用源码及其他测试/runner 未改；候选 `a93d6c107c80ef74…` 重新完整执行该真实 PG 集成，exit 0，见 [补充结果](evidence/batch6/linux/results-a93d6c107c80ef74-integration/results.json)。该次是普通 PG 集成执行，之前同一应用源码的并发/查询撤权场景已做 race；不声称新增加的两条删除断言又跑了 race。无必要重复未受影响的完整套件。

`verify_batch6_delivery.py` 强制两候选之间只有这一带标签的测试文件差异；任何应用、其他测试或运行工具漂移都会失败。两份服务器源目录各 5,932 文件在执行后重新读回 SHA256，均匹配其冻结源清单；当前应用源码匹配完整套件、当前新增断言匹配补充候选。完整指纹、命令结果和证明见 [45](45_SIXTH_BATCH_DELIVERY.md) 与 [交付验证](evidence/batch6/delivery-validation.json)。

本批容量共 1,424 个运行中采样，任务卷观测高水位 **15,438,864,384 bytes（约 14.38 GiB）**，无容量阻断或采样错误。不是精确瞬时峰值，前几批精确峰值仍 NOT_MEASURED。结束检查根盘可用 **55,317,884,928 bytes（约 51.52 GiB）**、数据盘可用 **1,626,789,552,128 bytes**，无仍运行的本批标签容器，卷和停止的本批 PG 容器保留。见 [结束检查](evidence/batch6/environment-after.log)。未清理历史文件，未发生 ENOSPC。
