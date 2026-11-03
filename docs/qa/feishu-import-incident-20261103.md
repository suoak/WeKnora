# 2026-11-03 飞书导入生产故障

状态：已完成生产只读定位；未恢复导入，未发布补丁。时间均为北京时间。

## 根因与范围

用户 wangtianyi1，空间洞察（10017），知识库洞察报告。
14:39:14 至 14:42:00 的七个批次对应六份文档（五篇 DOCX、一份 Sheet）；Sheet 重复提交一次。
七个批次使用 user_oauth。解析 Wiki 链接后，六份资源全部与 10 月 22 日同用户、同 App、同 KB 的 application_grant 导入记录重合。
这些历史记录均为 needs_attention，尚无 active_version_id；五篇 DOCX 的历史错误是 authorization_revalidation_failed，Sheet 是 candidate_content_incomplete。

生产版本 PersonalRecoveryRepository.Prepare 在 FirstCreateOnly 分支先锁定 canonical resource，发现 prior.AuthMode 与新请求不同便返回 ErrPersonalIdentity。
服务层 feishuV2Error 未识别这一拒绝，保存为 operation_failed；错误提示因而错误地引导检查连接或普通重试。
此定位由现场七个批次、Wiki 解析后的 canonical token、历史资源关系及运行源码共同支持。
生产没有保存这一次底层 Go error 的原文；不能声称日志直接记录了该 error。

此次失败发生在导入事务接纳之前。七项 import_id 均为空，没有本次 attempt、Knowledge 或处理 span；不能把它们归因于 DocReader、Embedding、索引发布或旧应用身份的历史失败原因。

| 批次前缀 | 创建 | 最后终态更新时间 | 重试次数 | 当前后端状态 |
| --- | --- | --- | --- | --- |
| 3a70b273 | 14:39:14 | 14:40:31.024666 | 1 | blocked / operation_failed |
| fa29375e | 14:40:16 | 14:40:31.296552 | 0 | blocked / operation_failed |
| 4fede2d7 | 14:40:28 | 14:41:26.420415 | 0 | blocked / operation_failed |
| 7d9991c4 | 14:40:37 | 14:41:27.119002 | 0 | blocked / operation_failed |
| 679e17bd | 14:40:59 | 14:42:26.487838 | 0 | blocked / operation_failed |
| e11f9a78 | 14:41:06 | 14:42:26.776655 | 0 | blocked / operation_failed |
| f616f629 | 14:42:00 | 14:43:26.012161 | 0 | blocked / operation_failed |

七项 lease_owner 为空、lease_until 为 NULL；API 终态 stage 为 needs_attention。
表中无独立 failed_at 字段，上列是终态更新证据，不能冒充独立失败审计时间。
原先三个“处理中”已被接纳扫描处理并拒绝，不是真实运行、等待重试或等待索引验证。

## 生产与 PR

- APP source：b874d5437302914b751b7a6d991162daa5bb1767；镜像 sha256:17845bb95a900aefe5f8af118135495d7b780bbf30e8a7ae0d71f10125e7f92e；healthy，RestartCount=0。
- Frontend source：24c5dd5514b44b6143af8b82f4b4edf361c439ca；镜像 sha256:5bfc08414292968c53e8cb648c4c108ffed03b12f9ace1dc86cd5e9dfebf7324。
- schema_migrations：3042，dirty=false；AUTO_MIGRATE=false。
- main：88e46547331acf28cf256617cdfd25ddd2c848e3。main 与 APP 各有 7 / 87 个独有提交，main 与 Frontend 各有 7 / 88 个独有提交。生产不能当作 main 的同义词；不得为这个故障直接整包覆盖 main。
- [PR #62](https://github.com/suoak/WeKnora/pull/62)：open、未合并；head 478544d79f5b66e6787a2dba775c5743fa402fb7 不在上述 APP、Frontend、main 的祖先链中。目标是 KB 来源限制可见性及阻塞任务识别，不能完成跨身份交接。生产源码也未检出其 source-restriction/admission-stop 路径。
- [PR #65](https://github.com/suoak/WeKnora/pull/65)：open、未合并；实际 API head 为 96090bb8582f1ba7f0ddb1b6b94b3ede1911b8c0，已被 APP 和 Frontend 源码包含，main 未包含。PR 正文还引用旧 head，不能代替实际状态。其个人 v3 DOCX、原 Knowledge/file 证明恢复不适用于本次尚未接纳且存在应用身份记录的资源；Sheet 也不是它的普通 DOCX 路径。
- [PR #69](https://github.com/suoak/WeKnora/pull/69)：open、未合并；head 7cdff9fbba07d7f852dcb43d14d2b2e769699b27 已被 APP 和 Frontend 源码包含，main 未包含。它处理 signed source-scoped execution、安全停止及授权队列，不能转换 personal import 的身份关系。

镜像有继承的 knowhub.source.git 等旧标签；以上 source 使用 OCI revision、容器镜像 ID 和运行时 system/info / 日志源码路径交叉核对，未把继承标签当成现用源码。

## 门禁、权限与执行链

只读现场检查：Worker 56c7cb7f937e:1 有控制器心跳和 Asynq 注册。
sync 队列仍有历史 feishu_safety_stop 暂停标记，pending=6，active/retry/scheduled=0；approved_sync 未暂停且为空；default/multimodal/postprocess/question/summary 为空；low 无活动任务、有 1 条历史 archive；wiki 无活动任务，有 8 条 retry 和 24 条 archive。后者不是这七个尚无 import_id 的批次。
共享 trip 不存在，permit 两次采样 TTL=575ms / 617ms；这是短租约的点时观测，不是长期健康保证。
没有清队列、解除暂停、清 trip 或触发 Reset/Canary。

目标 KB 的 data_sources（含软删除）和 feishu_legacy_resources 均为空，没有来源 hold/revoked/paused 证据。
个人 imports 为 needs_attention，不能把这个状态说成全局安全停止。

个人 OAuth 在 14:38:30 更新，access 到 16:38:30、refresh 到 11 月 10 日 14:38:30；故障时没有到期。
故障时日志可见 user_info、resolve_personal_resource（Wiki 另有 get_wiki_node）HTTP 200。
这只证明身份与资源元数据可读取，不能证明文档阅读、复制、导出权限全部通过。
本次在接纳事务碰撞处拒绝，尚无这七项的完整 live ACL/export/DocReader/model/index 证据；其它文档的权限成功日志未当作这七项的证据。

现场 KB 没有未删除 Knowledge/Chunk，也没有 Embedding。另保留一条 10 月 22 日已标 deleting 的历史 Knowledge 和三条已软删除 Chunk；本轮没有更改或清理。

## 最小补丁与恢复阻断

补丁从实际 APP source b874d5437 建立隔离 worktree，未修改用户原工作区：

1. 给跨身份首次接纳拒绝增加 ErrPersonalAuthModeConflict；仍包装 ErrPersonalIdentity，原拒绝边界不变。
2. 对外提供 source_identity_conflict、管理员操作提示；不伪造 Feishu API 错误或劝用户重新授权。
3. 对此类别禁止无效 RetryBatch，前端隐藏该项的重试按钮。
4. 接纳拒绝日志增加 batch/item ID 和安全分类，不输出 token、文档内容或原始错误详情。
5. 仓库回归要求碰撞后回滚且不允许写入；服务回归检查原身份拒绝仍生效及不可自动重试。

这只是诊断补丁，不会恢复历史七项，不能声称修复成功。
生产 feishu_attempt_principal_fk 明确为 (import_id,auth_mode,application_id) REFERENCES imports ON UPDATE RESTRICT ON DELETE RESTRICT。
旧应用身份与执行记录不可直接改成个人身份；现有导入 API 没有保留历史执行身份的受控交接操作。
删除历史 attempts/imports、放宽外键、假冒应用身份、绕过当前权限或另建 KB 重复导入均不采用。

恢复需要一个范围限定到上述六份资源的、审计可追溯的身份交接实现：保留原逻辑 Knowledge ID、原文件和历史 attempt/outbox，先验证当前个人 OAuth 的阅读/复制/导出和 revision，事务中验证无有效租约及无发布版本，再生成新执行身份而保持历史关系有效。
该路径必须经真实 PostgreSQL 约束及并发回归验证；不能用几条身份 UPDATE 代替。
先恢复一篇真实失败 DOCX，验证 Chunk/Embedding、索引发布以及该普通用户真实检索，再受控扩展到其它四篇 DOCX 和 Sheet；同一 Sheet 的两个批次必须指向同一资源结果。

## 验收状态

- 生产只读排查：已完成；所有 DB 查询 BEGIN READ ONLY + statement_timeout 后 ROLLBACK。
- 补丁：本地候选，未部署。验证结果另见随附 validation 文件。
- 本次真实失败文档导入、Chunk/Embedding、索引发布、普通用户检索：均未达成。
- 没有把本地测试、历史 PR 验收或其它用户文档当作本次生产恢复。

私有原始证据位于 E:/AIData/Codex/knowhub-incident-20261103-{inventory,batches,logs,gates,canonical,final-readonly}.txt 及 prs.json；含内部标识，不提交这些原始文件。
