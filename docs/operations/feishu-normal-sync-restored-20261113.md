# 飞书正常自动同步恢复记录

2026-11-13 14:13（北京时间）复核：飞书正常同步队列已开启，Permit 持续有效，Safety Trip 为空。恢复操作未重启 APP、未改 Compose/8443、未迁移数据库、未关闭 Safety Guard、未直接删除 Redis 安全锁。

| 项目 | 实际状态 |
| --- | --- |
| 维护工具 main | 825752a2bbae13767783c096ecd3e9acf09e9b82，PR #143 已合并 |
| 生产 APP 构建来源 | 224da1f4dd557cad9615945f08bee8379aff31be，未重启 |
| APP 镜像 manifest | sha256:be78f5a3a2780fbee9561e7fcb9dc40b6444fcc53600b9376982a9914b6de551 |
| 数据库 | 3047 / clean，未迁移 |
| 正常服务授权 | normal_feishu_sync / current_authorized_sources_v1 / until_revoked，active |
| approved_sync | 开启；pending / active / retry 均为 0；历史 archived 89 保留 |
| 历史 sync | 保持暂停，3 个原始 GitLab 任务待单独签名恢复；未清空 |
| 来源总数 | 65：active 31、error 15、paused 19 |
| 本轮实际周期验证 | 24 个来源，5 轮周期任务共 120 次全部成功 |
| 实际普通手动同步 | 1 次成功；合计 121 次成功，无失败或未完成任务 |

## 恢复方式

旧 worker 7c5dbab8e5cc:1 的 Docker 容器已不存在、租约过期，但 Redis 保留了属于它的过期 frozen 恢复记录。旧维护工具只接受 awaiting_review，因此无法退役它。PR #143 为独立 SSH 维护工具增加默认关闭、必须绑定独立签名的 allow-expired-frozen 选项：仅接受已过期、version=1、run_id 为空、generation=0、且 worker 与 fingerprint/incarnation 完全匹配退役目标的记录。Running、admit、未过期、其他身份、开始执行的记录仍拒绝。

17 项真实隔离 Redis 测试通过，PR 全部 9 项检查通过或按配置跳过，然后合入 main。执行前核对远端 main 与工具字节，使用独立签名主机上 DPAPI 保护的 Ed25519 私钥，仅在内存签名。生产仍只挂只读公钥，APP 无 Docker Socket。

正式工具写审计后仅移除已证明死亡的旧 worker 成员、租约和生命周期条目，保留原 recovery 字节、Safety Trip、Permit、队列消息及审计。当前 worker 38e16cc78868:1 不变，退役审计 ID 为 1794545140294-0。

之后使用真实管理员现有会话调用原有签名恢复 API。首次 Reset 已成功，但本地控制脚本写回执时发生文件权限调用错误；根据服务端精确 plan/nonce/Permit 确认已执行，未重复提交。修正回执写入后继续。首次 Canary 来源使用其他员工个人 OAuth，正确拒绝管理员代发，未创建同步记录；未改变来源归属或权限。

改用实际操作者本人拥有 OAuth 的来源后，真实 SQL 同步记录成功，但 Canary 控制收尾仍返回拒绝并重新关闭安全锁。该 Canary 不计作通过，控制收尾问题仍保留为后续调查项。

最终使用已存在、独立有效的正常服务级授权及新的签名 Reset。原有 controller 健康检查自动解除其自身持有的 approved_sync 暂停，Permit 正常续租；无需 Canary 证书长期续期，也不需要逐个来源人工批准。随后通过普通 /datasource/:id/sync 接口提交并由真实 Worker 执行成功，再等待真实周期任务验证。

## 实际业务验证

13:00、13:15、13:30、13:45、14:00 五个周期各有 24 个来源执行并全部成功。另一次普通手动同步于 12:53:59 至 12:54:06 完成；不是模拟任务或受控 Canary。14:13 复核时已超过临时恢复计划有效期，正常服务仍运行，说明普通同步不依赖该临时证书持续授权。

本轮增量拉取没有发现新增或修改内容，items_created / items_updated 均为 0。因此不能宣称本次重新验证了“飞书内容变更 → 新解析 → 新 Chunk/Embedding 发布”的完整链路，也没有为制造变化而编辑业务文档。

本人来源已有发布文档的 15 个 Chunk、15 条向量存在；使用真实员工 Personal Key 分别通过云外 MCP、TLS8443 查询得到该文档，网页 hybrid-search 与文档详情接口均 HTTP200。内容与密钥不进入报告。该证据证明既有文档可检索，不冒充新内容处理证据。

## 未批量放开的来源和历史任务

31 个 active 中，24 个按 15 分钟周期实际验证成功；另外 4 个有效来源采用六小时或每日周期，本观察窗口未到其周期。其余 3 个 active 来源在交互式核查中分别有 2 个目标空间无效、1 个当前本地管理权限不可用，未伪造授权。

15 个 error 来源保持原始状态，按现有周期/重试和实际权限判定：12 个历史错误为 Sync execution interrupted，另外 3 个分别是文档暂缓身份/审计未确认、文档准入检查点保留、从已接受检查点重试。其中 1 个目标空间无效；没有强制改为 active。19 个 paused 保持暂停，其中 2 个当前本地权限不可用。当前未删除来源中 hold/revoked 为 0；已删除且 revoked 的历史来源仍保留其状态。

approved_sync 原有 4 个积压任务的 SQL 记录均已 canceled。消费者通过现有 Claim 的终态保护确认它们不执行、不重新发布；最终 4 条记录仍 canceled、未领取，历史审计保留。89 个 archived 未清除。

历史 sync 的 3 个任务均是 active GitLab 来源、首次 generation=0、未领取的合法任务。它们继续保持原消息及暂停状态，不用删除队列或重建任务代替既有签名共享队列恢复协议。飞书正常任务进入 approved_sync，已成功恢复；不能把该结果描述为所有共享队列均已开启。

## 兼容性、审计和回退

本期仅维护工具源代码合入 main；生产 APP 仍为已验收的 main224da 镜像，MCP 云外 12 / 云内固定 8 个只读工具及 schema3047 不受影响。独立工具不是新的内容权限入口。

所有 Reset、Canary 和普通手动提交都有受限目录中的一次性尝试日志、签名与结果；拒绝或回执不明时先核对实际状态，不盲目重试。没有修改员工角色、凭证归属、知识库授权、来源暂停或撤权状态。若出现新故障，沿用现有正式 Stop/Guard 机制停止并调查，不删除安全记录或重置数据库版本。

脱敏证据及 LF Git 字节 SHA256 清单见 [证据目录](evidence/feishu-normal-sync-restored-20261113/sha256.json)。控制脚本的权限修正与 Canary 收尾拒绝均如实记录；未将 CI 通过代替生产验收。
