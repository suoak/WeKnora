# 第二次停止事件：隔离站受控恢复方案

2026-10-26。本方案尚未执行，须取得本轮新的明确批准。前轮消费者丢失恢复的批准已消耗，不能复用。本文仅覆盖测试站 `http://172.30.232.42/`、`test` → `test001` 的两个既有来源；不是生产恢复或正式发布批准。

## 1. 恢复对象与依据

| 对象 | 允许范围 | 当前保护 |
| --- | --- | --- |
| 全局停止 | 仅本隔离项目现有 `legacy-feishu` 命名空间 | 必须精确匹配 `automatic_membership_probe_context_deadline`，不处理其他原因 |
| Sheet `6d1698a6e0d9` | 原来源、原知识库、原个人 OAuth、原 Schedule | error / unmanaged；Cursor 和 Knowledge ID 保留 |
| Bitable `1dcf458346ba` | 原来源的一次获批全量安全对账，随后恢复原 Schedule | error / hold；不直接把 hold 改为 active |
| 持续发布策略 | 同一来源 / 个人授权代际 / 原受众；本次验收有效期 24 小时 | 需既有明确初次发布批准仍有效；受众或身份变化即失效 |

根因及证据见 [报告24](24_THREE_REPOSITORY_ACQUISITION_AND_SYNC.md)。已修复 Step 审计预算错配及 9499 的操作级分类；历史 9499 未保留原消息，仍不宣称它一定是限流。恢复必须重新核验真实个人资源权限，不以错误分类修复替代权限证明。

## 2. 已准备的执行机制

运行时固定 SHA：`2607fbec7b7ee63ee66d83fb0e199f02945074fb`，五组 CI 已通过并已部署；独立本机恢复工具修正 SHA：`80add81ddb9c2c235f7654dcab3464f83b94936a`。后者只改恢复脚本与离线测试，不是另一应用镜像；实际恢复拒绝使用其他运行时 SHA。

- `scripts/deploy/feishu_v2_membership_recovery.py` 使用独立事件、审批引用和状态目录，复用现有停机、租约、permit、队列快照和 Lua CAS 校验。旧脚本及旧审批保持原样。
- 审批引用：`user:report25:approved-membership-and-held-source-reconciliation`。没有本次明确批准，不运行 prepare/quiesce/reset/restart/resume，也不登记本方案的授权。该引用是操作审计标签，不是普通用户点击即可生成的许可。
- `ApproveSourceReconciliation` 仅管理员可调用，要求原初次发布批准、真实 owner/KB 权限和当前个人授权代际；与 continuing policy、具体 SyncLog 同一事务写入。
- Bitable 的 hold 特许限定一个 DB 执行日志及其当前有效 generation，最长 15 分钟；其他同步日志、未领取执行、旧 generation、终态执行、过期、paused/revoked 全部拒绝。普通导入和管理页面不能借此自动恢复 hold。
- 来源仍为 hold 时，仅该日志能进入既有完整同步流水线；读取和发布前重新检查授权、view/export/copy、源版本及目标授权。不存在应用 Token 切换或受限内容 API 绕行。
- 不提前解除 hold。只有完整内容、staging、解析、Chunk、向量、BM25、outbox 和安全发布完成，且全部资源无未解决 hold/revoked，现有原子对账事务才释放来源 hold；最终事务再次核验短期授权。
- 成功后从数据库重读来源控制项并登记原 Cron，无需再次重启进程；暂停、撤权或失效 Schedule 不会被恢复。
- `enqueue_reconciliation` 只允许指定 Bitable 的获批日志，要求原命名空间没有 trip、permit 有效且 sync 仍受控暂停；固定任务 ID，重复请求不产生第二轮。
- 解除队列暂停前必须存在上述唯一获批全量任务；其他排队任务只能来自这两个原来源。Lua 对整个待处理列表再次 CAS 核对，变化即拒绝。
- 现场有两条旧 canceled 执行的残留投递（generation=3、未领取）；恢复前逐条验证来源和执行终态，只允许这类已终态投递在暂停队列中保留。reset 对整个列表 CAS，不删投递、不改日志；有活跃 / 重试 / 延迟任务或非终态 pending 则拒绝恢复。队列继续后由原 Worker 按终态围栏正常确认这些重复投递，不重做内容读取。

没有新增面向普通用户的配置向导。持续策略让已批准资源的后续合法版本由后台处理；它不授予源端权限，也不扩大知识库读者范围。

## 3. 执行顺序（待批准）

1. **门禁**：只部署通过五组同 SHA CI 的修复版到隔离站，确认 APP、worker2 健康，原 trip/hold 未变。验证新的单次执行围栏、授权过期、撤权、原子发布和 PR #50 图片安全回归。
2. **只读复核**：两来源及原 KB、个人身份、GrantEpoch、配置 / Cursor 摘要、Knowledge ID、索引数量 / ID 摘要、原批准及受众一致；依赖和 Redis 短采样正常。来源数、队列、原版本或保护状态不符合预期则不恢复。
3. **准备并停机**：新恢复工具 `prepare` 固定镜像和配置摘要；`quiesce` 对 APP、worker2 正常 drain/stop，等待消费者退出和租约自然到期。任何活跃 / 延迟 / 重试任务或残留消费者阻止继续，不手工删租约。pending 仅允许上述原来源的已终态投递，逐条核验并原样保留。保留新鲜数据库备份及可恢复验证回执。
4. **登记获批策略**：控制器停机、队列关闭时，通过专用 operator 的 `prepare_reconciliation` 原子登记两来源 24 小时的原受众持续发布策略，以及 Bitable 单次日志 / 15 分钟恢复授权；不修改来源配置、原批准、旧知识、索引或 Cursor。登记失败整体回滚。
5. **精确解除本事件锁**：`reset` 重新核对停机、无活动 / 延迟 / 重试任务、pending 仅原来源已终态投递、原状态 / 索引和 membership revision。Lua 对完整 pending 列表再作 CAS，仅清除本事件的 trip，先追加审计；保留每条投递，sync 仍暂停，不清预算或任何来源 hold。
6. **重启及待命**：`restart` 复用原配置启动两个进程，等待健康、两个真实 sync 消费者、同策略、当前 incarnation、活跃 lease 和稳定 permit。出现其他 trip 即停止，不递归解锁。
7. **队列准备**：专用 operator `enqueue_reconciliation` 将唯一获批 Bitable 全量任务入队，仍保持暂停；取该日志的脱敏回执。Sheet 可由原自然调度生成本来源任务，不新增链接或扩大范围。
8. **受控继续**：`resume` 带上获批日志摘要，验证仅两来源待处理任务及唯一获批全量任务，CAS 检查新消费者 / 租约 / permit / membership / 完整队列列表后解除暂停。其他全量任务、其他日志、未知来源或列表变化均拒绝。
9. **完整对账**：Worker 领取上述日志，以原个人身份和原 Knowledge ID 处理。Bitable hold 在完整发布及最终授权校验前持续存在；读取、解析、资产、权限、版本或索引失败则保持 hold 和保护，不手动补写成功。
10. **自然同步验收**：无需用户重新提交链接，跟踪用户此前已修改的两篇文档，核对新源版本、字段 / 行配对、原 Knowledge ID、Chunk、1024维向量、BM25 与发布回执；成功后再跑自然无变化轮次，确认不重复解析 / 生成向量，不错误推进 Cursor。
11. **检索**：使用合法 `test` Portal 会话和其现有 Go/Python MCP 作用域验证新字段对应行；记录脱敏来源、版本、命中、耗时和错误类别。没有有效网页登录会话时，Portal 新值命中需用户实际确认，不伪造其登录身份。MCP 403 不写作通过。
12. **结果判定**：任一步保护条件不满足，停止后续恢复，保留旧版本和诊断，不自动清新的 trip / hold、不切换身份。只有两来源自然改值 / 无变化及三类检索、撤权 / 暂停 / 旧版本 / 限流全部通过，才评估最终发布；仍不合并 main 或部署生产。

## 4. 回滚与边界

停止锁的复位和来源 hold 的释放是两件事。解除全局锁不直接发布或解除 hold；短期授权本身也不会改变可检索知识。若恢复失败，保持 / 重新由现有门禁停止，候选不能覆盖旧发布版本。不得为“恢复原状”删除真实新索引、重置 Cursor、移动知识库或自动重写用户数据。

生产同步范围、24 项存量缺口、PR #50、受保护来源及 NTOS1.0R5-SPEC 不在本方案执行范围。恢复工具没有生产模式，不修改来源白名单、原速率预算、OAuth 模式或 namespace。

本方案与运行证据分别记录。未获得本轮明确批准前，现场状态继续为停止锁保留、Bitable hold 保留、最新值验收未通过。

验收准备证据：[五组同 SHA CI](evidence/acquisition-five-gates-20261026.json)、[已部署健康版本](evidence/acquisition-test-site-deployment-20261026.json)、[来源和待处理投递只读核对](evidence/acquisition-recovery-preconditions-20261026.json)、[临时 Redis 自测](evidence/membership-recovery-disposable-selftest-20261026.json)。最后一项只修改 network=none 的临时 Redis，并已清理容器；既有 Redis 没有被修改。13 项不加载第三方库的离线测试通过，现场恢复尚未 prepare / reset / resume，也尚未登记本方案的持续授权。
