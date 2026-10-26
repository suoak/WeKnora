# Sheet/Bitable 结构化解析阻塞与原任务恢复

2026-10-26，隔离测试站 `test` → `test001`。本报告继续第 16 份报告；未合并 main、未部署生产。

## 已确认的根因

用户刷新后，10:03:03 提交的新批次 `daaf55b67c5c` 已持久化共享意图，两个条目均绑定当前版本的共享批准。它们完成飞书正文导出并生成原 Knowledge，但没有进入实际表格解析：Worker 记录 `DOCREADER_UNAVAILABLE`，错误标识为 `rpc / not connected`。隔离 Compose 没有 DocReader 服务，也未给 APP/Worker 配置 `DOCREADER_ADDR`。Docx 使用已有 AnyDoc 路径，因此此前 Docx 成功并不能证明 XLSX 路径可用。

这是共享批准之后的独立基础设施阻塞。没有通过把 XLSX 改走通用 Markdown 来绕过结构化解析，也没有关闭下载权限、身份或发布保护。

## 修复前的真实状态

所有 ID 均为 SHA-256 前 12 位脱敏值，正文、资源 URL 和凭据不进入报告。

| 类型 | 条目 | 原 Knowledge | 导出字节 | 条目终态 | Chunk / 向量 |
| --- | --- | --- | --- | --- | --- |
| Sheet | `fc1828dc5a63` | `d0c4602a75ca` | 20,842 | failed；下游 needs_attention | 0 / 0 |
| Bitable | `ad28ecade3ad` | `f48df03f6510` | 13,121 | failed；下游 needs_attention | 0 / 0 |

两个条目在提交后约 50.4 秒转为失败；不能将该时长算作成功入库耗时。导出文件完整性和 attempt/import/candidate 归属核对通过。旧 09:34、07:49 批次保留，没有删除、迁移或伪造批准。

## 源端和导出结构核对

使用 `test` 现有合法个人授权进行只读 API 查询，没有切换成应用身份。

- Sheet：源端 2 张工作表；导出非空行数 13、10（包含首个非空行），非空数据单元格 41、30。
- Bitable：源端 4 张表，字段数 5、5、3、2；记录数 9、8、8、1。导出对应非空行数 10、9、9、2，包含表头；数据单元格 45、40、24、2。
- Bitable 样本没有类型 18/21 的关联字段。不能把这次验收扩展为“已验证真实关联字段完整保留”。XLSX 导出也不单独证明源端稳定字段 ID、记录 ID、关联属性被存储。

## 修复与安全边界

应用修复 SHA：`b11a0b7f1d89cf8ec640bb4eb6c61322f1d47e37`。新增定向、最多 3 次的 DocReader 依赖恢复路径，仅匹配：解析服务已连接、原失败码为 `DOCREADER_UNAVAILABLE`、明确共享意图未过期、当前绑定批准仍有效、连接 epoch 一致、无有效 lease、无 active source、无任何 Chunk/向量/索引成功回执。复用既有 Prepare 和 attempt/staging/outbox 流程，保留原导入、attempt、Knowledge，增加 generation。

不恢复已有部分产物、撤销批准、普通解析错误、超过重试上限或已经受来源管理的任务。不清空 Cursor，不解除 paused/hold/revoked 或版本保护。

五项精确 SHA 的回归均通过：

| 检查 | CI run |
| --- | --- |
| V2 PostgreSQL / race / lint / frontend | 40259900389 |
| 原生解析与 first-create 恢复 | 40259939536 |
| 集成安全发布 | 40259941782 |
| Portal 权限 | 40259940620 |
| Python MCP 权限 | 40259939672 |

DocReader 使用同一 SHA 的原始 Dockerfile 和锁定依赖，在 CI 构建，仅导出镜像工件，不推送公共镜像、不上传真实文档。CI run 40262581894：61 项结构化表格测试，59 通过、2 跳过；全部跳过不计为真实业务验收。镜像转入隔离站前后核对来源、镜像 ID、归档大小与 SHA-256。

## 真实恢复及检索验收

隔离站已升级到 `b11a0b7f1d89cf8ec640bb4eb6c61322f1d47e37`。DocReader 健康，APP/worker2 均健康；原两个条目自动重试 1 次后 succeeded。原 import、attempt、Knowledge ID 未变，没有追加新知识库或重复知识。两份 `personal_index_receipt` 均为 verified，正式 Knowledge 均 completed / enabled，来源成功交接为 source_managed。旧 attempt 在交接后为 obsolete，是正常退役状态。

| 指标 | Sheet | Bitable |
| --- | --- | --- |
| 原 Knowledge 脱敏 ID | `d0c4602a75ca` | `f48df03f6510` |
| DocReader | 110 ms | 189 ms |
| Embedding | 8.760 秒 | 13.659 秒 |
| 恢复后开始解析至处理管线结束 | 9.613 秒 | 14.475 秒 |
| 恢复后开始解析至安全发布/来源交接 | 32.233 秒 | 39.481 秒 |
| 恢复后开始解析至批次条目成功 | 62.823 秒 | 69.978 秒 |
| 批次终态北京时间 | 11:27:45 | 11:27:51 |
| Chunk 总数 | 24 | 30 |
| 文本行 / entity index / summary | 21 / 2 / 1 | 26 / 3 / 1 |
| 向量数量和维度 | 22 × 1024 | 27 × 1024 |
| BM25 实际查询匹配索引行 | 21 | 26 |

所有 Chunk 均 index_status=ready、is_enabled=true。entity index 是结构辅助条目，不生成向量；行记录和摘要已索引。上述处理时长不包含等待修复基础设施的约 84 分钟，不能声称用户首次提交后一分钟内成功。

逐行核对结果：Sheet 9/9 个表头、71/71 个数据单元格，Bitable 15/15 个表头、111/111 个数据单元格匹配。进一步按原工作表、原行号检查，每一行的全部单元格均位于对应行 Chunk，字段名和值的配对也分别 71/71、111/111 通过。日期单元格依据 XLSX number format 从 Excel serial 转为日期文本后比较：Sheet 6 个、Bitable 25 个。两份源端版本在只读核对前后未变化，且均与原导入版本相同。

真实 `test` 个人 MCP 凭据验证：Sheet 两个工作表各一条事实查询，Go keyword/semantic/hybrid 共 6/6 通过，实际 Python MCP hybrid 2/2 通过；Bitable 四张表各一条，分别 12/12、4/4 通过。每例同时要求正确 Knowledge ID 与同一命中内的预期行值，不只检查返回文档名称。样本事实命中率 100%，不是全量问题准确率或排序质量承诺。另有旧 Docx 回归 Go 6/6、Python 2/2 通过。

独立合成 HTTP 回归通过：8 个并发缺失 kb_id 请求全部 400，私人 KB 增量 0；同租户其他管理员不能读取或检索私人 KB。该结果只用于权限回归，不替代上述真实文档验证。

## 自动更新和仍待验收项

只读发现测试环境原配置 `FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS=none`，因此旧来源虽然 active，但从未自动执行同步。现仅在该隔离项目放行本次 Sheet/Bitable 的两个来源 ID；其他来源继续拒绝。保留默认 15 分钟增量调度、原 Cursor、个人身份和全部安全门禁，没有设置 all，也没有修改任何生产停止范围。

**11:45 自然调度未通过。** 两个进程均实际注册 2 条 Cron，APP/worker2 健康、来源白名单恰为上述两个 ID、安全预算保护开启。每个进程都对两个来源各记录一次 `sync safety gate closed`，`sync_logs` 仍为 0。控制器日志记录 `feishu_safety_stop / automatic_permit_consumer_lost`，继而 `automatic_permit_tripped`。这是全局同步控制器的停止锁，不能把 datasource.active 当作后台已在更新。

未删除或改写 Redis 停止锁、未恢复 paused 队列、未切换 namespace 来绕过保护。由于调度在入队前被拒绝，本轮不是“实际执行后识别无变化并跳过”，不能以 Chunk/向量数量不变冒充增量去重通过。后续须先核对实际 Asynq 消费者拓扑、健康租约和部署 drain 记录，按运维恢复流程处理该隔离项目的安全停止，再验证自然轮次和受控改版；保持当前源码 SHA、两个来源白名单与 Cursor，不能通过清除生产停止范围恢复测试。

首次导入的已知依赖故障重试有真实证据：Worker/APP 升级重启后，原两个持久任务自动各恢复一次并验证发布成功。自动更新的受控改版、撤权和限流故障注入尚未执行；相应 PostgreSQL/Asynq 回归通过不能冒充真实文档改版验收。

Portal 真实网页验收：用户使用 `test` 的现有网页登录会话，在 `test001` 搜索表头和熟悉行记录后明确反馈“已命中正确文档和对应行”。此项标为用户实测通过；并非自动化浏览器回放。没有伪造 `test` JWT，也未将 MCP 检索冒充为网页检索。

访问地址：http://172.30.232.42/ 。用户直接进入 `test001` 查看两篇文档或原任务，无需重建知识库、重新提交链接或进入高级审批。站点继续显示 HTTP 隔离测试风险提示。

结论：真实正文导出、结构化解析、索引、安全发布、失败任务重启恢复和 Go/Python MCP 样本检索通过；Portal 用户实测通过。自动更新被真实安全停止锁阻断，完整发布验收仍未通过，暂不建议最终发布。

升级前备份已在独立数据库成功恢复，备份 SHA-256 为 `dd8c07cf0c29363fcc1df3b99cc0b7ae9f88660fe038e8560aac7c26cced853a`。PR #50 仍 OPEN，head `2a532132eb038e0efd5c5564f2f35693577986b6`；五个受保护存量修复文件与 origin/main 的差异为空。未操作 main、生产部署或已有私人 KB 的依赖数据。

脱敏证据在 [structured-parser-recovery](evidence/structured-parser-recovery/)：原始失败快照、逐行及字段配对核对、真实 MCP 事实查询、用户 Portal 反馈、自然调度门禁、精确 SHA 回归和镜像/部署校验。

现有自动创建的私人 KB 保持原状；它存在有效知识、任务、索引及审计依赖，不建议删除或移动。
