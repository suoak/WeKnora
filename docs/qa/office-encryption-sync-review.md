# Office 加密拦截：同步与飞书专项评审

更新于 2026-11-25（北京时间）。用户已明确授权修复同步旧版本丢失和飞书独立写入路径。下述改动已经实现；测试站专项验收尚未完成，本文不是上线批准。

## 调用顺序与旧版本保留

原普通同步链路为 ingestItem → 查询旧 Knowledge → DeleteKnowledge → HardDeleteKnowledge → CreateKnowledgeFromFile。DeleteKnowledge 先改变知识状态并发起删除清理；HardDeleteKnowledge 进一步清除记录。创建服务此时才拒绝加密新文件，无法保证恢复旧文件、分块或索引。仅在创建服务添加拒绝不构成完整方案。

当前顺序为：现有来源配置检查 → 对已有 item.Content 执行有界检测 → 既有 Feishu staging 分流或普通更新链路。明确加密在旧记录查询、删除、暂存和发布替换之前返回 2400。正常文件仍走原更新流程；没有保证其他类型创建失败都能保留旧版本。

office_sync.go 集中记录单条 office_encrypted 失败。普通来源继续同批正常条目；飞书返回现有 DocumentFailureError，不触发来源级暂停或权限失效。已获得字节的父项先通过检测，再执行原子树处理；仍存在但被拒绝的子项继续列入现有保留集合。

## 拒绝、删除与游标

飞书适配器在授权下载后、格式校验及降级之前检查原始字节，明确加密保持类型化错误。错误项同时标注 fetch_document、content_refused、office_encrypted；同步消费只在这三个元数据同时匹配时按内容拒绝处理，并在 IsDeleted 分支之前判断。内容拒绝不等同于源端删除或撤权。

既有 Engine 在下载前登记 seen ID。拒绝保留 ID 和上一成功修订，记录 failed-document cursor；正常兄弟项继续，游标序列化重启后仍可重试。没有生成 tombstone。飞书保留签收仍经过 retainVerifiedFeishuItem 和执行代际检查，不能据此恢复 held/revoked 来源。真正的源端删除、撤权及 stale sweep 原规则保持生效。

## 飞书独立路径

| 文件 | 检测位置 |
| --- | --- |
| core/personal_resource.go | 用户身份下载/导出后、OOXML/PDF 格式校验前；加密拒绝不触发 blocks fallback |
| feishu_personal_recovery.go | 已授权下载与修订检查后、manifest 写入前；沿用 needs_attention 协议 |
| feishu_personal_manifest.go | 主体及附件全部检测后才创建目录；读回先验证所有权/摘要再检查内容，防止旧加密暂存重放 |
| feishu_personal_first_create.go | 桥接 SaveFile/claim 前；dispatchable 在原授权后检查 manifest |
| feishu_legacy_staging.go | 原配置及知识库范围检查后、attempt 与 SaveFile 前 |
| feishu_legacy_assets.go | 原 owner/renew 检查后、附件 SaveFile 前 |
| feishu_personal_exact_document.go | 原文摘要校验后，拒绝加密文档证明 |
| feishu_personal_exact_consumer.go | proof 原文进入解析器前 |
| feishu_personal_exact_publish.go | 新鲜证明再校验后、发布事务前 |
| feishu_personal_exact_resume_images.go | 空输出恢复先通过当前证明检查，再检测拟保存字节；保留图片复用也拒绝伪装 Office 的字节 |

逐项检查飞书服务中直接 SaveFile/SaveBytes 调用点。图片专用路径不扩展 Office 格式权限。OAuth、凭证引用、来源身份、幂等 claim、代际、签收、发布条件及个人更新禁用策略均未解除。个人增量更新测试仍应返回既有 ErrPersonalUpdateDisabled，不能强制开启更新来让测试通过。

## 集成证据与边界

office_sync_postgres_test.go 使用隔离 PostgreSQL schema、真实 Knowledge/Chunk 仓储、本地旧文件和 PostgreSQL GIN 全文索引，比较拒绝前后完整快照及检索结果，覆盖七种加密扩展名、混合批次、父子保留和伪装删除元数据。该手工索引表不等同于生产向量引擎，因此另有原生联合测试。

feishu_source_pipeline_native_v2_test.go 使用真实迁移、ParadeDB、原生解析器、Knowledge/Chunk、向量与 BM25 行及本地文件，验证发布后的加密更新不改变旧快照、不调用模型；随后正常版本仍能发布，撤权仍拒绝。源端 HTTP 和模型为受控 fixture。安全控制器使用实际隔离 Redis、worker inventory、TTL 健康协议及 SQL fencing；未清除锁或写入 permit key。

首次导入、重复投递、禁用的个人更新和 exact 图片恢复均有 SQL 测试。原生联合测试首次失败于复用旧修订上下文，被既有一致性校验正确阻止；已让测试通过正常来源绑定获取新修订。最终执行结果、失败日志及复测见 [测试报告](office-encryption-report.md)。

仅 URL 且尚无字节的来源仍不能承诺首次数据库记录/任务之前识别。未增加额外抓取或绕过 SSRF。尚未取得认证及隔离证明的测试站不执行同步任务、暂停/恢复队列或版本替换。

## 发布与回滚约束

代码层面的 P0 预检顺序已修复，合入仍取决于关键测试与测试站专项验收。无数据库迁移、生产数据修复或凭证变更。回滚使用审核后的逐提交 revert 并恢复配套前后端产物；不得删除已保留旧版本或重置飞书安全锁。回滚会重新暴露加密上传及原同步先删后建的风险，应在批准后执行。
