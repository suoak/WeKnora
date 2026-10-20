# RC2 正式发布与恢复步骤（未执行）

本轮没有生产变更授权。以下必须在 RC2 技术门禁、业务范围批准、HTTP 风险决策及正式发布窗口获准后执行。测试批准和数据克隆批准均不能替代生产批准。

## 发布前

1. 再次只读核对生产服务器 `10.51.134.114`、Compose release-package、APP/Frontend/Python MCP 镜像摘要、挂载、DB `weknora`、Redis 和 `schema_migrations=3023,false`。DB/Redis 不公开端口。实际文件卷为 `release-package_data-files`。不使用打包机 `WeKnora` 测试数据库代替。
2. 冻结发布内容，完成 RC2 源码提交和 PR/CI，Manifest、提交 SHA、构建镜像与已验收代码一一对应；当前 RC1 未提交快照不能被标为正式 RC2。
3. `main` 和 `v*` 推送会触发 Docker 发布工作流；Python MCP 目录/工作流变化可能在 main 触发 PyPI 发布。现有生产为固定 RC24 镜像、手工打包上传；仓库工作流未见直接 SSH 生产部署，但不能据此证明外部自动化不存在。确认全部发布控制面再批准合并。不要强推。
4. 明确旧 Wiki/Drive 正常更新解决或批准暂停；单纯 fail-closed 不是兼容验收通过。禁止清理队列、重建已有知识及批量重新向量化。
5. 备份 Compose、受控配置、加密密钥、APP/Frontend 镜像、**生产现有 Python MCP 镜像**、DB 和文件卷。先验容量和备份恢复，备份摘要存受控目录。不能以本轮 DB 快照代替发布时点 DB/文件/队列协调备份。

保持生产原 JWT_SECRET、数据/模型/来源凭证加密密钥及存储映射；不得用测试环境密钥替换，否则会使旧 Session、模型配置或来源凭证失效。新增 OA/飞书配置逐项审查，不复制整份测试 env。

## 数据库升级

1. 停止写接纳并等待相关 APP/Worker/文件副作用终态，避免 DB 与文件恢复点不一致。禁止临时清空 Redis。
2. 使用确认的同版本 PostgreSQL 17 / pg_search 0.22.2 / pgvector 0.8.1；只读检查 dirty=false、磁盘与锁。
3. 执行独立版本迁移步骤，使用候选原始 `migrations/versioned` 和相同版本 golang-migrate；本轮已提供不初始化 APP 的 `tools/migrate/main.go`（禁止 AutoRecoverDirty、ForceVersion）。实际序列为 3024、3025、3026、3027、3028、3029、3030、3031、3033；缺号 3032 合法，不补文件。构建、受控配置和 check/up 命令见 `tools/README.md`。
4. **必须先独立确认迁移进程成功及 DB3033 clean，再启动 APP。** 当前 container.go 会记录迁移错误后继续启动，故 APP 容器 running/health 不能替代迁移门禁。该历史行为本轮不改。
5. 可显式 `AUTO_MIGRATE=false` 启动已完成迁移的 APP；核对 SourceID/飞书新增表、FK/唯一约束、原数据与索引完整性、读写，并保留前后核对证据。出现 dirty 或超时立即停止，不手工改版本号。

恢复环境须提前配置足够共享内存：本轮默认 64MiB 使并行 HNSW 创建失败，隔离恢复使用 4GiB 共享内存继续验证。不得将此还原容器的参数变更未经批准施加到生产。使用空 template0 目标、等待最终 PostgreSQL 进程，按 pre-data/data/post-data 分阶段能明确记录完成点；定向恢复使用独立 schema/table 选项及 `--strict-names`，不能仅凭 exit0 断言已恢复。具体语义见 [PostgreSQL 17 pg_restore](https://www.postgresql.org/docs/17/app-pgrestore.html)。隔离全库复制采用 FILE_COPY、会触发检查点，只用于专用克隆，见 [CREATE DATABASE](https://www.postgresql.org/docs/17/sql-createdatabase.html)。

## 配置和冒烟

OA 显式 `code_flow_tls_userinfo`；生产不能复制 `continuous_test` 或测试实例标记。现有实现支持经真实批准引用和截止时间的 `time_limited` 策略；如生产需另一生命周期，必须另行批准，不能填虚假日期或绕过实例检查。保持 SID 官方 HTTPS Token/UserInfo、state/nonce/一次性事务、严格声明和 sub 一致性。

HTTP 回调仍存在授权码、Cookie、浏览器令牌及页面篡改风险，生产必须单独风险接受。固定公钥或 B 策略均未消除此风险。飞书仅 application_grant、Docx/Sheet/Bitable 首次导入；关闭首发 user_oauth 和变化版本个人更新，不新增 Go MCP。

先限已批准、已绑定身份的试点用户；管理员成功不代替普通员工/JIT/多空间 ACL 实测。完成真实登录、原 user_id/RJGH/RJEMAIL、Portal、KB 读写拒绝、退出旧 Access 失效、三格式索引/检索与原文引用、Python MCP 调用者 Key 只读冒烟。无身份/Key 项记录 NOT_EXECUTED/NOT_AVAILABLE，不推广为通过。

## 回滚

**当前恢复放行阻塞：**逻辑备份还原的数据/权限/DDL 对账通过，但 HNSW 自召回相对生产退化，串行重建未解决。不能把该备份标为已通过完整检索恢复的生产回滚保障。正式发布前须独立验证保留物理索引的备份恢复或可证明的逻辑恢复修复，具体证据见 `RC2_DB3023_3033_VALIDATION.md`。迁移本身未重建原生产 HNSW，不应为解决恢复实验问题擅自重建生产索引。

身份错绑、越权、泄露、数据异常立即停止扩大开放及新写入，保存脱敏证据。
仅应用故障且数据库向后兼容经验证：回退已保存的原镜像和配置，保留用户/知识数据；不自动执行 down，不启用危险旧来源更新。

回退至 RC24 前，停止新 OA 和导入接纳、排空/隔离新协议未完成任务，并按已批准流程撤销 OA Session 及 `auth_tokens.session_id <> ''` 的 Access/Refresh。源码核对确认 RC24 的 ValidateToken/RefreshToken 检查 `auth_tokens.is_revoked`，但不认识新增 SourceID 会话语义；**只更新新 Session 表的 revoked 标记不够**。保留普通本地账号及身份绑定审计，不重置密码或删除账号。APP/Frontend 成对恢复 API 兼容版本，Python MCP 保留生产现有镜像。旧镜像的 DB3033 静默启动通过也不等于未完成新任务和 OA Session 的语义回滚已通过。

迁移失败或不具备兼容证明：保持 APP 停止，隔离失败数据库和证据，在独立新库恢复已验证备份、核对一致性后经批准切换；文件卷和任务恢复需同一恢复点，避免丢失发布后新写入。禁止 ForceVersion、重置账号、清知识库、删除旧任务来制造恢复成功。

OA 切回 JWKS 只在 SID 正式修复、无 kid 签名兼容真实验收通过后显式配置 `jwks_signature`。不得在 B 失败时自动切换认证策略。
