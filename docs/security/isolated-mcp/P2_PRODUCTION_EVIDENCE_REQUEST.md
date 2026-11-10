# 生产只读补证清单及最小候选门槛

截至本轮 P1.5，已有生产入口授权只读协议证据只证明 initialize、tools/list 和只读知识库列表成功；生产 APP/前端镜像、源码、有效代理链路、schema 与策略均无现场证据。此前 `aqyfzx@10.51.134.114` 严格 SSH 校验下认证失败，且没有新的获批生产只读会话。本轮不重复尝试该连接，不读取员工 Key，不从 GitHub main 或历史部署文档推断当前运行版本。

| 项目 | 请运维通过已有只读流程提供的最小信息 | 当前状态 |
|---|---|---|
| APP 与前端制品 | 运行容器 ID/镜像 ID/RepoDigest、构建 OCI revision 或对应源码提交完整 SHA、制品仓库/标签；证明 revision 与二进制/镜像可追溯。仅给所需字段，避免整份 `docker inspect` 的环境变量。 | UNKNOWN |
| 数据库 schema | 只读查询 `schema_migrations` 等实际治理表的当前 version/dirty，及部署版本允许的迁移范围；不执行迁移。 | UNKNOWN |
| Compose | 项目名、部署目录、基础/overlay/env 文件**路径和文件哈希**、生效 service/网络/端口/挂载选择字段；不要发送 env 文件内容或未脱敏 `docker compose config`。 | UNKNOWN |
| Nginx 与上游 | 运行实例版本/模块、有效 server/listen/location/`proxy_pass`/Host/real_ip/日志/缓存/超时/重试选择字段；从 `http://10.51.134.114/api/v1/mcp/personal` 关联到真实 upstream 与 APP 容器。不要输出可能含 Key/Header 的整份 `nginx -T`。 | UNKNOWN |
| 边界设备 | 实际 NAT/VIP/交互机、TLS 终止点、隔离区 DNS/IPv6、入站源 ACL；提供设备规则 ID 和命中证据。 | UNKNOWN |
| 日志与模型 | Nginx/WAF/APM/容器日志/模型请求或 embedding 服务的正文、Header、Key、签名 URL 采集及保留策略；必要时用合成标记在获批环境验证。 | UNKNOWN |
| 对象存储 | 当前存储后端、可访问网络范围、匿名/签名下载 URL 范围和有效期、PUT/上传入口与审计；不要求真实桶凭据。 | UNKNOWN |
| 回滚基线 | 当前可恢复 APP/前端镜像 digest、配置备份身份、回滚负责人/窗口/耗时和内网客户端回归清单。 | UNKNOWN |

可由运维在既有授权终端执行只读选择字段命令或给等效脱敏导出。镜像 digest 以运行实例对应的不可变 digest 为准，标签或 GitHub main SHA 不能代替。生产证书目录和 TLS 终止选择须先核实真实拓扑；本地证书包的检查结果见 [P0证书审计](P0_AUDIT_2026-11-10.md)，不表示证书已在生产安装。

## 候选版本区分

| 身份 | SHA / digest | 含义 |
|---|---|---|
| P1 主干参考 | `4b385fd11e2f617eee491781067b2f69cb8aa1be` | 当时已核验 GitHub main；**不代表生产** |
| PR #131 初始提交 | `d68231f7c6e440b64d7c82beceb893b52d525133` | 合成测试完整 APP 的源码候选；测试二进制 SHA256 见 P1 证据；**不代表生产可发布制品** |
| 生产现运行 | **UNKNOWN** | 等待运维镜像 digest、源码 SHA 和 schema 补证 |
| 最小生产候选 | **NOT_BUILT，SHA/digest 待生产基线确认** | 先把 PR 中必要安全改动适配到已证明的生产版本，再构建生产镜像、回归、记录不可变 digest |

生产候选不能通过合并旧 main 全量差异生成。最小补丁范围是复用现有 PersonalServer 的独立隔离只读路由 `/internal/mcp/isolated/personal`、仅该路由的严格请求/结果/错误边界、个人入口日志隐私控制和固定路径改写的独立8443代理配置；原内网 `/api/v1/mcp/personal` 保持原协议及正文契约，无数据库迁移。构建后必须对飞书同步、Wiki、RAG、旧个人 MCP 和真实内网 Codex/Claude Code/VS Code 客户端执行批准的回归。生产源码不明时不创建可上线镜像，不标注虚假的 digest。

补证完成前，[30项矩阵](P1_TEST_RESULTS.md) 中所有 PARTIAL/NOT_RUN 保持原状态，真实隔离网络、证书链、WAF/APM/模型、存储、容量和生产回滚须单独验收。停止条件是未证实的生产基线、TLS终止、网络源/目的或私有数据留存；见 [P2执行与回滚草案](P2_DEPLOYMENT_AND_ROLLBACK.md)。
