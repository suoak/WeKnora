# RC3 正式维护窗口操作卡

状态：准备材料，尚未授权生产执行。业务已接受短期停更；开始/恢复/最长时限及具名联系人待指定，见 `RC3_BUSINESS_SUSPENSION_CONFIRMATION.md`。

## 首发范围与配置边界

- OA：显式 `code_flow_tls_userinfo`；默认源码仍为 `jwks_signature`。Token/UserInfo 保持可信 HTTPS、严格声明与事务校验，不依赖 A/B 公钥，不自动降级。
- 生产不得照搬测试 `continuous_test` 和测试实例标记。使用实现支持的 `time_limited` 生命周期、真实生产审批引用、实例范围及明确有效期；配置不齐全时不启用。
- 入口和回调仍为 HTTP，授权码、Cookie、localStorage Access/Refresh 和 Bearer 请求有明文与页面篡改风险。需要独立生产风险接受和发布批准；测试通过不等于风险消除。
- 新功能仅开放 application_grant 的 Docx/Sheet/Bitable 首次导入。user_oauth、变化版本更新、Go MCP 迁移和未验收高级大表能力不开放。
- 全部新 APP/Worker 配置 `FEISHU_LEGACY_SYNC_SUSPENDED=true`。该配置必须进入受控 Compose；不是临时 shell 变量，不是旧 Worker 围栏。
- Python MCP 原镜像、API Key 模式、服务和配置保持不变。真实只读专项验收由用户移出本轮门禁，记 WAIVED，不宣称已迁移或已通过测试。

## 窗口前

1. 锁定 PR 最终源码提交、Manifest、APP/Frontend 镜像 ID 和归档 SHA256；确认正式服务器由 Windows 上传归档，遵循现有流程。
2. 复核 main 自动构建：`.github/workflows/docker-image.yml` 在 main/v* 推送时发布镜像。外部是否自动拉取必须由运维明确确认；未确认前不得合并 main 或打 v* Tag。代码库未发现 SSH 自动部署不能证明不存在外部 Watchtower/定时拉取。
3. 确认生产仍是已审查的 RC24 / DB3023；再清点所有使用同一数据库、Redis、文件卷的 APP/Worker 与自动重启/外部调度。变更时必须再次核验，不以历史 docker ps 代替。
4. 取得 HTTP OA 生产风险接受、OA time_limited 配置授权、维护窗口及停更影响确认；指定能处理紧急撤权的业务/技术联系人。
5. 保存镜像与 Compose、受控配置、文件卷、Redis 和数据库备份；敏感备份仅在受限位置。物理恢复必须通过 pg_verifybackup 与实际启动/检索验证，不能只看备份退出码。备份时间点与文件/Redis 对齐方式需明确。

## 窗口执行（尚未执行）

1. 进入维护状态，停止新写入/同步接纳。通过正常管理接口暂停各来源，导出完整来源配置、cursor、schedule、status、last_sync_at/result 及同步执行代次、任务 ID 到受限文件，校验摘要。不把完整凭证/正文写日志或 Git。
2. 停止全部旧 APP/Worker 并阻止自动重启；核对实际构建身份、进程退出与远端在途请求终态。结果未知保持阻塞。不能仅靠 pending=0 或租约到期确认排空。
3. 不允许旧 Worker 与新 APP 同库混写。没有确认全部消费者退出，不运行迁移或恢复候选写入。
4. DB3023→3033 使用已经验证的原始版本迁移：实际文件为 3024–3031、3033，**没有 3032**。不补号、不改历史 SQL、不 ForceVersion。复核 dirty=false 后启动候选；失败则保留日志与数据库，不强制跳版本。
5. 在全部新 APP/Worker 明确启用停更开关，部署锁定镜像；保留 DB/Redis/文件映射和 Python MCP 原镜像。不要重新向量化，不清理任务队列。
6. 新 Worker 对旧来源已排队任务在 connector/知识写入前终态拒绝：SyncLog failed、来源 paused、保存原因、释放执行认领、保留旧游标/成功时间/结果，返回 SkipRetry；重复投递不再次有效处理。DB 确认失败不虚报成功，须恢复/对账后才能放行。
7. 分页检查 pending/active/retry/scheduled/archived，按来源/SyncLog/TaskID 汇总；不能每条任务扫描全部队列。只允许候选消费者执行正常终态流程，不清队列、不删归档补偿。
8. 对账旧 354 Knowledge 的 KID、内容/Chunk/Embedding、Wiki、共享 ACL、来源引用和游标摘要；允许的来源 paused/错误原因变化单独列出。检查新三格式首次导入最终完成索引、检索及引用，OA 登录/退出与服务端无权限拒绝。
9. 限定获准用户试点，监控认证/索引失败、旧同步抓取次数（应为 0）、重试/归档增长、权限异常和数据变化；异常停止扩大开放。

## 回滚

应用问题且数据未受损：停止候选写入，保留证据与新产生数据；选择经兼容性验证的旧镜像和配置。RC24 不认识新停更开关，不能仅保留 true 就重新启动其同步消费者；需要保持来源和旧同步入口隔离/停机，单独批准后恢复服务。

数据库损坏或迁移无法恢复：停止所有写入，保存故障现场到独立路径；在新空卷恢复已验证物理备份，绝不覆盖原故障卷。使用匹配 PostgreSQL 17/pg_search/pgvector 镜像与目录权限，恢复对应文件/配置，先独立无网络验证，再按批准流程切换。回滚会丢失备份后写入，必须明确 RPO 和业务批准；不能把数据库回滚描述为无损。不得以删用户、清 Knowledge 或 ForceVersion 恢复。

## 恢复增量更新（后续独立工作）

安全更新实现通过失败保旧、重复执行、稳定 KID/ACL/引用、索引最终完成及 Wiki 保护验收后，逐来源批准恢复；使用保存的 cursor/配置先单源试点。不允许直接关停更开关后恢复危险 Replace，不强制全量、不重建原成功 Knowledge。预计恢复时间尚未承诺。
