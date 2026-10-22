# 飞书安全增量：阶段 B 实施记录（持续更新，未验收）

本文件承接原执行清单，不替代原 Cursor 局部交付及其 FAIL 日志。日期：2026-10-21，北京时间。当前仍在开发，**阶段 B 未整体通过、阶段 C 未部署、不得据此解除生产停更**。

## 源码及保存边界

- 独立 worktree：`E:/code/WeKnora-worktrees/feishu-safe-incremental-20261021`；分支 `fix/feishu-safe-incremental-20261021`；HEAD `e62b17e2d305358467821aeff1cce63ce1d61254`；源码均未提交。
- V1 `5468da42...` 冻结包和现有五容器实例未覆盖；独立飞书源码不含个人 MCP 功能移植。
- 各测试候选完整源码、逐文件 SHA256 和包保存在 `E:/AIData/Codex/feishu-safe-incremental-20261021/<label>-<sha16>/`；本地日志在 `evidence/feishu-incremental/linux-<label>/results/`。Git HEAD 不能代表这些源码。
- 3034/3035 为冻结 V1 原 SQL 的逐字节副本，校验见 `evidence/feishu-incremental/v1-migration-preservation.json`。新能力使用 3036，不覆盖、不重编号、不补造生产迁移。

## 当前实现和已知限制

| 范围 | 源码 / 函数 | 已实现 | 仍待验证或修复 |
|---|---|---|---|
| B1 私有构建 | service/feishu_legacy_staging.go `StageFeishuLegacyItem` | 实际 docreader、chunker、既有 HTTP Embedding 客户端；私有候选 Chunk/halfvec/BM25；文件按 attempt 隔离 | 当前仅共享 PostgreSQL/ParadeDB、document KB、平面分块、local storage；其他模式明确拒绝；正式模型和云端不属于合成 PASS |
| B1 原子切换 | repository/feishu_legacy_atomic.go `Begin/Prepare/Publish/Receipt` | 可信 tenant+KB+datasource+external 身份；复用 RC3 KID；版本/模型/KB 配置校验；单事务切换 Knowledge、Chunk、向量/BM25和回执；COMMIT ACK 丢失独立查询恢复 | APP/Worker 集中验收、容量计量、私有孤儿文件回收仍待完成；不能将逻辑索引行数称为实际物理 posting 数 |
| B2 真实围栏 | 3036 SQL trigger；`execution/owned` | 身份 advisory lock 排空未提交旧写入；KID及资源行锁贯穿校验和写入；generation、nonce、lease；旧 worker 写入与清理拒绝 | 需独立进程及 Worker/Redis 故障集中验证；不能只凭 check 后释放锁的单测宣布完成 |
| B3 持久拒绝 | repository/feishu_legacy_quarantine.go `RestrictSource/SetSourceUncertain` | 不可信缺失先 HOLD；有效投影私有快照、公开正文清空；可信撤回保留 revoked；不物理删除旧文件 | `confirmed=true` 目前为内部接口和测试，尚无已验收真实飞书可信事件入口；不把 404/403 或列表缺失当永久删除 |
| B3 读取 | types/feishu_legacy_read.go；postgres/feishu_legacy.go；router/feishu_legacy_files.go | APP 正式迁移后安装持久门禁，关闭 ingestion 不解除；Knowledge/Chunk/KB/hooks，Wiki投影预过滤，真实 PG 候选过滤，文件读取复验 | 当前无法可靠归因的同 KB 衍生内容整体 HOLD；Raw SQL、历史派生、非 PG 驱动等读取面的完整矩阵仍待核对；不能声称完整 M3 ACL |
| B3 模型边界 | service/feishu_legacy_model_guard.go | Chat/Rerank 调用前复验，流输出逐块复验；spy 测试 | 无可靠完整来源时当前采用同租户模型调用保守拒绝，会影响无关 Chat 可用性；需要在联合候选中验证边界，不能隐去该限制 |
| B4 游标 | datasource_execution.go `WriteExecution`；types/feishu_legacy_version.go receipt set | 成功发布回执与游标同事务验证；同步 ingestion 完成才返回；过期回执拒绝；完整核对解除 source HOLD 与最终游标同事务 | 新状态机完整 APP/Worker恢复、事件撤回、备份恢复待集中验证 |
| B5 连接器 | core/engine.go、shared.go、datasource_service.go | checkpoint失败停止；跨配置根目录移动延迟判断；429/5xx与权限类错误分离；失败项不确认；仍复用原 Wiki/Drive 客户端 | Docx/Sheet/Bitable/新个人导入、OA/Portal/Python/MCP 在新联合候选上不继承旧 PASS |

上述路径前缀为 `internal/application/`，特别标明的 types、router、migration 除外。

## 当前测试证据

- `b1-pg2/3` 的真实 parser/manifest 失败原样保留；修复 PG jsonb 语义哈希后后续通过。
- `b2-pg2/3` 的读取 guard 生命周期失败保留；已改为启动时数据库 plugin，避免 statement setting 在 hook/session 中丢失。不能关闭断言。
- `b2-pg4` 私有快照被旧结构 Save 覆盖的失败保留；已改为字段更新，保留私有快照。
- `b2-pg5` 驱动返回结果容器包含空 Results 的断言失败保留；后续断言针对实际候选数量，未放宽零内容泄漏要求。
- `b2-pg6`，源码 `b57a0e2bb3c2b6420db06700604ca2a4ad68642021e34d75afee39db690e65d3`：core/service/router 编译及聚焦回归 exit0；仓储真实 PG 和实际 parser + 合成 HTTP 1024 维 Embedding 均 exit0。
- `b2-pg7`，源码 `14a0a9868a7dc2d9bbb5f853caf220016abf0bab09e4ff1f883fd851a8c8c8bd`：上述回归 exit0；增加 Wiki projection guard。不是最终源码。
- `b2-pg8`：SQLite 真正 3033→3036 / down→3033 / 再 up 通过；PG 初次 up 成功，但迁移不变性测试将新 Go 纳秒时间与 PG 持久微秒时间比较而失败。已改为比较迁移前持久值与迁移后持久值，尚待新候选结果，不篡改旧 FAIL。
- 真实 PG 验证包含：实际私有与公开 BM25、真实向量候选过滤、旧 worker拒绝、写入失败事务回滚、MVCC旧/新完整视图、未提交旧写入接管、提交 ACK 丢失、租约接管、直接 ID 拒绝、私有快照及同 KID 恢复。
- 完整 race/vet/native APP build 与阶段 C 仍为 TODO，不能用局部 PASS 替代。

补充进展（不覆盖以上旧记录）：

- `b2-state1` 源码 `1e1739745f2c91ce4879b0e37a83d12b284f12f10a8c6f303bac243d4dffb7f6`：ProcessSync 实际仓储/游标状态机测试 exit0，合成外部连接器；重复终态无网络调用，429保旧，权限异常HOLD，完整核对解除与Cursor原子确认。
- `b2-http1` 源码 `a6f6b6190cb0ef3e6aebee3b1f6e167a0475785d896bf79fe87358c08fb68717`：真实 Wiki、Drive Go HTTP 连接器访问合成飞书服务、blocks→Markdown解析、合成HTTP模型1024维、真实PG/ParadeDB、ProcessSync全链 exit0；不是飞书真实云端验收。
- `b2-quality2`：race、vet、native APP build exit0，但完整受影响 service 回归 FAIL（测试实例被重复运行且同库夹具主键碰撞）；总体验收仍 FAIL。
- `b2-quality3`：启用原有 PostgreSQL 回归，发现 Redis fixture 未提供而两项 Asynq 测试按原断言 FAIL，日志保留；没有跳过或修改这些断言。
- `b2-quality4`：PG/Redis启动成功，但 Docker 不允许 `--network container:...` 与 `--add-host` 同时使用，后续容器 exit125。该次仍 FAIL。新 runner 采用只读 fixture hosts 挂载；旧测试固定 Redis alias 仅解析到新命名空间 loopback，FLUSH/SHUTDOWN 不会访问共享旧fixture。
- 独立联合树已准备（尚未冻结/部署）：`E:/code/WeKnora-worktrees/knowhub-v1-feishu-integration-20261021`，基础RC3相同；证据 `joint-preparation/resolved.json`。只保留原V1+本轮飞书，不移植并行OA/JIT或额外MCP功能。PostgreSQL候选过滤显式合取，不覆盖V1授权条件。
- 3036新增1024维索引仅在不存在时创建并标记migration3036所有权；允许的隔离down只删除本迁移拥有的索引，保留先前已有索引。尚待新候选回归。

## 回滚与上线门禁

默认保持 `FEISHU_SAFE_INCREMENTAL_ENABLED=false`；生产 `FEISHU_LEGACY_SYNC_SUSPENDED` 不变。不执行生产 SQL、游标修改、开关解除或发布。

**重要回滚限制**：有 HOLD/revoked 数据后，直接恢复旧 RC3 APP 可能失去文件/衍生读取门禁；不能认为仅停同步即可安全回滚。需要保留加固读取组件，或在受控网关阻断相应读取面后才恢复旧 APP。该流程尚未集中演练，是待关闭门禁。默认保留 3036 Schema，不直接 down。3036 down 会拒绝存在 managed resource/source 的库。

历史匿名链接：本 APP 本地文件路径可在每次访问复验；已经被浏览器/CDN缓存或下载的字节不能远程收回。对象直链、外部缓存及不经过加固 APP 的路径没有证明可撤销，继续 HOLD，不以短 URL 有效期替代验证。

47/48 差异已由生产只读伪匿名集合比较确认：历史48项中一 KB 已软删除，空间仍有效且 Published；未读取正文，未写生产。详见 `kb-range-set-difference.json`。

停更期限记录为 **2026-10-27 19:25（北京时间）**。不自动解除或延长；若门禁不能关闭，保留停更并提供人工决策依据。

## 2026-10-22 收口进展（尚未冻结）

- 独立 RC3 开发候选：`d39c698b5bcae7a7a370e6315c7d70bb801d4d54844bc6b505b6df741dc7740c`。这是排除本集成文档目录的源码指纹，不是 HEAD 或最终完整包指纹。
- 已对接现有注册 local storage backend、ResourceCatalog 及 resource://。候选文件先绑定 attempt，正式发布事务校验 backend 配置摘要后绑定稳定 KID；旧文件及失效 attempt 链接持续按最新策略拒绝。
- 发布事务复用 PG 索引容量估算，锁定租户并原子更新 storage_used/Knowledge.StorageSize；配额不足不切换。新增真实 PG 故障断言已通过。
- HTTP 请求记录真实候选 KB 来源；Chat/Rerank 网络调用和 HTTP 正文写出前复验跨租户来源 HOLD，文件逐次 Read 复验。非飞书普通资源不因本组件增加来源授权事实。
- 恢复同步仅放行管理权限下的 KB 元数据查找。历史日志仍走正常内容权限；受限时管理响应移除历史游标、同步结果、日志、错误正文及来源配置，不修改持久化游标。
- 受控 Go 构建镜像补足 Python 3.11 ensurepip（仅纯 Python 标准库和离线 wheel），真实 venv 与 pip 安装已验证。未改 APP/生产 Python；此前环境缺失导致的完整套件 FAIL 保留。
- 最新独立候选的原子发布、实际解析/HTTP Embedding、正式双库迁移、graph 门禁、完整 database/service 套件及 PG race 已 exit0。完整 ./...、剩余 race/vet/APP 构建尚在运行，不得提前标为最终 PASS。
- 联合候选 `c13e04d...` 的完整 service 套件 FAIL：V1 共用夹具断言迁移完成版本为 3035，而实际为 3036。原日志保留；版本断言和空库回退目标已明确更新，全部授权和历史数据断言保留。修正后的联合候选另行完整测试，不继承旧 PASS。
- V1 冻结工作树运行源码重新逐文件核对：0 差异。联合源码保留 V1 检索授权、DI 与路由，再合取飞书拒绝条件；没有整目录覆盖。
- 合成飞书私有 HTTP 服务及独立部署脚本已准备。阶段 C 尚未部署；真实云端撤权、正式模型、真实 SID 继续 NOT_TESTED/BLOCKED。

