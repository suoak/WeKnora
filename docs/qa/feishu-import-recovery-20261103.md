# 生产身份冲突最小恢复候选

本记录续接 feishu-import-incident-20261103.md，替代其中“仅诊断补丁、没有恢复实现”的当时状态。尚未声称生产导入恢复。

直接原因：七批次/六文档全部碰撞同用户同库的历史 application_grant 记录；新请求为 user_oauth。目标知识库无 data_source 或 legacy hold/revoked/paused 记录。个人批次接纳与历史 sync 队列暂停为两条链路。

候选基于实际生产 APP b874d5437；Frontend 单独基于实际生产 24c5dd551，仅修改三份飞书提示/操作文件，保留线上 MCP 功能。

3045 增加导入身份历史，将旧 attempt 外键绑定到保留的历史身份；新 attempt 的数据库触发器仍要求当前身份。旧 attempt 身份不能重写，也不能用历史身份创建新执行。原 import、resource、逻辑 Knowledge ID 保留。3043/3044 为独立任务账本工作的保留编号，未把该工作纳入本次发布。

交接仅在服务器明确配置 FEISHU_PERSONAL_UNPUBLISHED_HANDOFF_IDS 时启用；首轮只允许一份真实失败 DOCX 的 import ID。请求仍来自原用户、同 KB、同 App、同 canonical resource。
先验证当前个人 OAuth 的 view/copy/export 与 revision，再由 canonical 资源锁和原有 admission 事务核查：旧状态 needs_attention、无 Knowledge/Chunk/Embedding/active version、所有旧 attempts 无租约/nonce、outbox 未认领、当前成员/知识库写权限及 OAuth epoch 仍有效。
然后记录新身份，保留旧身份和执行历史，使用已有 prepare/outbox/原 parser/embedding/publication 流程。重复请求只复用一个执行。任何拒绝或事务失败均回滚交接。

回滚限制：交接发生后，3045 down 将因保留的旧身份而拒绝执行；不得改写历史身份强行降级。可关闭本次 allowlist 并使用兼容 schema 的旧应用或前向修复，不能恢复旧 DB 快照覆盖新业务。

验证：首轮 Linux 真实 PostgreSQL + race 的目标测试通过，20 个 pass 事件（含子测试和包），无 fail/skip。包括并发去重、身份历史保留、新旧身份数据库边界、失败状态回滚和权限否定；飞书 API 为隔离夹具，不是生产 ACL 证明。最终源码另补跑既有解析/BM25/vector/幂等/原 exact 执行门禁和 migration governance，发布前确认结果。

生产同步：PR #68/#69 实际包含在 APP 中。2026-11-03 16:03 两个已签名来源成功（46、59 份文档未变）；另一个 Drive 来源 162 份未变、一份等待所有者授权，失败数 0。正常来源通过 approved_sync 运行，六条历史 sync 消息继续隔离；异常来源未解除保护。更换 APP 后需按新 runtime/worker/incarnation 重新验证并签署来源策略，不复用旧签名。

最终验收必须由 wangtianyi1 正常登录重试真实失败 DOCX，核实解析完成、Chunk/Embedding、索引发布和内容检索；管理员会话或隔离测试不能替代。
