# M0/M1 测试记录与后续验收矩阵

状态含义：ALREADY_SUPPORTED=已有源码；IMPLEMENTED=本批交付；PASS/FAIL=实际命令结果；NOT_TESTED=未执行；BLOCKED=外部条件/工具链阻碍；NEEDS_DECISION=需业务或安全决策。静态阅读不算生产验收。

## 本批实际执行

环境：Windows PowerShell，Go 1.26.0，MinGW g++，Python 3.12.11（复用仓库外已有 venv）。所有 Go 测试运行在独立 worktree、审计 HEAD；不混入原工作树未跟踪测试。

| ID | 命令或操作 | 实际结果与限制 |
| --- | --- | --- |
| T01 | git branch --show-current；rev-parse HEAD；status --porcelain=v1；diff --stat | PASS；原工作树 17 tracked 修改及未跟踪文件已记录 |
| T02 | git ls-remote upstream HEAD refs/tags/*；fetch --no-tags upstream HEAD；merge-base；rev-list --left-right --count | PASS；固定 SHA 比较；未合并 |
| T03 | 受保护 SSH 运行 tools/production_readonly.py | PASS；只读版本、schema 和资源聚合；未执行业务读取/漏洞攻击 |
| T04 | Python 3.12 tools/build_local_evidence.py | PASS；31 个工具均映射 REST；5 个基线模块摘要；实际部署模块与本地一致 |
| T05 | `go test ./internal/application/access ./internal/types ./internal/middleware ./internal/mcpserver ./internal/application/repository -count=1` | 初次 BLOCKED：`__stack_chk_fail` 原生链接缺依赖；补进程内 `CGO_LDFLAGS=-O2 -g -lssp` 后执行结果见下行 |
| T06 | 同 T05，`GOSUMDB=sum.golang.org`、`CGO_LDFLAGS=-O2 -g -lssp` | access/types/mcpserver/repository PASS；middleware FAIL，唯一失败 TestMultipartFormCleanupRemovesDiskTemporaryFiles，未在其预期目录发现临时文件 |
| T07 | `go test ./internal/middleware -run 'TestAuthenticateUserMCP\|TestMCPEndpoint\|Test.*APIKey\|Test.*Portal\|Test.*KBAccess' -count=1`（Go -run 为正则表达式，实际参数去掉表格中的转义反斜杠） | PASS；个人身份/空间范围、MCP、API Key、Portal、KB 相关已有测试；未验证新增功能 |
| T08 | `go test ./internal/application/service ./internal/router ./internal/agent/tools -run 'Test.*(UserMCP\|APIKey\|Portal\|SharedAccess\|SharedScope\|ScopeRevalid\|SearchKnowledge\|HybridSearch\|WikiSearch\|KBFile\|MessageFile)' -count=1` | router PASS；service/agent 初次 BLOCKED：缺 Windows rstrtmgr 链接库；增加现有库路径后结果见 test-results.json |
| T09 | `go vet ./internal/application/access ./internal/types ./internal/middleware ./internal/mcpserver ./internal/application/repository` | PASS，最终使用现有原生库路径并捕获 vet 实际退出码 0，无诊断；环境见 evidence/test-results.json |
| T10 | `python -m unittest discover -s mcp-server -p test_mcp_transports.py -v` | FAIL：14 项中 13 PASS，1 HTTP readiness FAIL；认证隔离、tenant 透传、stdio 31 工具等通过 |
| T11 | `python docs/integration/knowhub-mcp-portal/tools/verify_python_http_windows.py` | PASS：只将原 HTTP smoke 中 curl `-o /dev/null` 换成 Windows os.devnull，原应用/测试不修改；1 项实际 HTTP 初始化通过。原完整套件仍记 FAIL |
| T12 | `python docs/integration/knowhub-mcp-portal/tools/verify_delivery.py` | PASS；13 报告、链接、Python 语法、盘点合计、实际部署模块摘要和 31 路由映射一致 |

T06 失败源码使用 TMPDIR，但 Windows os.TempDir 使用 TMP/TEMP；这是环境原因推断，不把未执行 Linux 结果写成 PASS。T11 通过支持 T10 的 `/dev/null` 平台不兼容解释。没有修改旧 Python MCP 工具或绕开授权断言。

Go 环境只在子进程设置 GOSUMDB/CGO_LDFLAGS，没有 go env -w 或修改系统工具链。完整 middleware、Linux race/集成测试、端到端客户端、PostgreSQL/SQLite 新策略迁移均未通过发布验收；本批无新迁移，不能要求其不存在的迁移测试 PASS。

## 后续合成验收主体

A=已认证普通员工且非目标空间成员；B=目标空间有效成员；C=无敏感文档 grant；D=有明确有效 grant；E=团队端点机器身份；F=无效/未认证客户端。不得用真实敏感文档或系统管理员 Key 模拟 A/C。所有下列项目当前 NOT_TESTED，属于 M2–M6 门禁。

| 编号 | 场景/主体 | 断言及证据 |
| --- | --- | --- |
| P01 | A + enterprise_retrieve + 审核过公共 KB | 非成员检索成功、无需 Organization；空间管理/原始文件下载仍拒绝 |
| P02 | A + 旧 Key | 无新增能力不读取跨 Portal 公共内容；旧私人 scope 正常 |
| P03 | Portal draft/archived/tenant disabled/deleted/temp KB | 不进入企业目标；状态撤销后下一请求拒绝公共分支 |
| P04 | 审核空间新普通 KB/doc；历史缺行；外部未知 ACL | 可信新建可继承；历史缺行/未知来源拒绝公开；客户端伪造 metadata 无效 |
| D01 | C 搜索敏感 canary | vector/keyword/grep/FAQ/graph/Wiki 均无正文/标题/片段；Rerank/LLM spy 输入从未出现 |
| D02 | C 猜 document_id/chunk_id/旧引用 | detail/batch/revisions/neighbor/image/preview/download/export/签名/短链拒绝 |
| D03 | D 读取明确授权文档 | 必须仍在个人 Key 范围内；撤销 grant/成员/Key/source ACL 后下一请求及缓存命中拒绝 |
| D04 | B/E + mcp_blocked；旧 Python + full_access Key | 都不能通过 REST/MCP 绕过；Web 受控读取单独验证 |
| D05 | 历史 Wiki/summary/cache/message refs | 来源依赖未知拒绝；收紧同步阻断读取，异步清理失败也不泄露 |
| D06 | 空/超大 allowed-doc 集、驱动不支持预过滤 | 空集不执行全库；上限不能 fail-open；不支持模式 HOLD |
| C01 | F：错误/过期/吊销 Key、停用账号 | 拒绝、无资源元信息，不建立 MCP 有效主体 |
| C02 | A Key + B user_id/任意 X-Tenant-ID | 不冒用身份；目标不存在或超 scope 拒绝 |
| C03 | user_mcp/tenant/platform/mcp_ 交叉替代 | 主体和路由互斥；个人不继承管理员/CanAccessAllTenants |
| C04 | E 允许 KB + 隐藏工具直调 | 精确范围；ask/写工具个人模式 tools/call 拒绝；团队原能力保留 |
| S01 | 多空间/同名 KB/不同 embedding/store | 真 tenant/KB/doc 引用、去重、不串库；异构 score 不直接比较 |
| S02 | 中文问英文资料/错误码/函数路径 | 标注集合上的 Recall@10/MRR/nDCG；以实测阈值决策 |
| S03 | timeout/单空间失败/全部失败/embedding fallback | truthful partial/failed 状态，不误称完整或零结果 |
| S04 | 并发用户、revocation、限流/轮换/断线 | 无 Caller/context 污染；请求超时终止，断线重连重验权限 |
| S05 | 性能与监控 | 冷热/单跨空间 P50/P95、error rate、scope-resolution 成本和驱动 fan-out 基线 |
| M01 | PostgreSQL/SQLite up/down/up + 旧数据 | 零历史自动公开、零旧 Key 扩权，FK/CHECK/CAS/索引兼容 |
| M02 | 三个真实客户端 + 新旧 API 参数映射 | 实际隔离环境连接记录；不能只改工具名 |

验收产物必须绑定候选 SHA、镜像 digest、DB migration、配置摘要、数据集版本、测试命令/退出码与角色。门禁不能用表格打勾代替可复现证据。

## 第二批结果（保留以上第一批原始记录）

第一批两项平台兼容问题已修复并实测：Windows middleware完整PASS，Windows Python transport14/14 PASS，Linux Python完整53/53 PASS。M2候选及双数据库迁移、Linux完整/race/静态检查的具体通过和剩余失败见 [17](17_SECOND_BATCH_DELIVERY.md)，原始命令和退出码见 [batch2证据](evidence/batch2)。旧FAIL不删除；service完整套件仍有1项Python运行时依赖断言FAIL，不能用新增企业能力测试PASS替代。

P01–P04、C01–C03、M01适用组件子场景对应 [14](14_M2_IMPLEMENTATION_AND_TESTS.md)；未接真实Hybrid/Go用户路由，M3文件/旧REST/衍生链路仍NOT_TESTED。不能把本批read Grant的合成通过称为生产文档ACL验收。
