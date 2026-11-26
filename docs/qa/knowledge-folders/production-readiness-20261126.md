# 多级目录生产发布准备与限定发布方案

> 历史准备快照（2026-11-26）：以下3051基线和发布前门槛描述当时状态。用户随后批准发布、豁免新全库备份/恢复，并单独批准SID限时续期；生产现已61130bc80 / Schema3052。以[2026-11-27实际发布验收](production-acceptance-20261127.md)为最新状态。611版回滚脚本不能直接用于Docker29；本收尾PR已补充身份映射及SID到期预检，仍不免除回滚备份要求。

本文件是供用户决定的方案，**不是生产执行授权**。在该准备快照时点，生产应用和数据库保持原状；随后按用户新增授权清理了历史存储，见[清理回执](storage-cleanup-20261126.md)。未停写、迁移、替换运行镜像或执行飞书恢复操作。目录验收与测试站点的历史飞书停止分别结论，见[兼容项](compatibility-20261126.md)和[独立诊断](sync-incident-20261126.md)。

## 当前基线与候选选择

只读观察对象 `10.51.134.114`，数据库 `weknora`，PostgreSQL 17.9，Schema **3051、clean**；`knowledge_folders` 不存在，`knowledges.folder_path` 为 varchar(1024)。[只读清单](production-prep-evidence/production-readonly.json)、[实际拓扑](production-prep-evidence/production-topology.json)。

| 组件 | 当前生产基线 |
| --- | --- |
| APP（内嵌 Worker） | `WeKnora-app`，源 556657a92，镜像 `sha256:41ded77264df8097a6d7641f3e87ae07e2809cdd57ada72d1a2539b079395967` |
| Frontend | `WeKnora-frontend`，源 4257ca6c5，镜像 `sha256:00eecab6b8ccf5808beab525254a0f308bc0d4c71ac7297151ab5d041249aa2e` |
| Compose / 网络 | `release-package`，`release-package_WeKnora-network` |
| 写进程 | 当前容器环境与网络清单仅发现 APP，Worker 内嵌；切换时必须重新清点，不能把测试站点的 Worker2 拓扑照搬过来 |

两份基线提交均在 main 历史中；4257ca6c5 与 556657a92 的整个 frontend 代码树相同。旧镜像标签不一致的事实保留记录，不能把原组合宣称为同 SHA 新发布。

候选必须在发布时重新 fetch main，全部相关 CI 成功，APP/Worker/Frontend **严格同一完整 main SHA**；不能直接发布测试镜像 642bbe7f1，也不能用“包含 main 的分支提交”替代当前 main。运行 `scripts/ops/verify_release_provenance.py` 并额外断言 paired_source_sha 等于实时 remote main，而不只是祖先关系。main 变化、镜像 ID 变化或生产基线变化即停止并重新生成证据。

本次补充发布工作流的 Frontend OCI revision/source 标签、两组件标签断言及 image-inspect 回执，避免仅靠容器标签证明来源。新镜像还须检查 archive SHA、原始配置 digest、rootfs diff IDs、APP 构建信息、前端静态提交及迁移文件；标签不替代字节和运行验证。候选实际 SHA 和镜像 ID 以对应构建 artifact、来源校验回执为准，不在文档追加提交中制造循环更新 main。

## 必须保留的生产差异

- APP 的 7 个挂载保留：只读词典、文件卷、只读 docreader 目录、只读配置、个人飞书私有文件卷、恢复审批公钥、MCP usage spool。生产已有 `/app/jieba-dict` 只读挂载，保留原路径；不能照搬测试站点的词典路径适配。
- Frontend 的 4 个只读挂载保留：nginx 模板、隔离 MCP HTTPS 配置、来源白名单、TLS 目录；80 和 8443 端口绑定原样保留。新目录不增加隔离入口写权限。
- 原环境、权限、RBAC、数据源、normal-service/automatic-service 模式、生产 namespace、预算、密钥、Redis 和队列原样保留。仅将发布 source SHA 与实际编译版本对齐；禁止用旧 source SHA 冒充新 Worker。
- 生产 namespace 与测试站点不同。只读采样中无 trip、permit TTL 为正、租约有效，不能将测试故障当成生产故障；同样不能据此把全部真实来源同步记为成功。[控制状态](production-prep-evidence/production-control.json)。

完整原始 inspect 与生成的原配置 Compose 已在本机 DPAPI 加密保管，未向生产写文件，未把密钥或生产数据提交到 Git。[快照摘要](production-prep-evidence/production-baseline-snapshot.json)。实际执行前重新确认基线无漂移；审批后才解密传入生产私有发布目录，权限 0700/0600。

## 发布检查清单与顺序

发布材料和全库/目录备份应使用 `/mnt/sata0/knowhub-folders-release-<SHA>` 的真实私有目录（0700；文件 0600），不能用符号链接跳转。根盘 `/home` 本次采样仅约 10.2 GiB 空余，小于现有历史全库 dump 的约 21 GB。回滚工具保留旧 `/home/aqyfzx` 目录兼容性，但本次方案使用数据盘；工具在停服务前要求空余量至少覆盖当前数据库大小、目录表独立备份及 `max(5 GiB, 数据库大小的 10%)` 余量。此检查不代替新鲜备份、恢复演练、WAL/临时空间及两次备份同时保留的预算。

历史 3049 备份记录明确 `full_restore_verified=false`，其过去的豁免不适用于本次发布。旧 3042 归档的恢复证明也不能冒充当前 3051 的生产鲜备份及恢复验证。恢复演练通过后，可核对归档 SHA 并清理本次可重建的演练副本，再重新测量发布与回滚空间；不得删除原始归档或生产数据卷凑空间。

| 阶段 | 门槛 | 当前状态 |
| --- | --- | --- |
| 交付收尾 | #273 全部 CI 完成并正常合入，证据和实际部署对应 | 已完成，f3573b6f5；运行测试版仍为 642bbe7f1 |
| 目录功能 | 页面、35 API 断言、权限、原数据及检索 | 已完成，使用现有 QA 和合成数据 |
| Feishu 静态兼容 | #188、003048/003051、HOLD 受管文档目录移动及读写保护 | 已通过真实 PostgreSQL CI，控制代码未被目录提交修改 |
| 精确迁移路径 | 3051→3052，仅新建目录表，不改旧表和旧 migration | 生产只读状态已核实；合成 PG 全备份/恢复与迁移、目录保全已通过，来源记录数为 0 |
| 最新候选 | 当时最新 main CI、同源镜像、字节与 native 启动、原挂载兼容 | 以发布构建的独立回执为准；未通过不能切换 |
| 生产鲜备份/恢复 | 真实生产全库、文件/配置和队列快照，恢复到生产侧私有离线副本验证，无 Worker | **未执行，须在生产操作审批后执行**；不得拿合成演练冒充此项，也不把 Canary 数据恢复到测试环境 |
| 停写/迁移 | 新任务入口进入维护；正常退出所有写进程并等待租约/发布/处理中任务归零 | 未执行，待审批 |
| 切换 | 同源 APP/内嵌 Worker/Frontend，AUTO_MIGRATE=false，CLI 显式 migrate，3052 clean | 未执行，待审批 |
| 生产页面/API/MCP | 普通已授权身份/合成空间、目录/检索、隔离 8443 拒绝、Portal 和 MCP 回归 | 待部署后运行，不预记通过 |
| 真实在线同步 | 现有生产来源下一轮自然调度、权限、租约、发布/游标 | 尚未运行；保留为限定发布观察项，不复制 test03、不新造来源、不重放归档 |

生产审批后的具体顺序：

1. 重新取 remote main、CI 和实际 image inspect，校验候选字节；核对运行镜像/网络/挂载/端口/环境未漂移。生成只包含 app/frontend 的新 Compose 与原组合 `rollback-compose.json`；对照原 inspect 确认无权限或挂载差异。保存配对回执为 `release-provenance.json`、原镜像摘要为 `production-baseline.json`，置于 `/mnt/sata0/knowhub-folders-release-<SHA>` 私有目录。
2. 审批后先做生产一致性全备份及离线恢复演练并测耗时。恢复副本留在生产侧受控目录/独立库，不能连接 APP/Worker 或外网；不得把 production test03 来源移入测试站点。恢复校验失败或磁盘/维护窗口不足则终止，不迁移。
3. 进入经批准的维护窗口，阻止新写入，核查现有飞书执行/发布、Asynq active、pending-op fresh claim 和文档解析。正常停止全部写进程，包含 APP 内所有 Worker pool；使用无限等待的正常 TERM，不设置超时强杀。等待在途任务结束，保留未领取任务、Redis、游标和既有停止记录。
4. 停写后做新的 `pg_dump -Fc --no-owner --no-acl`，记录 SHA 和关键表摘要；验证恢复点与演练的差异。目录表尚不存在时记录此事实。保留原镜像、配置、文件卷及权限、队列和来源状态。超过批准维护窗口则中止，正常恢复原服务，不强迁移。
5. 在 `release-package_WeKnora-network` 使用新 APP 内的 `/usr/local/bin/migrate`，显式 `-path /app/migrations/versioned -database postgres://postgres@postgres:5432/weknora?sslmode=disable up`。PGPASSWORD 仅由私有 env-file 提供，不进参数或日志。必须是 3051 clean→3052 clean；禁止 Force 或修改历史 SQL。
6. 同版启动 APP/内嵌 Worker 与 Frontend，保持 AUTO_MIGRATE=false 和全部原挂载、隔离策略。先确认健康、API commit、前端静态提交、Worker 实际身份/租约、生产 permit 和原安全状态，再结束维护。失败走下述回滚，不为健康检查清任何锁。
7. 普通业务身份执行目录/检索和隔离拒绝；只观察原有已授权来源的自然同步，不新增授权或移入测试来源。新建目录表立即独立 `pg_dump -Fc --table=public.knowledge_folders`，另做完整验收后快照，记录行数和 SHA。

## 可执行回滚及范围

已提供 [rollback_knowledge_folders.sh](../../../scripts/ops/rollback_knowledge_folders.sh)，默认 `--dry-run` 无操作。它检查精确原镜像、保护挂载、额外写进程、Schema 和在途工作，正常停写，备份当时完整数据库和目录表，回切已记录的原兼容镜像组合，**保留 Schema 3052 和目录表**。旧前端可能暂不显示空目录；再次升级后仍可恢复显示，不能据此 down 删除数据。

已将生产基线 556657a92 的旧 repository 代码连接到专用合成 Schema 3052，真实 PostgreSQL 读、移动、重命名通过，3 行目录标记保留，合成文档最后全字段一致。[回执](production-prep-evidence/rollback-old3051-repository.json)、[日志](production-prep-evidence/rollback-old3051-repository.txt)及[可重现测试源码](production-prep-evidence/old3051-repository-test.go.txt)已保留。此项不冒充旧原生 APP 的完整启动演练，回退后仍须健康和业务检查。

```bash
# 仅在明确批准该候选的回滚、且私有材料已验证并落盘后执行。
CONFIRMED_ROLLBACK_RELEASE_SHA=<已批准候选完整SHA> \
  bash rollback_knowledge_folders.sh --execute \
  /mnt/sata0/knowhub-folders-release-<SHA>
```

脚本的环境变量只是防误触门槛，不等于业务/安全审批。私有 `rollback-compose.json` 必须来自经确认的原快照，不能随意拼接 249 层历史 override，也不能拉取浮动 tag。原镜像仍须完整保留；旧前端标签不一致的事实见基线说明，不能伪造配对证明。

**迁移失败/dirty 的数据库恢复单独处理**：脚本会拒绝继续。保留失败库及其完整和目录独立备份，将已验证的停写备份恢复到生产侧新私有库（createdb + pg_restore --exit-on-error），比较数据后把原 APP/Worker 和 Frontend 一起指向该库。不得覆写当前库或 Force migration；有部署后新业务写入时，必须先保全和审查差异，不以旧快照覆盖新数据。

以下为审批后、全部写进程已停且恢复点已确认的独立数据库恢复命令；不在测试站点执行，也不覆盖现存库：

```bash
restore_db="knowhub_folders_restore_${APPROVED_SOURCE_SHA:0:9}"
docker exec WeKnora-postgres createdb -U postgres --template=template0 "$restore_db"
docker exec -i WeKnora-postgres pg_restore -U postgres -d "$restore_db" \
  --exit-on-error --no-owner --no-acl < "$PRIVATE_RELEASE_DIR/before-3051.dump"
docker exec WeKnora-postgres psql -X -U postgres -d "$restore_db" -qAt \
  -c 'SELECT version,dirty FROM schema_migrations;'
```

只有恢复库为 3051 clean、关键数据与停写恢复点一致后，才从原私有 Compose 生成一个独立恢复配置，将 APP 的 DB_NAME 指向该库、AUTO_MIGRATE 保持 false、镜像保持原 ID；使用 `docker compose -p release-package -f <独立恢复配置> up -d --no-deps --no-build --pull never app frontend` 回切并验收。当前失败库及独立目录表备份继续保留。

## 风险判断与供决定的方案

**目录功能具备提交发布审批的功能证据；尚不等于无条件全量上线许可。** 测试站点历史飞书停止已与目录改造分开证明，不能强行恢复来凑验收，也不应因此自动否定目录功能。

- **推荐的限定方案**：待最新 main 候选全部构建/字节/启动门槛通过后，申请一个受控维护与观察窗口。只部署同版代码及 3052，不变更来源授权、namespace、预算或恢复模式；保留全部未运行验收项。生产鲜备份与恢复为切换前硬门槛，生产普通权限/检索/隔离验证为解除维护硬门槛；真实同步在原授权下观察。
- **同步观察边界**：按生产已有最短调度周期约定一轮自然执行及完成的截止时间，不凭空承诺 5 分钟或绕过授权。若观察窗口内不能运行，状态仍为未验证；由用户选择维持受限观察或代码回滚，不能自动改写为通过。出现新 trip、Worker 身份/租约异常、检索/权限退化、数据异常，立即停止发布扩展并按保全回滚处理。
- **保守方案**：在不接受上述剩余在线同步风险时暂缓生产。目录、数据、检索和迁移证据仍成立，不要求重做飞书来源授权；历史测试站点恢复按独立方案处理。

不删除在线同步验收项，不将源码兼容、租约存活、permit 为正或合成测试替代真实同步成功。生产操作必须另行得到用户确认。
