# M2 默认关闭组件、迁移与合成验收

第二批实施，独立 worktree/分支保持不变。M1 当前决策见 13；本文件只对已实现组件和实测范围负责，不宣称 M3 全路径 ACL 或 M4 个人 Go MCP 已完成。原 M0 证据保留，不重新核查生产。

## 已实现源码

| 文件 | 具体行为 | 状态与边界 |
| --- | --- | --- |
| internal/types/enterprise_access.go | space/KB/doc 策略、资源/凭据安全投影、revision/resource_version | IMPLEMENTED；仅受控字段，无 Key/APIKeyHash/文档正文 |
| internal/types/tenant_api_key.go | 显式 enterprise_retrieve capability | IMPLEMENTED；不由 full_access 或 retrieve 推导 |
| internal/application/service/tenant_api_key.go；enterprise_capability_test.go | 个人 Key 显式能力与 retrieve 配对；默认能力保留 | IMPLEMENTED / 合成验证；旧 Key 不回填 |
| internal/application/access/enterprise_public.go | 必须显式启用；可信 caller/principal、企业资格 verifier、当前 Key/账号及政策查询；批准正文 Grant | IMPLEMENTED；零值/关闭/缺 verifier/store 全拒绝，无路由、DI 或通用 KB grant |
| internal/application/repository/enterprise_access.go | 当前 DB 状态读取；propose/approve CAS、优先 revoke；原 audit_logs 同事务；严格 JSONB/TEXT credential projection | IMPLEMENTED；当前审核人有效性检查，审批绑定元数据版本 |
| internal/application/access/document_filter.go/test | DENY_ALL、allow-set求交、目标 tenant/KB绑定、未知衍生来源拒绝 | IMPLEMENTED / 契约测试；真实 driver pipeline 尚未接入 |
| internal/application/repository/enterprise_access_test.go | A/B/C 合成资源、权限/撤销/并发/版本/审计/迁移与 PostgreSQL JSONB读取夹具 | IMPLEMENTED；明示哪些在 SQLite、哪些在隔离 PostgreSQL 执行 |
| migrations/versioned、sqlite/003034_enterprise_publication.{up,down}.sql | 三张表、拒绝默认、CHECK/FK、审核主体与版本约束 | IMPLEMENTED / 迁移实测；仅新表及其索引，无历史回填 DML |
| migrations/migration-map.json；internal/database/migration_*test.go | 登记 3034 双方言逻辑对等；实际 runner fresh/legacy/up/down/up；Windows file URL斜杠兼容 | IMPLEMENTED / 实测；immutable 历史不改，版本断言更新为新版本而非删除 |
| internal/middleware/multipart_cleanup_test.go | TMPDIR/TMP/TEMP 同指向隔离临时目录 | IMPLEMENTED / Windows+Linux完整 middleware、Linux race通过；原清理断言保留 |
| mcp-server/test_mcp_transports.py | 标准库 HTTP 请求、隔离动态端口、明确合成 shared测试模式；不依赖 curl/空设备路径 | IMPLEMENTED / Windows14项、Linux53项回归；未改生产 Python模块 |

完整逐文件变更与字节摘要由 evidence/batch2 中候选 manifest 与最终 change manifest 记录。没有新增前端、Portal业务路由、OA/飞书逻辑、生产部署或个人 Go MCP入口。

## 数据库与事务语义

仅新增 enterprise_space_policies、kb_publication_policies、knowledge_access_policies。无行与 pending_review 都不产生公共许可。审核记录复用 audit_logs，不新建角色/Token/组织/成员事实。所有状态变更记录 actor、target、scope、旧/新 state、revision、resource_version、原因、时间；audit失败回滚策略变更。

propose收紧到 pending_review并生成版本；approve必须期望 revision 精确匹配、仍为 pending、审核人不同于提出者、资源版本未变。revoke锁定当前行后递增，而不是要求客户端版本仍最新；因此与 approve竞争时最终收紧，旧审批不能复活。初次并发插入通过主键/ON CONFLICT与行数检测拒绝，不存在 last-write-wins 的旧批准覆盖。

审批绑定 tenant/KB/Knowledge 的更新时间、归属、KB文档集合及 chunk 的 ID/content_revision/更新时间/enabled/deleted元数据摘要。文档或分块版本变化、新增未知文档、源渠道异常均阻断 M2整库。未证明所有旧内容写路径都会可靠更新这些元数据，不能据此开放不受控生产写入或混合库；该审查属于 M3。

M2不实现未来普通资源自动继承hook，虽然策略可记录继承开关；不能把开关或缺行当 public。外部来源即使应用可读，在M2也不开放。员工私人受限许可与文档grants由M3实现；本组件不假装已覆盖这些路径。

## 合成矩阵的已验证与未验证范围

| 原矩阵 | 本批断言 | 结论范围 |
| --- | --- | --- |
| P01 | A非目标空间成员可读明确批准text doc；caller不变；没有成员行；ResolveKB Viewer/Admin仍拒绝；目标带真实Tenant/精确docIDs | 组件解析与批准正文读取已测试；真实Hybrid/Go MCP路由尚NOT_TESTED |
| P02 | 旧 retrieve-only Key无公共许可；当前DB撤销cap即使旧ctx仍有cap也拒绝 | 组件兼容已测试；客户端旧连接迁移不在本批 |
| P03 | Portal draft/archived、空间停用、KB删除/临时、未批准KB、Key吊销/到期、用户停用 | 下一次Authorize/既有Grant重新读取拒绝，已测试 |
| P04 | 历史无记录、新增无审批doc、外部feishu/gitlab、未知ACL、restricted/mcp_blocked/deny、文件修改 | 整库HOLD已测试；自动继承尚未实现，保持拒绝默认 |
| C01 | 无Principal/Key、未知KeyID、过期/撤销、停用账号 | 组件负例与既有middleware完整回归；没有新HTTP token入口可验收 |
| C02 | C携A的KeyID、Caller/Principal错配、目标不在已批准范围 | 拒绝已测试；原X-Tenant-ID规则由已有middleware回归，未来个人Go路由仍待实现 |
| C03 | tenant/platform FullAccess、mcp_endpoint主体、普通Web无Key、未启用组件 | 不得消费个人公共能力，已测试 |
| M01 | PostgreSQL空库实际3033源码链+003034 up/down/up、3033形状旧库夹具；SQLite真实项目runner fresh/legacy3027→3034→down→up | 双数据库实测，历史策略不自动批准、旧Key/原schema保持；不是生产执行 |
| M3准备 | 空授权不能退化全库、unsupported预过滤拒绝、求交不能扩大scope、未知衍生源拒绝、M2正文资源链接拒绝 | 合成接口契约已测试；真实driver/REST/文件/Wiki/LLM尚NOT_TESTED |

PostgreSQL还用独立连接池验证并发approve/revoke与事务审计；JSONB凭据、实际批准正文与Portal撤销的最终结果见原始 postgres-go-cas.log，不用SQLite结果代替。空数据库指先由冻结的生产源码迁移构建原有schema，再应用新增迁移；不使用生产数据、备份或账号。

## 测试命令、原始输出与失败保留

所有原始 Linux 日志/命令/退出码/时间在 [evidence/batch2/linux](evidence/batch2/linux)，Windows记录和测试汇总在 [evidence/batch2](evidence/batch2)。最终结论以机器记录和17报告为准，首轮FAIL/BLOCKED不覆盖。

执行过的主要命令：

```text
go test -count=1 ./internal/application/access ./internal/types ./internal/application/repository ./internal/middleware ./internal/mcpserver ./internal/router ./internal/application/service ./internal/agent/tools
go test -race -count=1 ./internal/application/access ./internal/application/repository ./internal/middleware ./internal/mcpserver
go test -count=1 ./internal/database
go test -race -count=1 -v ./internal/application/repository -run ^TestEnterpriseM2Postgres
go test -count=1 -v ./internal/application/service -run ^TestEnterpriseCapabilityIsExplicitAndRequiresRetrieve$
go vet ./internal/application/access ./internal/application/repository ./internal/database ./internal/application/service ./internal/types ./internal/middleware ./internal/mcpserver
python -m unittest discover -s mcp-server -p test_mcp_transports.py -v
python -m unittest discover -s mcp-server -p test_*.py -v
```

Linux工具与原始失败：首次缺docreader/docs依赖打包、宿主旧libstdc++、pip TLS问题；补全依赖后用既有Bookworm工具镜像与离线MCP镜像。原始完整service/agent在network=none时还暴露DNS与ensurepip依赖；使用明确DNS fixture和已有独立Python runtime复测，不关闭SSRF校验、不跳过授权断言。任何剩余失败仍按FAIL记录，不能用筛选单测的PASS替代完整套件。

Windows原始TMPDIR失败已通过设置TMP/TEMP解决；HTTP /dev/null问题通过移除外部curl依赖解决。新增迁移测试还发现原`file://E:\...`路径不兼容，改为URL斜杠；真实database完整回归已通过。原始首次输出保留。

## 已验证回滚及激活门禁

隔离PostgreSQL与SQLite均执行新迁移down再up，验证旧表和旧Key不变、没有自动公开记录。默认关闭组件和无路由意味着本批无需生产回滚。未来激活前仍需人工确认企业目录/发布角色、具体资源审批、M3全部读取路径及URL撤销、真实检索驱动与客户端、性能基线。

一旦敏感策略实际激活，禁止简单down或回退无ACL后端；应关闭新增公共入口、保留拒绝守卫及审计、受控处置已签URL和缓存。M2合成PASS不能作为生产发布或混合库开放批准。
