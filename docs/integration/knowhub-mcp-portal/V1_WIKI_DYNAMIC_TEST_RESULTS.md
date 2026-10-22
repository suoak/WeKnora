# 本候选测试矩阵与原始命令索引

候选 `c2f260317fc151fbab98d9e87b2fe5c9acbd903dd4be2e857e0326565d390d75`；业务源码清单指纹 `b702755c033328560bbb28802c93a3a1abce31a43352870b43d8f1fee2d74cdb`。RC3 HEAD不代表未提交源码。本报告PASS仅指明示的隔离范围；真实云端、正式身份和模型不在其内。

环境：Linux受控Go1.26、Python3.11+、Node22；PostgreSQL17.9/ParadeDB pg_search0.22.2/vector0.8.1；SQLite启用fts5。任意文档、向量、身份及飞书HTTP均为合成数据。离线单元容器有DNS夹具，不能据此认定真实域名/网络可用。原命令数组、运行时间、退出码和环境挂载均在对应results/done JSON，不包含密钥明文。

| 测试 | 结果/退出码 | 实际范围与证据 |
|---|---|---|
| 完整Go | PASS / 0 | `development-b702755c03332856/full-go.log`；`go test -tags sqlite_fts5 -p 2 -count=1 ./...`。无配置的可选集成子测试不等于已执行 |
| 完整相关包race | PASS / 0 | `extra-b702755c03332856/done.json`，6包完整race，25m预算，718秒；原10m超时保留 |
| vet / 编译 / 种子程序 | PASS / 0 | 同开发results，正式APP另用anydoc标签重建并实际启动 |
| 新增PG与race | PASS / 0 | `new-real-PG.log`、`new-real-PG-race.log`，真实候选前过滤、HOLD隔离、模型spy、请求中撤权、动态身份、Wiki版本/混合来源 |
| SQLite+PG正式迁移 | PASS / 0 | `formal-migrations.log`、`formal-tagged-migration-race.log`；新库、3033→3037、失败恢复、允许down/up和历史不扩权 |
| 最终APP集中正式迁移 | PASS / 0 | `acceptance-c2f260317fc151fb/formal-pg-migration-race.log`；不继承e53的FAIL为PASS，确实重跑 |
| 前端 | PASS / 0 | `build-c2f260317fc151fb/`：45项测试、vue-tsc、Vite build，全新同源构建 |
| 真实APP+Nginx装配 | PASS / 0 | `deploy-c2f260317fc151fb/`，关闭403、启用后未鉴权401，健康200，Schema3037无dirty |
| HTTP业务和权限 | PASS / 0 | `acceptance-c2f260317fc151fb/http-business.json`：八工具，4KB公共/双私人联合，A/B两用户，未来私人空间加入/撤销，旧fixed不扩权，Key轮换/撤销，Portal取消发布 |
| Portal实际管理路由 | PASS / 0 | `portal-dynamic-c2f260317fc151fb/result.json`：未发布拒绝、同Key发布后发现、新KB、新索引文档三检索、退发布且私人授权保留；非管理员发布403 |
| 大KB/Hybrid/维度 | PASS / 0 | `acceptance-c2f260317fc151fb/postgres-security-scale.log`，128/257/512/864/1001文档、3KB2租户、实际PG候选检索；1024为受控HTTP模型，正式模型未验收 |
| Wiki负向 | PASS / 0 | `wiki-negatives-c2f260317fc151fb/result.json`：归档、旧Writer、ABA、混合外国来源、缺证明拒绝，健康页面仍可读；缺index无写入另在HTTP/PG断言 |
| 浏览器业务闭环 | PASS / 0 | `browser-c2f260317fc151fb/attempt-*/result.json`及桌面/390px截图；原路由、name+∞、dynamic_all、复制TOML/JSON、本人重新查看加密Secret、轮换 |
| 实际Codex | PASS / 0 | `codex-c2f260317fc151fb/attempt-20261022T061522Z/`八工具与正文语义检查；`attempt-20261022T062033Z/`真实读取两飞书合成来源更新正文；实际CLI和复制的UI模板，非仅模板单测 |
| 飞书Wiki/Drive完整状态机 | PASS / 0 | `feishu-app-final-c2b-c2f260317fc151fb/`；真实APP/Worker/PG/Redis，更新、失败保旧、稳定KID、未变不重建、临时403HOLD、恢复、可信删除/撤权、REST部分结果、MCP与Python拒绝、旧HMAC拒绝、备份恢复 |
| 原子切换/并发/故障注入 | PASS / 0 | 集中 `feishu-canonical-atomic*.log`、`feishu-canonical-parser*.log`，普通和race均实际独立PG运行，旧Worker围栏与失败保旧；不是仅结果后过滤 |
| Python完整回归 | PASS / 0 | `python-full.log`，55项，受控Python3.11+；`python-sdk.log`真实HTTP/SSE与后端REST；飞书另有两transport源状态验证 |
| 完整service/agent回归 | PASS / 0（有原生SKIP） | 集中 `service-full.log`、`agent-full.log`；service日志34条SKIP（包含子项），涉及未配置真实应用/表格/生产形状克隆等，原逻辑保留，无删除授权断言 |
| 备份恢复 | PASS / 0 | `backup-restore.json`、`restore-check.log`；额外已撤权来源完整恢复及真实APP读拒绝见飞书结果 |
| 硬化兼容回滚 | PASS / 0 | `rollback-compatibility-c2f260317fc151fb/proof.json`，9项；`rollback-approval-c2f260317fc151fb.json`绑定镜像与证明SHA，不执行down |
| OA/SID、真实飞书Docx/Sheet/Bitable | NOT_TESTED / 环境缺口 | 已有代码与单元兼容保留，真实账号、真实应用未配置；离线APP跳过DuckDB扩展加载，不据此宣称真实Office表格解析通过 |
| 正式Embedding/生产索引 | NOT_TESTED / 环境缺口 | 正式服务1024返回、798/3584历史索引和真实模型绑定需正式环境 |
| WorkMate tools/call | FAIL / 暂缓 | 保留历史失败，不专项排查，不当成本候选发布前置功能 |
| 生产范围/积压/部署 | NOT_TESTED / 未授权 | 没有对生产库、凭据、开关、游标和网络执行写入；不声明生产17空间48KB已实际开放 |

性能：本候选HTTP业务39次调用中位约187ms；PG合成1001篇Hybrid 6样本p95约2983ms，3KB2租户约3109ms。原始25条规模指标见 `performance-c2f260317fc151fb.json`，共享构建主机且非持续负载，不能外推生产SLA。

必须保留的失败：e53正式迁移夹具FAIL、e53 Portal克隆序号FAIL、e53回滚DNS名称FAIL、新候选飞书首次初始化夹具序号FAIL（final-c2）。其后修正夹具/受控重试均使用单独证据；final-c2b执行真实Worker恢复残留来源后完成全流程。原始失败没有覆盖。Portal新Chunk克隆改为分配唯一seq_id，后续脚本使用真实序列避免与后续Worker插入冲突。此改动属于合成夹具，不更改产品授权/状态机。

所有读取证明、动态Key、Wiki、REST与检索权限PASS均限于列明边界。未知来源及无可信证明的历史Wiki继续HOLD，旧RAG/Agent未知范围继续保守拒绝。主动促进测试重试的控制器仅在合成项目按精确来源ID调用Asynq官方RunTask，不修改payload、授权、最大重试数；自然时间退避的完整墙钟周期未验收。

以下为机器生成的精确命令索引；原始stdout/stderr以日志文件为准。


### Development

- `prepare` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/prepare.log`

```sh
docker run --add-host example.com:8.8.8.8 --add-host api.e2b.app:8.8.8.8 --add-host oss-cn-hangzhou.aliyuncs.com:8.8.8.8 --add-host open.feishu.cn:8.8.8.8 --add-host api-inference.modelscope.cn:8.8.8.8 --add-host api.anthropic.com:8.8.8.8 --add-host api.deepseek.com:8.8.8.8 --add-host api.hunyuan.cloud.tencent.com:8.8.8.8 --add-host api.lkeap.cloud.tencent.com:8.8.8.8 --add-host api.longcat.chat:8.8.8.8 --add-host api.minimaxi.com:8.8.8.8 --add-host api.moonshot.ai:8.8.8.8 --add-host api.notion.com:8.8.8.8 --add-host api.novita.ai:8.8.8.8 --add-host api.openai.com:8.8.8.8 --add-host api.qnaigc.com:8.8.8.8 --add-host api.siliconflow.cn:8.8.8.8 --add-host api.xiaomimimo.com:8.8.8.8 --add-host ark.cn-beijing.volces.com:8.8.8.8 --add-host company.yuque.com:8.8.8.8 --add-host dashscope.aliyuncs.com:8.8.8.8 --add-host generativelanguage.googleapis.com:8.8.8.8 --add-host integrate.api.nvidia.com:8.8.8.8 --add-host open.bigmodel.cn:8.8.8.8 --add-host openrouter.ai:8.8.8.8 --add-host qianfan.baidubce.com:8.8.8.8 --add-host router.requesty.ai:8.8.8.8 --add-host weknora.weixin.qq.com:8.8.8.8 --rm --network none --name knowhub-next-development-b702755c0333 -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/results:/results -v knowhub-next-development-b702755c0333:/work -v knowhub-next-development-b702755c0333-socket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v feishu-incremental-1d533edfe339:/baseline-work:ro -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v knowhub-mcp-b7-20261021-102200-data:/prior -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/work/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/work/sqlite-headers' -e CGO_LDFLAGS=-L/native -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 sh -c 'mkdir -p /work/tmp && cp -R /baseline-work/sqlite-headers /work/sqlite-headers'
```

- `focused` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/focused.log`

```sh
docker run --add-host example.com:8.8.8.8 --add-host api.e2b.app:8.8.8.8 --add-host oss-cn-hangzhou.aliyuncs.com:8.8.8.8 --add-host open.feishu.cn:8.8.8.8 --add-host api-inference.modelscope.cn:8.8.8.8 --add-host api.anthropic.com:8.8.8.8 --add-host api.deepseek.com:8.8.8.8 --add-host api.hunyuan.cloud.tencent.com:8.8.8.8 --add-host api.lkeap.cloud.tencent.com:8.8.8.8 --add-host api.longcat.chat:8.8.8.8 --add-host api.minimaxi.com:8.8.8.8 --add-host api.moonshot.ai:8.8.8.8 --add-host api.notion.com:8.8.8.8 --add-host api.novita.ai:8.8.8.8 --add-host api.openai.com:8.8.8.8 --add-host api.qnaigc.com:8.8.8.8 --add-host api.siliconflow.cn:8.8.8.8 --add-host api.xiaomimimo.com:8.8.8.8 --add-host ark.cn-beijing.volces.com:8.8.8.8 --add-host company.yuque.com:8.8.8.8 --add-host dashscope.aliyuncs.com:8.8.8.8 --add-host generativelanguage.googleapis.com:8.8.8.8 --add-host integrate.api.nvidia.com:8.8.8.8 --add-host open.bigmodel.cn:8.8.8.8 --add-host openrouter.ai:8.8.8.8 --add-host qianfan.baidubce.com:8.8.8.8 --add-host router.requesty.ai:8.8.8.8 --add-host weknora.weixin.qq.com:8.8.8.8 --rm --network none --name knowhub-next-development-b702755c0333 -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/results:/results -v knowhub-next-development-b702755c0333:/work -v knowhub-next-development-b702755c0333-socket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v feishu-incremental-1d533edfe339:/baseline-work:ro -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v knowhub-mcp-b7-20261021-102200-data:/prior -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/work/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/work/sqlite-headers' -e CGO_LDFLAGS=-L/native -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5 -p 2 -count=1 -v ./internal/application/service ./internal/application/repository ./internal/mcpserver ./internal/middleware ./internal/handler -run 'TestV1Next|TestPortalM7|TestPortalM6PersonalDefaultClosed'
```

- `create-disposable-pg` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/create-disposable-pg.log`

```sh
docker run -d --name knowhub-next-dev-b702755c0333-pg --label knowhub.next=development --network none -v knowhub-next-dev-b702755c0333-pg-data:/var/lib/postgresql/data -v knowhub-next-development-b702755c0333-socket:/var/run/postgresql -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_DB=feishu_atomic_test paradedb/paradedb:v0.22.2-pg17
```

- `pg-version` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/pg-version.log`

```sh
docker exec knowhub-next-dev-b702755c0333-pg psql -U postgres -d feishu_atomic_test -Atc 'SELECT version();SELECT extname,extversion FROM pg_extension WHERE extname IN ('"'"'pg_search'"'"','"'"'vector'"'"');'
```

- `create-tagged-knowhub_m2_test` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/create-tagged-knowhub_m2_test.log`

```sh
docker exec knowhub-next-dev-b702755c0333-pg createdb -U postgres knowhub_m2_test
```

- `create-tagged-knowhub_m3_chain` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/create-tagged-knowhub_m3_chain.log`

```sh
docker exec knowhub-next-dev-b702755c0333-pg createdb -U postgres knowhub_m3_chain
```

- `create-tagged-knowhub_m3_fresh` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/create-tagged-knowhub_m3_fresh.log`

```sh
docker exec knowhub-next-dev-b702755c0333-pg createdb -U postgres knowhub_m3_fresh
```

- `create-tagged-knowhub_m3_empty` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/create-tagged-knowhub_m3_empty.log`

```sh
docker exec knowhub-next-dev-b702755c0333-pg createdb -U postgres knowhub_m3_empty
```

- `create-tagged-knowhub_m3_failure` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/create-tagged-knowhub_m3_failure.log`

```sh
docker exec knowhub-next-dev-b702755c0333-pg createdb -U postgres knowhub_m3_failure
```

- `new-real-PG` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/new-real-PG.log`

```sh
docker run --rm --network container:knowhub-next-dev-b702755c0333-pg --name knowhub-next-development-b702755c0333 -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/results:/results -v knowhub-next-development-b702755c0333:/work -v knowhub-next-development-b702755c0333-socket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v feishu-incremental-1d533edfe339:/baseline-work:ro -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v knowhub-mcp-b7-20261021-102200-data:/prior -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/work/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/work/sqlite-headers' -e CGO_LDFLAGS=-L/native -e 'FEISHU_LEGACY_TEST_DSN=postgres://postgres@127.0.0.1:5432/feishu_atomic_test?sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5 -p 2 -count=1 -v ./internal/router -run '^TestV1NextPostgres'
```

- `new-real-PG-race` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/new-real-PG-race.log`

```sh
docker run --rm --network container:knowhub-next-dev-b702755c0333-pg --name knowhub-next-development-b702755c0333 -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/results:/results -v knowhub-next-development-b702755c0333:/work -v knowhub-next-development-b702755c0333-socket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v feishu-incremental-1d533edfe339:/baseline-work:ro -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v knowhub-mcp-b7-20261021-102200-data:/prior -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/work/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/work/sqlite-headers' -e CGO_LDFLAGS=-L/native -e 'FEISHU_LEGACY_TEST_DSN=postgres://postgres@127.0.0.1:5432/feishu_atomic_test?sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -race -tags sqlite_fts5 -p 2 -count=1 -v ./internal/router -run '^TestV1NextPostgres'
```

- `formal-migrations` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/formal-migrations.log`

```sh
docker run --rm --network container:knowhub-next-dev-b702755c0333-pg --name knowhub-next-development-b702755c0333 -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/results:/results -v knowhub-next-development-b702755c0333:/work -v knowhub-next-development-b702755c0333-socket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v feishu-incremental-1d533edfe339:/baseline-work:ro -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v knowhub-mcp-b7-20261021-102200-data:/prior -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/work/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/work/sqlite-headers' -e CGO_LDFLAGS=-L/native -e 'FEISHU_LEGACY_TEST_DSN=postgres://postgres@127.0.0.1:5432/feishu_atomic_test?sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5 -p 2 -count=1 -v ./internal/application/repository -run '^TestFeishuLegacyFormalMigrationChain$'
```

- `formal-tagged-migration-race` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/formal-tagged-migration-race.log`

```sh
docker run --rm --network container:knowhub-next-dev-b702755c0333-pg --name knowhub-next-development-b702755c0333 -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/results:/results -v knowhub-next-development-b702755c0333:/work -v knowhub-next-development-b702755c0333-socket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v feishu-incremental-1d533edfe339:/baseline-work:ro -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v knowhub-mcp-b7-20261021-102200-data:/prior -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/work/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/work/sqlite-headers' -e CGO_LDFLAGS=-L/native -e 'FEISHU_LEGACY_TEST_DSN=postgres://postgres@127.0.0.1:5432/feishu_atomic_test?sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -race -tags sqlite_fts5,knowhub_m3_postgres -p 2 -count=1 -v ./internal/application/repository -run '^TestEnterpriseM2Postgres|^TestDocumentM3Postgres.*MigrationRunner$'
```

- `database` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/database.log`

```sh
docker run --rm --network container:knowhub-next-dev-b702755c0333-pg --name knowhub-next-development-b702755c0333 -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/results:/results -v knowhub-next-development-b702755c0333:/work -v knowhub-next-development-b702755c0333-socket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v feishu-incremental-1d533edfe339:/baseline-work:ro -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v knowhub-mcp-b7-20261021-102200-data:/prior -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/work/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/work/sqlite-headers' -e CGO_LDFLAGS=-L/native -e 'FEISHU_LEGACY_TEST_DSN=postgres://postgres@127.0.0.1:5432/feishu_atomic_test?sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5 -p 2 -count=1 -v ./internal/database
```

- `race-new` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/race-new.log`

```sh
docker run --rm --network container:knowhub-next-dev-b702755c0333-pg --name knowhub-next-development-b702755c0333 -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/results:/results -v knowhub-next-development-b702755c0333:/work -v knowhub-next-development-b702755c0333-socket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v feishu-incremental-1d533edfe339:/baseline-work:ro -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v knowhub-mcp-b7-20261021-102200-data:/prior -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/work/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/work/sqlite-headers' -e CGO_LDFLAGS=-L/native -e 'FEISHU_LEGACY_TEST_DSN=postgres://postgres@127.0.0.1:5432/feishu_atomic_test?sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -race -tags sqlite_fts5 -p 2 -count=1 ./internal/application/service ./internal/mcpserver ./internal/application/repository -run TestV1Next
```

- `vet` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/vet.log`

```sh
docker run --rm --network container:knowhub-next-dev-b702755c0333-pg --name knowhub-next-development-b702755c0333 -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/results:/results -v knowhub-next-development-b702755c0333:/work -v knowhub-next-development-b702755c0333-socket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v feishu-incremental-1d533edfe339:/baseline-work:ro -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v knowhub-mcp-b7-20261021-102200-data:/prior -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/work/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/work/sqlite-headers' -e CGO_LDFLAGS=-L/native -e 'FEISHU_LEGACY_TEST_DSN=postgres://postgres@127.0.0.1:5432/feishu_atomic_test?sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go vet -tags sqlite_fts5 ./internal/application/access ./internal/application/repository ./internal/application/service ./internal/mcpserver ./internal/middleware ./internal/router
```

- `full-go` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/full-go.log`

```sh
docker run --add-host example.com:8.8.8.8 --add-host api.e2b.app:8.8.8.8 --add-host oss-cn-hangzhou.aliyuncs.com:8.8.8.8 --add-host open.feishu.cn:8.8.8.8 --add-host api-inference.modelscope.cn:8.8.8.8 --add-host api.anthropic.com:8.8.8.8 --add-host api.deepseek.com:8.8.8.8 --add-host api.hunyuan.cloud.tencent.com:8.8.8.8 --add-host api.lkeap.cloud.tencent.com:8.8.8.8 --add-host api.longcat.chat:8.8.8.8 --add-host api.minimaxi.com:8.8.8.8 --add-host api.moonshot.ai:8.8.8.8 --add-host api.notion.com:8.8.8.8 --add-host api.novita.ai:8.8.8.8 --add-host api.openai.com:8.8.8.8 --add-host api.qnaigc.com:8.8.8.8 --add-host api.siliconflow.cn:8.8.8.8 --add-host api.xiaomimimo.com:8.8.8.8 --add-host ark.cn-beijing.volces.com:8.8.8.8 --add-host company.yuque.com:8.8.8.8 --add-host dashscope.aliyuncs.com:8.8.8.8 --add-host generativelanguage.googleapis.com:8.8.8.8 --add-host integrate.api.nvidia.com:8.8.8.8 --add-host open.bigmodel.cn:8.8.8.8 --add-host openrouter.ai:8.8.8.8 --add-host qianfan.baidubce.com:8.8.8.8 --add-host router.requesty.ai:8.8.8.8 --add-host weknora.weixin.qq.com:8.8.8.8 --rm --network none --name knowhub-next-development-b702755c0333 -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/results:/results -v knowhub-next-development-b702755c0333:/work -v knowhub-next-development-b702755c0333-socket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v feishu-incremental-1d533edfe339:/baseline-work:ro -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v knowhub-mcp-b7-20261021-102200-data:/prior -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/work/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/work/sqlite-headers' -e CGO_LDFLAGS=-L/native -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5 -p 2 -count=1 ./...
```

- `build` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/build.log`

```sh
docker run --rm --network container:knowhub-next-dev-b702755c0333-pg --name knowhub-next-development-b702755c0333 -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/results:/results -v knowhub-next-development-b702755c0333:/work -v knowhub-next-development-b702755c0333-socket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v feishu-incremental-1d533edfe339:/baseline-work:ro -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v knowhub-mcp-b7-20261021-102200-data:/prior -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/work/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/work/sqlite-headers' -e CGO_LDFLAGS=-L/native -e 'FEISHU_LEGACY_TEST_DSN=postgres://postgres@127.0.0.1:5432/feishu_atomic_test?sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go build -tags sqlite_fts5 -o /work/WeKnora ./cmd/server
```

- `seed-compile` exit=0; log: `evidence/v1-wiki-dynamic/development-b702755c03332856/seed-compile.log`

```sh
docker run --rm --network container:knowhub-next-dev-b702755c0333-pg --name knowhub-next-development-b702755c0333 -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/results:/results -v knowhub-next-development-b702755c0333:/work -v knowhub-next-development-b702755c0333-socket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v feishu-incremental-1d533edfe339:/baseline-work:ro -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v knowhub-mcp-b7-20261021-102200-data:/prior -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/work/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/work/sqlite-headers' -e CGO_LDFLAGS=-L/native -e 'FEISHU_LEGACY_TEST_DSN=postgres://postgres@127.0.0.1:5432/feishu_atomic_test?sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5,knowhub_v1_deployed -c -o /work/seed.test ./internal/router
```


### Frozen build

- `build-init` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/build-init.log`

```sh
docker run --rm --label knowhub.v1=build --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e CGO_LDFLAGS=-L/native -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 sh -c 'mkdir -p /v1/tmp /v1/sqlite-headers; cp /home/aqyfzx/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.24/sqlite3-binding.h /v1/sqlite-headers/sqlite3.h; cp /home/aqyfzx/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.24/sqlite3ext.h /v1/sqlite-headers/'
```

- `app-native-build` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/app-native-build.log`

```sh
docker run --rm --label knowhub.v1=build --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e CGO_LDFLAGS=-L/native -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go build -tags sqlite_fts5,anydoc -ldflags '-X github.com/Tencent/WeKnora/internal/handler.Version=V1-candidate -X github.com/Tencent/WeKnora/internal/handler.CommitID=worktree-c2f260317fc151fb -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn' -o /artifacts/WeKnora ./cmd/server
```

- `seed-build` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/seed-build.log`

```sh
docker run --rm --label knowhub.v1=build --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e CGO_LDFLAGS=-L/native -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5,anydoc,knowhub_v1_deployed -c -o /artifacts/seed.test ./internal/router
```

- `synthetic-retry-controller-build` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/synthetic-retry-controller-build.log`

```sh
docker run --rm --label knowhub.v1=build --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e CGO_LDFLAGS=-L/native -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go build -o /artifacts/retry-control /artifacts/retry-control.go
```

- `frontend-source` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/frontend-source.log`

```sh
docker run --rm --label knowhub.v1=build --network bridge -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source/frontend:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -e NODE_OPTIONS=--max-old-space-size=4096 -e VITE_IS_DOCKER=true -e VITE_FRONTEND_COMMIT=worktree-c2f260317fc151fb -w /v1 node:22-bookworm sh -c 'mkdir -p /v1/frontend; cp -a /source/. /v1/frontend/'
```

- `lockfile-cache-import` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/lockfile-cache-import.log`

```sh
docker run --rm --label knowhub.v1=build --network bridge -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source/frontend:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -e NODE_OPTIONS=--max-old-space-size=4096 -e VITE_IS_DOCKER=true -e VITE_FRONTEND_COMMIT=worktree-c2f260317fc151fb -w /v1/frontend node:22-bookworm npm cache add /artifacts/xlsx-lockfile.tgz --cache /v1/npm-cache
```

- `verified-npm-cache-copy` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/verified-npm-cache-copy.log`

```sh
docker run --rm --network none -v knowhub-v1-feishu-dev-985bdcb168c46ef5:/previous:ro -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 node:22-bookworm sh -c 'mkdir -p /v1/npm-cache; cp -a /previous/npm-cache/. /v1/npm-cache/'
```

- `frontend-dependencies` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/frontend-dependencies.log`

```sh
docker run --rm --label knowhub.v1=build --network bridge -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source/frontend:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -e NODE_OPTIONS=--max-old-space-size=4096 -e VITE_IS_DOCKER=true -e VITE_FRONTEND_COMMIT=worktree-c2f260317fc151fb -w /v1/frontend node:22-bookworm npm ci --prefer-offline --cache /v1/npm-cache --registry=https://registry.npmmirror.com
```

- `frontend-tests` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/frontend-tests.log`

```sh
docker run --rm --label knowhub.v1=build --network bridge -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source/frontend:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -e NODE_OPTIONS=--max-old-space-size=4096 -e VITE_IS_DOCKER=true -e VITE_FRONTEND_COMMIT=worktree-c2f260317fc151fb -w /v1/frontend node:22-bookworm node node_modules/tsx/dist/cli.mjs --test src/components/portal/publicationWorkflow.test.ts src/views/integrations/personalMcpConfig.test.ts src/views/integrations/personalMcpPresentation.test.ts src/views/integrations/mcpConfig.test.ts src/views/integrations/mcpAccessEditor.test.ts src/views/integrations/mcpAccessLifecycle.test.ts src/views/integrations/mcpAccessCenter.test.ts src/views/integrations/mcpAccessScope.test.ts src/views/portal/portalContracts.test.ts
```

- `frontend-types` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/frontend-types.log`

```sh
docker run --rm --label knowhub.v1=build --network bridge -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source/frontend:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -e NODE_OPTIONS=--max-old-space-size=4096 -e VITE_IS_DOCKER=true -e VITE_FRONTEND_COMMIT=worktree-c2f260317fc151fb -w /v1/frontend node:22-bookworm node node_modules/vue-tsc/bin/vue-tsc.js --build --pretty false
```

- `frontend-build` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/frontend-build.log`

```sh
docker run --rm --label knowhub.v1=build --network bridge -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source/frontend:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -e NODE_OPTIONS=--max-old-space-size=4096 -e VITE_IS_DOCKER=true -e VITE_FRONTEND_COMMIT=worktree-c2f260317fc151fb -w /v1/frontend node:22-bookworm node node_modules/vite/bin/vite.js build --configLoader runner --outDir /artifacts/dist
```

- `runtime-material` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/runtime-material.log`

```sh
docker image inspect knowhub-rc3/app:0.8.0-oa-feishu-rc3-1e2ded5b6 nginx:1.30.3-alpine node:22-bookworm
```

- `app-image-runtime` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/app-image-runtime.log`

```sh
docker build --network none --pull=false -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/image-context/Dockerfile.app -t knowhub-v1-wiki-dynamic/app:c2f260317fc151fb /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/image-context
```

- `frontend-image` exit=0; log: `evidence/v1-wiki-dynamic/build-c2f260317fc151fb/frontend-image.log`

```sh
docker build --network none --pull=false -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/image-context/Dockerfile.frontend -t knowhub-v1-wiki-dynamic/frontend:c2f260317fc151fb /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/image-context
```


### Independent deployment

- `compose-validate` exit=0; log: `evidence/v1-wiki-dynamic/deploy-c2f260317fc151fb/compose-validate.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml config --quiet
```

- `startup-disabled-edge` exit=0; log: `evidence/v1-wiki-dynamic/deploy-c2f260317fc151fb/startup-disabled-edge.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml up -d --wait --wait-timeout 180
```

- `nginx-check` exit=0; log: `evidence/v1-wiki-dynamic/deploy-c2f260317fc151fb/nginx-check.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml exec -T frontend nginx -t
```

- `schema-disabled` exit=0; log: `evidence/v1-wiki-dynamic/deploy-c2f260317fc151fb/schema-disabled.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml exec -T postgres psql -U postgres -d knowhub_v1 -Atc 'SELECT version,dirty FROM schema_migrations'
```

- `postgres-engine-version` exit=0; log: `evidence/v1-wiki-dynamic/deploy-c2f260317fc151fb/postgres-engine-version.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml exec -T postgres psql -U postgres -d knowhub_v1 -Atc 'SHOW server_version; SELECT extname,extversion FROM pg_extension WHERE extname IN ('"'"'pg_search'"'"','"'"'vector'"'"') ORDER BY extname'
```

- `seed-synthetic` exit=0; log: `evidence/v1-wiki-dynamic/deploy-c2f260317fc151fb/seed-synthetic.log`

```sh
docker run --rm --network knowhub-v1-wiki-dynamic-c2f260317fc1_isolated --env-file /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/private/app.env -e KNOWHUB_V1_SEED=SYNTHETIC_V1_ONLY -e KNOWHUB_V1_ARTIFACTS=/artifacts -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -w /source --entrypoint /artifacts/seed.test knowhub-v1-wiki-dynamic/app:c2f260317fc151fb -test.v -test.count=1 -test.run '^TestPortalV1SeedDeployed$'
```

- `seed-synthetic-wiki` exit=0; log: `evidence/v1-wiki-dynamic/deploy-c2f260317fc151fb/seed-synthetic-wiki.log`

```sh
docker run --rm --network knowhub-v1-wiki-dynamic-c2f260317fc1_isolated --env-file /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/private/app.env -e KNOWHUB_V1_SEED=SYNTHETIC_V1_ONLY -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts --entrypoint /artifacts/seed.test knowhub-v1-wiki-dynamic/app:c2f260317fc151fb -test.v -test.count=1 -test.run '^TestV1NextWikiSeedDeployed$'
```

- `startup-enabled` exit=0; log: `evidence/v1-wiki-dynamic/deploy-c2f260317fc151fb/startup-enabled.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml up -d --force-recreate --wait --wait-timeout 180 app frontend
```

- `containers` exit=0; log: `evidence/v1-wiki-dynamic/deploy-c2f260317fc151fb/containers.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml ps --format json
```


### Concentrated acceptance

- `future-private-fixture` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/future-private-fixture.log`

```sh
docker run --rm --network knowhub-v1-wiki-dynamic-c2f260317fc1_isolated --env-file /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/private/app.env -e KNOWHUB_V1_SEED=SYNTHETIC_V1_ONLY -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts --entrypoint /artifacts/seed.test knowhub-v1-wiki-dynamic/app:c2f260317fc151fb -test.v -test.run '^TestV1NextDynamicPrivateFixtureDeployed$'
```

- `create-knowhub_m6` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/create-knowhub_m6.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml exec -T postgres psql -U postgres -d postgres -c 'CREATE DATABASE knowhub_m6'
```

- `create-knowhub_m2_test` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/create-knowhub_m2_test.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml exec -T postgres psql -U postgres -d postgres -c 'CREATE DATABASE knowhub_m2_test'
```

- `create-knowhub_m3_chain` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/create-knowhub_m3_chain.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml exec -T postgres psql -U postgres -d postgres -c 'CREATE DATABASE knowhub_m3_chain'
```

- `create-knowhub_m3_fresh` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/create-knowhub_m3_fresh.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml exec -T postgres psql -U postgres -d postgres -c 'CREATE DATABASE knowhub_m3_fresh'
```

- `create-knowhub_m3_empty` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/create-knowhub_m3_empty.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml exec -T postgres psql -U postgres -d postgres -c 'CREATE DATABASE knowhub_m3_empty'
```

- `create-knowhub_m3_failure` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/create-knowhub_m3_failure.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml exec -T postgres psql -U postgres -d postgres -c 'CREATE DATABASE knowhub_m3_failure'
```

- `create-feishu_atomic_test` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/create-feishu_atomic_test.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml exec -T postgres psql -U postgres -d postgres -c 'CREATE DATABASE feishu_atomic_test'
```

- `postgres-security-scale` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/postgres-security-scale.log`

```sh
docker run --rm --label knowhub.v1=acceptance --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/work/results -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/work/pgsocket -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e 'KNOWHUB_M6_POSTGRES_DSN=host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -e KNOWHUB_M7_PLAN_DIR=/work/results/pg-plans -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5,knowhub_m6_postgres -p 2 -count=1 -v ./internal/router -timeout 18m -run 'TestPortalM6RealPostgresPersonal|TestPortalM7RealPostgresScale|TestPortalM7PublishedDynamicRealPostgres|TestPortalV1LinksAndIndexDimensionsPostgres'
```

- `postgres-feishu-ingress` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/postgres-feishu-ingress.log`

```sh
docker run --rm --label knowhub.v1=acceptance --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/work/results -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/work/pgsocket -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e 'KNOWHUB_M6_POSTGRES_DSN=host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -e KNOWHUB_M7_PLAN_DIR=/work/results/pg-plans -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5,knowhub_m6_postgres -p 2 -count=1 -v ./internal/application/service -timeout 10m -run '^TestPortalM7PublishedFeishuIngressPostgres$'
```

- `feishu-canonical-atomic` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/feishu-canonical-atomic.log`

```sh
docker run --rm --label knowhub.v1=acceptance --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/work/results -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/work/pgsocket -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e 'KNOWHUB_M6_POSTGRES_DSN=host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -e KNOWHUB_M7_PLAN_DIR=/work/results/pg-plans -e 'FEISHU_LEGACY_TEST_DSN=postgresql://postgres@/feishu_atomic_test?host=%2Fwork%2Fpgsocket&sslmode=disable' -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5 -p 2 -count=1 -v ./internal/application/repository -timeout 15m -run '^TestFeishuLegacyAtomicPostgres$'
```

- `feishu-canonical-parser` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/feishu-canonical-parser.log`

```sh
docker run --rm --label knowhub.v1=acceptance --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/work/results -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/work/pgsocket -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e 'KNOWHUB_M6_POSTGRES_DSN=host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -e KNOWHUB_M7_PLAN_DIR=/work/results/pg-plans -e 'FEISHU_LEGACY_TEST_DSN=postgresql://postgres@/feishu_atomic_test?host=%2Fwork%2Fpgsocket&sslmode=disable' -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5 -p 2 -count=1 -v ./internal/application/service -timeout 15m -run '^TestFeishuLegacyRealParserEmbeddingPostgres$'
```

- `feishu-canonical-atomic-race` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/feishu-canonical-atomic-race.log`

```sh
docker run --rm --label knowhub.v1=acceptance --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/work/results -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/work/pgsocket -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e 'KNOWHUB_M6_POSTGRES_DSN=host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -e KNOWHUB_M7_PLAN_DIR=/work/results/pg-plans -e 'FEISHU_LEGACY_TEST_DSN=postgresql://postgres@/feishu_atomic_test?host=%2Fwork%2Fpgsocket&sslmode=disable' -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -race -tags sqlite_fts5 -p 2 -count=1 -v ./internal/application/repository -timeout 15m -run '^TestFeishuLegacyAtomicPostgres$'
```

- `feishu-canonical-parser-race` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/feishu-canonical-parser-race.log`

```sh
docker run --rm --label knowhub.v1=acceptance --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/work/results -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/work/pgsocket -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e 'KNOWHUB_M6_POSTGRES_DSN=host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -e KNOWHUB_M7_PLAN_DIR=/work/results/pg-plans -e 'FEISHU_LEGACY_TEST_DSN=postgresql://postgres@/feishu_atomic_test?host=%2Fwork%2Fpgsocket&sslmode=disable' -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -race -tags sqlite_fts5 -p 2 -count=1 -v ./internal/application/service -timeout 15m -run '^TestFeishuLegacyRealParserEmbeddingPostgres$'
```

- `formal-pg-migration-race` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/formal-pg-migration-race.log`

```sh
docker run --rm --label knowhub.v1=acceptance --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/work/results -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/work/pgsocket -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e 'KNOWHUB_M6_POSTGRES_DSN=host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -e KNOWHUB_M7_PLAN_DIR=/work/results/pg-plans -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -race -tags sqlite_fts5,knowhub_m3_postgres -p 2 -count=1 -v ./internal/application/repository -run '^TestEnterpriseM2Postgres|^TestDocumentM3Postgres.*MigrationRunner$'
```

- `python-full` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/python-full.log`

```sh
docker run --rm --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -w /source -e WEKNORA_API_KEY= --entrypoint python weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95 -m unittest discover -s mcp-server -p 'test_*.py' -v
```

- `python-sdk-build` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/python-sdk-build.log`

```sh
docker run --rm --label knowhub.v1=acceptance --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/work/results -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/work/pgsocket -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e 'KNOWHUB_M6_POSTGRES_DSN=host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -e KNOWHUB_M7_PLAN_DIR=/work/results/pg-plans -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5,knowhub_m5_integration -c -o /artifacts/python-sdk.test ./internal/router
```

- `python-sdk` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/python-sdk.log`

```sh
docker run --rm --network none -v /home/aqyfzx/go/pkg/mod/github.com/yanyiwu/gojieba@v1.4.7/deps/cppjieba/dict:/testdict:ro -e JIEBA_DICT_DIR=/testdict -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -w /source/internal/router -e WEKNORA_API_KEY= --entrypoint /artifacts/python-sdk.test weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95 -test.v -test.count=1 -test.run TestDocumentM5PythonActualREST
```

- `regression-redis` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/regression-redis.log`

```sh
docker run -d --name knowhub-v1-wiki-dynamic-c2f260317fc1-regression-redis --restart always --label knowhub.v1=acceptance --network knowhub-v1-wiki-dynamic-c2f260317fc1_isolated --network-alias knowhub-p1-dispatch-redis-20261018 redis:7.0-alpine redis-server --save '' --appendonly no
```

- `service-build` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/service-build.log`

```sh
docker run --rm --label knowhub.v1=acceptance --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/work/results -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/work/pgsocket -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e 'KNOWHUB_M6_POSTGRES_DSN=host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -e KNOWHUB_M7_PLAN_DIR=/work/results/pg-plans -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5 -c -o /artifacts/service.test ./internal/application/service
```

- `service-full` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/service-full.log`

```sh
docker run --rm --network knowhub-v1-wiki-dynamic-c2f260317fc1_isolated -v /home/aqyfzx/go/pkg/mod/github.com/yanyiwu/gojieba@v1.4.7/deps/cppjieba/dict:/testdict:ro -e JIEBA_DICT_DIR=/testdict -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/work/pgsocket --add-host example.com:8.8.8.8 --add-host api.e2b.app:8.8.8.8 -w /source/internal/application/service -e WEKNORA_API_KEY= -e 'SYNC_EXECUTION_POSTGRES_DSN=postgresql://postgres@/knowhub_m6?host=%2Fwork%2Fpgsocket&sslmode=disable' -e REQUIRE_SYNC_EXECUTION_POSTGRES=1 -e FEISHU_DISPATCH_REDIS_ADDR=knowhub-p1-dispatch-redis-20261018:6379 --entrypoint /artifacts/service.test weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95 -test.v -test.count=1
```

- `agent-build` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/agent-build.log`

```sh
docker run --rm --label knowhub.v1=acceptance --network none -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/work/results -v knowhub-v1-wiki-dynamic-dev-c2f260317fc151fb:/v1 -v knowhub-mcp-b7-20261021-102200-data:/prior -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/work/pgsocket -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/v1/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers' -e 'KNOWHUB_M6_POSTGRES_DSN=host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable' -e 'KNOWHUB_M2_TEST_POSTGRES_DSN=host=/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 user=postgres dbname=knowhub_m2_test sslmode=disable' -e KNOWHUB_M7_PLAN_DIR=/work/results/pg-plans -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -tags sqlite_fts5 -c -o /artifacts/agent.test ./internal/agent/tools
```

- `agent-full` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/agent-full.log`

```sh
docker run --rm --network knowhub-v1-wiki-dynamic-c2f260317fc1_isolated -v /home/aqyfzx/go/pkg/mod/github.com/yanyiwu/gojieba@v1.4.7/deps/cppjieba/dict:/testdict:ro -e JIEBA_DICT_DIR=/testdict -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/artifacts:/artifacts -v knowhub-v1-wiki-dynamic-c2f260317fc1_pgsocket:/work/pgsocket --add-host example.com:8.8.8.8 --add-host api.e2b.app:8.8.8.8 -w /source/internal/agent/tools -e WEKNORA_API_KEY= -e 'SYNC_EXECUTION_POSTGRES_DSN=postgresql://postgres@/knowhub_m6?host=%2Fwork%2Fpgsocket&sslmode=disable' -e REQUIRE_SYNC_EXECUTION_POSTGRES=1 -e FEISHU_DISPATCH_REDIS_ADDR=knowhub-p1-dispatch-redis-20261018:6379 --entrypoint /artifacts/agent.test weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95 -test.v -test.count=1
```

- `regression-redis-stop` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/regression-redis-stop.log`

```sh
docker stop --time 3 knowhub-v1-wiki-dynamic-c2f260317fc1-regression-redis
```

- `create-restore-db` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/create-restore-db.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml exec -T postgres psql -U postgres -d postgres -c 'CREATE DATABASE knowhub_v1_restored TEMPLATE template0'
```

- `restore-check` exit=0; log: `evidence/v1-wiki-dynamic/acceptance-c2f260317fc151fb/restore-check.log`

```sh
docker compose --project-name knowhub-v1-wiki-dynamic-c2f260317fc1 --project-directory /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb -f /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/compose.yml exec -T postgres psql -U postgres -d knowhub_v1_restored -Atc 'SELECT version,dirty FROM schema_migrations; SELECT count(*) FROM knowledges; SELECT count(*) FROM tenant_api_keys;'
```


### Complete relevant race

```sh
docker run --add-host example.com:8.8.8.8 --add-host api.e2b.app:8.8.8.8 --add-host oss-cn-hangzhou.aliyuncs.com:8.8.8.8 --add-host open.feishu.cn:8.8.8.8 --add-host api-inference.modelscope.cn:8.8.8.8 --add-host api.anthropic.com:8.8.8.8 --add-host api.deepseek.com:8.8.8.8 --add-host api.hunyuan.cloud.tencent.com:8.8.8.8 --add-host api.lkeap.cloud.tencent.com:8.8.8.8 --add-host api.longcat.chat:8.8.8.8 --add-host api.minimaxi.com:8.8.8.8 --add-host api.moonshot.ai:8.8.8.8 --add-host api.notion.com:8.8.8.8 --add-host api.novita.ai:8.8.8.8 --add-host api.openai.com:8.8.8.8 --add-host api.qnaigc.com:8.8.8.8 --add-host api.siliconflow.cn:8.8.8.8 --add-host api.xiaomimimo.com:8.8.8.8 --add-host ark.cn-beijing.volces.com:8.8.8.8 --add-host company.yuque.com:8.8.8.8 --add-host dashscope.aliyuncs.com:8.8.8.8 --add-host generativelanguage.googleapis.com:8.8.8.8 --add-host integrate.api.nvidia.com:8.8.8.8 --add-host open.bigmodel.cn:8.8.8.8 --add-host openrouter.ai:8.8.8.8 --add-host qianfan.baidubce.com:8.8.8.8 --add-host router.requesty.ai:8.8.8.8 --add-host weknora.weixin.qq.com:8.8.8.8 --rm --network none --name knowhub-next-development-b702755c0333-full-race -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/source:/source:ro -v /home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/b702755c03332856/results:/results -v knowhub-next-development-b702755c0333:/work -v knowhub-next-development-b702755c0333-socket:/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1 -v feishu-incremental-1d533edfe339:/baseline-work:ro -v /home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro -v /home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu:/native:ro -v knowhub-mcp-b7-20261021-102200-data:/prior -e GOPATH=/home/aqyfzx/go -e GOCACHE=/prior/go-cache -e GOTMPDIR=/work/tmp -e GOTOOLCHAIN=local -e 'CGO_CFLAGS=-O2 -g -I/work/sqlite-headers' -e CGO_LDFLAGS=-L/native -w /source knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022 go test -race -timeout 25m -tags sqlite_fts5 -p 2 -count=1 ./internal/application/access ./internal/application/repository ./internal/application/service ./internal/middleware ./internal/mcpserver ./internal/router
```

Exit code: 0.
