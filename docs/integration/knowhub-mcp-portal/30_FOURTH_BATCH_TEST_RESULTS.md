# 第四批：测试结果与候选指纹

本批 P0-A/P0-B 在隔离实测范围内通过。M3 整体 HOLD，Windows Python 完整套件仍 FAIL，不能将本表解释为全部平台、全部仓库或全部业务安全验收。

## 候选与环境

- RC3 Git 基线：`e62b17e2d305358467821aeff1cce63ce1d61254`。
- 最终冻结测试候选：`04bed643e30d6a7e9bf6bd071e919722dfbe7e0eb6b73784f07fcd25eeafd3e4`，任务 `675615`。
- 当前运行源码集合 SHA256：`f2d281a90a70d8f417bc2b666bdbdc17f94ee99f528a6d853c2e06b99f40708e`；已逐文件核对当前运行源码与冻结候选相同。
- 当前变更源码、测试及新增编排工具集合 SHA256：`6e482642f3d78e26d50ed151932c430cb6834cd7069c62c3b44779d1a3fe513c`。编排 resume/finish 在冻结 source 之外执行，单独保留脚本哈希；新增交付报告也与冻结候选分开标识。
- Go 1.26.7 linux/amd64；Python 3.11.16、mcp 2.2.0、requests 2.34.2、packaging 26.3、uvicorn 0.52.4；Windows Python 3.12.11 / mcp 2.3.0。
- Linux Docker 禁止网络；Go CGO 使用 sqlite 3.46.1 模块 header，另测旧 header 拒绝；PostgreSQL 使用 ParadeDB v0.22.2-pg17 的隔离 tmpfs 数据库与限定 Unix socket。没有生产 DSN。
- 后续补跑的容器临时目录使用显式 `exec` tmpfs，避免磁盘峰值；只影响隔离测试容器。Go service/agent 在受控 Python 3.11 镜像运行，保留全部测试。

证据：[逐文件源码](evidence/batch4/source-manifest.json)、[冻结完整清单](evidence/batch4/linux/675615/manifest.json)、[镜像 ID](evidence/batch4/images.json)、[运行源码完整性](evidence/batch4/linux/675615/results-final/candidate-integrity.json)。新增代码清单为 source manifest 的 `changed_and_new_files`，包含移植 M2/M3、正式迁移测试、Python 修复及测试工具共 49 项。

## 最终验证结果

| 检查 | 实际结果 | 证据/退出码 |
|---|---|---|
| PostgreSQL 正式旧库升级/空库/rollback/CAS/故障快照恢复 | PASS | [完整 Go 迁移与 CAS 输出](evidence/batch4/linux/675615/pg-live-cas.log)、[PG 结果](evidence/batch4/linux/675615/pg-results.json)，0 |
| SQLite 正式全量、3033 升级、down/up、全行一致性、失败恢复 | PASS | [相关完整 Go 包](evidence/batch4/linux/675615/results-final/go-full.log)，0 |
| 相关 Go 完整包测试 | PASS | 同上，未以聚焦用例替代完整所列包 |
| Go race | PASS | [race](evidence/batch4/linux/675615/results-final/go-race.log)，0 |
| 旧 SQLite header 无安全 IN 能力 | PASS（确认拒绝/HOLD） | [原始输出](evidence/batch4/linux/675615/results-extra/old-sqlite-capability-hold.log)、[命令/退出码](evidence/batch4/linux/675615/results-extra/results.json)，0 |
| Go vet | PASS | [vet](evidence/batch4/linux/675615/results-resume/go-vet.log)、[独立退出码](evidence/batch4/linux/675615/results-resume/go-vet.exit.txt)，0 |
| Linux Python 全部 55 项 unittest | PASS | [完整输出](evidence/batch4/linux/675615/results-resume/python-full.log)，0 |
| 完整 service，在 Python 3.11 + tomllib 环境 | PASS | [service](evidence/batch4/linux/675615/results-final/service-runtime.log)，0 |
| 完整 agent/tools | PASS | [agent](evidence/batch4/linux/675615/results-final/agent-runtime.log)，0 |
| 真实 Python HTTP/SSE → Go ACL，SDK 2.2 | PASS | [协议/时序/业务观察器](evidence/batch4/linux/675615/results-final/python-integration-runtime.log)，0 |
| 真实 Windows SDK 2.3 HTTP/SSE | PASS | [输出](evidence/batch4/windows-sdk23-integration.log)、exit.txt，0；运行代码与最终冻结候选相同 |
| Windows Python 完整 55 项 | **FAIL，3 errors** | [完整失败输出](evidence/batch4/windows-python-full.log)、[退出码](evidence/batch4/windows-python-full.exit.txt)，1 |

最终 SSE 撤销返回 MCPError(-32001) 约 0.020 秒，随后旧 session 返回 Connection closed(-32000)；HTTP 有界错误约 0.005 秒。两者撤销后知识 REST 调用增量 0；错误后清理、撤销凭据重连拒绝、另一有效 Key 重新认证均通过。SDK 异常和 HTTP 状态独立记录，不把普通通用异常或测试总超时视为授权拒绝 PASS。

Windows 完整套件错误来自两个临时 cwd 在 TemporaryDirectory 清理前未恢复引发 WinError 32，以及实际符号链接创建缺少权限 WinError 1314（伴随清理错误）。没有跳过符号链接授权断言、启用系统权限或改写为 PASS。Linux 同一完整套件通过；Windows 文件路径相关回归仍需单独修复/准备平台能力。

## 完整命令

完整 Docker 参数、开始结束时间、各 exit code 在 [final results](evidence/batch4/linux/675615/results-final/results.json)、[Python/vet results](evidence/batch4/linux/675615/results-resume/results.json) 中。主要 Go 命令：

```text
go test -tags sqlite_fts5 -p 2 -count=1 ./internal/application/access/... ./internal/application/repository ./internal/application/repository/retriever/... ./internal/application/service/retriever ./internal/types ./internal/database ./internal/middleware ./internal/router ./internal/mcpserver
go test -tags sqlite_fts5 -race -p 2 -count=1 ./internal/application/access/... ./internal/application/repository ./internal/application/repository/retriever/sqlite ./internal/application/service/retriever ./internal/middleware ./internal/router
go vet -tags sqlite_fts5 ./internal/application/access/... ./internal/application/repository ./internal/application/repository/retriever/sqlite ./internal/application/service/retriever ./internal/types ./internal/database ./internal/middleware ./internal/router
python -m unittest discover -s mcp-server -p test_*.py -v
```

service/agent 用 `go test -tags sqlite_fts5 -c` 编译，完整执行测试二进制；Python 集成使用 `sqlite_fts5,knowhub_m3_integration`；PG 使用 `sqlite_fts5,knowhub_m3_postgres`、race 和真实临时 DB。没有执行仓库全部 `go test ./...`，不能宣称所有包通过。

## 必须保留的失败与证据限制

1. 第三批正式来源缺失 3033 的原 FAIL：`evidence/batch3/linux/7098d6/pg-live-cas.log`，按字节保留。
2. 第三批 SSE SDK 超时的原 FAIL：`evidence/batch3/linux/ce1cb5/python-http-sse-go-acl.log`，按字节保留。
3. 第四批修复前复现 `41e207`：真实 SSE 10 秒超时，exit=1。
4. `7f29b2` 的编排前缀校验错误，以及 `e95889/c88a65/675615` 早期 ENOSPC 中断，保留原始残缺 JSON/日志；未从缺失记录补造退出码，视为环境失败/未完整执行。
5. `675615/results-resume` 的 Go/race/service 因 tmpfs 未指定 exec 而失败，exit=1；保留原输出，纠正容器配置后完整重跑于 results-final，不放宽任何断言。
6. PG 重试的 leaf 日志使用既有固定文件名，保留最终 leaf；resume/final 各自编排 stdout、命令和退出码保留。独立 `2c1385` 故障恢复原始 leaf 也保留。未宣称重试间共享 leaf 为多个独立副本。

修复两个 P0 与完成 M3 是不同门禁：生产 PG 检索、实际服务/Handler、模型 HTTP spy、文件/CDN、Wiki、历史引用及来源 ACL 仍 HOLD/NOT_TESTED，见 [29](29_READ_SURFACE_COVERAGE_MATRIX.md)。没有将尚未完成的全套业务验收标绿。
