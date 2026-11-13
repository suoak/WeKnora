# 线上飞书个人文档与 Wiki 存量恢复

2026-11-13 15:39–15:41 北京时间，只读现场；本文件不是线上恢复成功回执。
按用户要求：修复集中验证 → 合入 main → 集中部署 → 分批验证同步和 Wiki。不得逐个临时补丁部署。

## 个人文档：21份非完成

| 类别 | 数量 | 具体处理 |
|---|---:|---|
| 已停止、缺receipt、零分块零向量 | 7 | 新恢复路径重新证明来源/原文件/当前身份后，事务内建立恢复receipt并重排原dispatch；保持并发2的原恢复容量，先验收一份再推进其余 |
| staging保留、恢复预算已到5 | 9 | 分别验证零输出、已有分块缺向量、图片绑定；需要有审计且绑定具体对象/代次/已修复原因的额外恢复机会，不能仅发布新代码就宣称自动恢复，也不能重置累计计数 |
| 内嵌依赖预检拒绝 | 4 | 用原文件在隔离环境完整转换；链接/数量支持修复后重新预检，公式/绘图等未支持原因仍需解决；不能略过表格或图片以凑成功 |
| 授权或资源缺失 | 1 | 查明当前来源和owner是否仍有效；需要补授权时给到具体用户操作，不能由重试伪造权限 |

7份缺receipt对象均在库 `141e3aa3-df71-468e-a64d-a43f9be231a2`，Knowledge ID：

- `557089df-feb3-4608-a2cf-ee273d44a58b`
- `3a2b7b10-e2b7-4853-9362-e5416d5ca9ec`
- `bf6ac3a2-33e4-4fa5-b822-993dba643759`
- `144275ff-d586-4233-9a21-1c32241ac982`
- `a4588810-ed2a-49e1-b744-33e5ddc04f69`
- `b7e46f83-7432-4c5b-aab2-3e3c9b3d6b8f`
- `3633265a-829a-4b13-8c86-d069bd8212f6`

验收每份：原文件保留，attempt/代次/receipt一致，真实分块及向量完整写入，published数据库证明通过，用户检索可命中，历史状态收敛。索引成功与Wiki成功分别验收。

## Wiki：8库1226条阻塞操作

| KB ID | 操作数 | 当前阻塞 |
|---|---:|---|
| `54b0a305-1ab4-4fab-9937-778fd71c3a74` | 230 | source_verification_required |
| `8ca41885-69e1-4d43-8bfd-ffc8670900fe` | 50 | source_verification_required |
| `a01317cc-fcda-4dfd-b9c5-2086d7c0cf7a` | 56 | source_verification_required |
| `0d13271c-89ef-43b2-a296-13489f33e68c` | 56 | source_verification_required |
| `0944913a-c708-408b-bb07-7ed695831faa` | 106 | source_verification_required |
| `9ab5fab6-c21b-4cb1-bbd0-f622afdfeb89` | 289 | verified_regeneration_required |
| `c3dadce9-29ed-4dfa-b119-536b75f8bcbb` | 430 | verified_regeneration_required |
| `817c2313-1418-4a3b-8f5c-931994a8a745` | 9 | verified_regeneration_required |

这些库的checked_at已经更新至14:37附近，仍拒绝来源/重建校验；不能把所有阻塞归为丢触发器，独立唤醒只能解决交付缺口。

- 来源类498条：核对当前source、已发布resource/attempt、绑定覆盖、owner/当前授权；允许的文档单独参与生成。暂停/撤权/身份不明的来源继续隔离。补齐证明或同步后重检，不能直接解除全库保护。
- 重建类728条：使用现有只读Preview核对历史页面与当前输入；快照一致后受控Apply，保留人工编辑、有效页面及原队列证据。真实生成与Finalize消费由正常Worker执行。
- 对无效输入采用明确的文档级隔离，保留其原因和补入意图；不能先把失败文档SQL改为completed。
- 每库分批推进；观察实际发布页面、原操作消费、Finalize收敛、网页/MCP检索及引用，而非只观察Redis触发器是否入队。

NTOS库 `45c73999-ba38-4e12-895f-b53ac59fb647` 的495条操作处于活动执行，最近15:35仍发布页面。数量增长包含新Finalize，不能据此当作阻塞、取消或重启它。

## 生产变更前后证据

发布前固定运行镜像、main SHA、schema、目标原文件/分块/向量/receipt/任务ID、页面与人工编辑基线，准备回滚镜像。发布后先单文档/单库验收，再分批扩展；出现身份不匹配、输出差异或重复任务则停止扩大并保留证据。

本轮生产仅查询，无任务重试、队列清理、状态改写或服务重启。原始只读回执在私有目录 `E:/AIData/Codex/knowledge-incident-20261113-c996` 的 `personal-current-recovery-snapshot.json` 和 `wiki-current-recovery-snapshot.json`。
