# 第五批交付：核心读取子批

**M3 整体 HOLD；生产能力默认关闭；停止在 commit/push/merge/release/deploy 之前。** 本次按任务书允许的顺序交付容量门禁、Windows 回归和 Knowledge/Chunk 首批业务接入；真实生产检索、模型、衍生与文件完整验收留下一子批，不以局部通过宣称 M3 完成。

分报告：[33 容量门禁](33_BUILD_CAPACITY_GATE.md)、[34 Windows](34_WINDOWS_PYTHON_TEST_FIX.md)、[35 核心读取](35_M3_KNOWLEDGE_CHUNK_INTEGRATION.md)、[36 真实检索状态](36_M3_REAL_RETRIEVER_ACL.md)、[37 模型与资源边界](37_M3_MODEL_SECURITY_TESTS.md)。

## 候选与保留

worktree：`E:/code/WeKnora-worktrees/knowhub-mcp-b4-rc3-20261021`；branch：`audit/knowhub-mcp-b4-rc3-20261021`；基线 HEAD：`e62b17e2d305358467821aeff1cce63ce1d61254`。源码保持未提交；HEAD 不是当前代码版本。

完整候选包含 tracked 修改及全部未跟踪源码/报告/测试工具，每个文件独立 SHA256；打包排除 evidence 避免递归。候选与 archive hash 见 `evidence/batch5/candidates/*.json`。最后交付校验见 [delivery-validation.json](evidence/batch5/delivery-validation.json)，包含当前源码、测试输入指纹、本批相对第四批改动清单、所有实际命令、退出码和容量汇总。报告 prose 可以追加结果；runtime、测试、实际执行 harness 必须与最终测试候选逐文件一致。离线交付校验器不属于测试执行输入，其当前版本另记录于完整 source_files 和 artifact manifest。

第一至第四批 00～32 报告、原始第三批两个 FAIL、第四批 Windows FAIL 和原工作树快照全部保留。第四批冻结 manifest 的 **194 项**在本批开始和最终交付均逐项校验；最终另核对前三批保留清单的 **1,198 项**报告/证据/工具，不改写旧证据。

本批 runtime/测试改动共 26 个文件，另有 3 个本批执行/校验工具；新增与修改分类、旧 SHA256 和新 SHA256 见 [逐文件清单](evidence/batch5/change-inventory.json)，函数与作用见 [35](35_M3_KNOWLEDGE_CHUNK_INTEGRATION.md)。没有改动既有迁移 SQL、个人 Go MCP 或生产 Python 实例。

## 当前状态

| 类别 | 状态与实际边界 |
| --- | --- |
| IMPLEMENTED | 实际 Knowledge/Chunk service、五条既有 REST 路由、列表 SQL 预过滤、live Grant 输出复验；SQLite Key JSON scanner；Windows 测试 cwd 生命周期；容量门禁 |
| PASS | 最终 Linux 相关完整套件、授权场景 race、vet、Python 55 项、实际 REST + SDK HTTP/SSE、完整 service/agent，具体范围见末尾；不代表全仓库或全部 M3 |
| FAIL | Windows 完整 55 tests 仍 exit 1：1 个 WinError 1314；修复前 3 errors、所有中间 Linux FAIL 保留 |
| BLOCKED | 当前 Windows 进程缺真实 symlink 权限；未修改 OS 权限或跳过断言 |
| NOT_TESTED | 生产 ParadeDB 真实关键词/向量/Hybrid/grep、多 KB 正向检索；实际 Rerank/LLM 网络输入 spy；完整 Agent/stream/cache；多 Worker SSE；真实存储/CDN/source ACL；性能基线 |
| NEEDS_DECISION | 企业审核角色正式映射、可信员工资格来源、用户级 deny 语义；不得自动把 Portal 管理员/SystemAdmin/Space Owner 设为审核人 |

## 测试执行及已知失败

隔离 Linux：Go 1.26.7、CGO + SQLite 3.46.1 header、`sqlite_fts5`；受控 Python 3.11.16 / MCP SDK 2.2.0；固定镜像 ID、Docker Root、卷位置和可用空间见 [环境原输出](evidence/batch5/environment-and-capacity.log)。网络均 none；测试需要的公共域名仅以 `--add-host example.com:8.8.8.8` / `api.e2b.app:8.8.8.8` 固定离线解析，不开启真实外网、不关闭 SSRF 检查。

runner：`tools/batch5_linux.py`。每个命令记录容器参数、时间、退出码和完整输出，不把 shell launcher 成功当作测试通过。阶段 `focused` / `python` / `all` 使用各自唯一结果目录；all 包含相关包完整测试、相关新增授权场景 race、vet、Python 完整 55 项，以及 Python 3.11 运行的完整 service/agent 测试二进制和实际 REST + Python SDK tagged 集成。

中间候选失败包括夹具缺真实 NOT NULL、user_mcp tenant CHECK、SQLite Key capability scanner，以及 container 装配缺 import。均修改原因后重新运行，未修改 CHECK、未删授权断言。首次 all 的 handler parser 配置用例因离线域名解析返回 400，runner 加入既有公共测试域名映射；原 exit 1 保留。最终若仍有 FAIL，不可用 focused PASS 覆盖。

主要命令（精确 argv 以 results.json 为准）：

```text
go test -tags sqlite_fts5 -p 2 -count=1 <access/repository/retriever/database/types/middleware/router/handler/container/mcpserver packages>
go test -tags sqlite_fts5 -race -p 2 -count=1 <related packages> -run 'TestDocumentM5|TestDocumentM3|TestEnterpriseM2'
go vet -tags sqlite_fts5 <related packages>
python -m unittest discover -s mcp-server -p 'test_*.py' -v
go test -tags sqlite_fts5,knowhub_m5_integration -c -o <volume>/router-python.test ./internal/router
router-python.test -test.v -test.count=1 -test.run TestDocumentM5PythonActualREST
service.test -test.v -test.count=1
agent.test -test.v -test.count=1
```

race 覆盖列出的授权场景，**不是全仓库 race**；相关包完整测试也不是 `go test ./...`。PostgreSQL 迁移本批未改源码，第四批 P0-A 历史 PASS 保留，本批没有重新执行 PG 真实检索或将历史结果自动升级为新业务链 PASS。合成核心夹具实际执行正式 SQLite 0→3035，并断言 version=3035、dirty=false。

## 读取面及矩阵门禁

| 读取面 / 矩阵 | 本子批覆盖 | 仍需验收 |
| --- | --- | --- |
| Knowledge detail/list/batch，Chunk detail/list，D02/D03 | 真实 service/repository/Auth/Handler/route；ID、tenant、Key scope、集合/count、撤权与输出检查 | 复杂 shared/agent 私人路径、PG 同业务链 |
| P01/P02/P04，C01～C03 子场景 | A 合成员工非目标成员，B 成员，C 无 grant，D 有 grant，E full_access 机器，F revoked/missing Key；旧 Key 无公共扩权；伪造 Header 不生效 | 正式企业身份/审批业务资格；platform Key、share/agent 及所有账号状态的完整组合 |
| P03 | Portal 取消发布的核心 HTTP 回归，公共读取关闭后私人显式授权独立核验 | Portal archived、租户 disabled、临时 KB 的完整 HTTP 组合 |
| D04，旧 Python HTTP/SSE | 实际 REST tagged 测试结果另列；所有 Key 对 mcp_blocked 一致拒绝，Web 受控读取单测独立 | 多 Worker SSE / 生产 proxy、全部工具/文件链 |
| D01/D06，Search/Hybrid/grep | 相关未认证路径 HOLD；列表 DENY_ALL、allow-set 只收窄、上限拒绝 | 真实 ParadeDB 候选过滤及完整检索子矩阵 |
| D05，Wiki/FAQ/graph/summary/history/cache/file | HTTP 未认证路径拒绝；源/文件/任意 metadata/标签/历史 revision 不返回 | 持久化来源依赖、broker、外部旧 URL/CDN 撤权 |
| S01/S04 | 合成租户并发、Key/grant 撤销、旧 scope、响应前竞态 | 多模型多索引、真实搜索中撤权、异步流式/缓存重试 |
| S02/S03/S05 | NOT_TESTED | 召回质量、检索故障状态、性能及稳定性压测 |
| C04 | 本批未完整执行所有隐含/写工具直调场景 | 完整 tools/call 能力限制和路由枚举；不能从核心只读测试推导 PASS |

说明：表中未完整执行的矩阵行仍 HOLD/NOT_TESTED。匿名 `/r/:token` 和文件路径在试点隔离 router 前拒绝，不代表已撤销外部对象直链。正常生产 Python、租户、空间成员、现有 Key、Portal 状态没有任何变更。

## 下一子批与回滚

先在同一默认关闭边界内完成 [36](36_M3_REAL_RETRIEVER_ACL.md) 的真实 ParadeDB 预过滤与 [37](37_M3_MODEL_SECURITY_TESTS.md) 的 HTTP 模型 spy，再接 RAG/Agent、引用与缓存；随后衍生 ACL、文件 broker/旧链接/CDN/source ACL。多 Worker SSE 单列门禁，不能从单 Worker PASS 推导生产部署通过。

本批没有迁移新增、生产启用或凭据轮换。回滚保持默认关闭，逐文件审查 changed manifest 与第四批快照；保留旧工作树与所有证据，不使用 reset/clean 覆盖未提交成果。当前无需生产数据库 down。只有所有适用读取面真实验收且 NEEDS_DECISION 定稿后才能另行评估发布；本报告不批准发布。

## 最终实测结果

冻结测试候选：`c8f72624df0efcaae12779565f6486e60151846b405cf526f998b467f0aea038`。

当前全部 runtime、测试和实际执行工具逐文件匹配该候选；测试输入集合 SHA256：`8b80b3ed935a434b2f2ff6c123f9277f6dd950dfcda0cd605db36029557f39a4`。报告与离线交付校验器随后补充，其当前文件 SHA256 另保存在交付 manifest，不混称为重跑过的应用代码。

完整 [命令和退出码](evidence/batch5/linux/results-c8f72624df0efcaa-all/results.json)、[结束标记](evidence/batch5/linux/results-c8f72624df0efcaa-all/done.json)：

| 实际执行 | Exit | 结果 |
| --- | --- | --- |
| 容器环境初始化、Go version | 0 / 0 | PASS |
| Go 相关包完整套件（含 handler/container/router/迁移/各驱动包） | 0 | PASS；PG 包无测试文件，不能算实际 PG 检索 PASS |
| 相关授权场景 race | 0 | PASS；非全仓库 race，handler 在此 run pattern 下无匹配测试 |
| 相关包 vet | 0 | PASS |
| Linux Python 完整 55 tests | 0 | PASS，真实 symlink |
| 实际 REST + Python SDK tagged 构建 / 执行 | 0 / 0 | PASS；HTTP、SSE，单 Worker，SDK 2.2.0 |
| service 全量测试二进制构建 / Python 3.11 镜像执行 | 0 / 0 | PASS，含 tomllib 环境依赖及本批五组服务测试 |
| agent/tools 全量测试二进制构建 / 执行 | 0 / 0 | PASS，非本批真实 Agent ACL 正向接入证明 |
| Windows 完整 55 tests | 1 | FAIL / BLOCKED_OS_PRIVILEGE；1 个 WinError 1314 |

核心服务的新增测试包含实际 batch/neighbor/parent 查询及越权阻断、SQL epoch、过大输入拒绝、20 请求并发、旧内容与新审批竞态。router 完整套件包括本批五条实际正向路由、输出前撤权、Portal 取消发布及匿名 `/r/` 等入口 HOLD。实际 SDK 用的是同一真实核心路由栈，未替换成旧 adapter。

Key 撤销协议分项（仅该隔离候选、单 Worker）：

| 属性 | 实测 |
| --- | --- |
| Server auth 正确性 | PASS；Key 撤销后后端 Auth 拒绝，消息 POST 为 HTTP 401 |
| SSE 协议响应 | PASS；原流收到对应 RPC `-32001`，未把 RPC 留在无界等待 |
| SDK 错误传播及清理 | PASS；HTTP `MCPError -32603` 约 0.0061 秒，SSE `MCPError -32001` 约 0.0375 秒；退出旧客户端并以有效 A Key 重新建立会话成功 |
| 撤销后业务内容读取 | PASS；下一工具请求的 knowledge REST 调用增量均为 0，无正文返回 |

原始 HTTP 状态、SSE 事件、RPC ID、异常类型、时序和工具断言见 [实际 SDK 原日志](evidence/batch5/linux/results-c8f72624df0efcaa-all/router-python-sdk.log)。本批没有把 SDK 2.2 结果扩展成 SDK 2.3 新业务链或多 Worker PASS；第四批 2.3 组件证据继续保留。

容量门禁 PASS：专用数据盘卷观测高水位约 13.95 GiB，未触发阻断/ENOSPC；结束根盘约 52.1 GiB 可用；无历史文件删除。历史 1,198 项、第四批 194 项校验通过，新增 runtime/测试 26 文件及三个工具均列出独立 SHA256。所有中间 FAIL 与旧批次 FAIL 仍存在。**M3 全局 HOLD、真实检索/模型/衍生/文件尚未验收的结论不变。**
