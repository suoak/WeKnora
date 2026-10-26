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

状态：进行中。上述修复前诊断和合成/集成回归不能代替真实 Sheet/Bitable 入库。待隔离服务就绪后，核对原 Knowledge 的解析、逐行结构、Chunk、1024 维向量、BM25、索引成功回执、安全发布及 MCP 事实匹配结果。

Portal 真实网页会话仍由用户持有；不得伪造 `test` JWT，也不得把个人 MCP 检索称为已经执行 Portal 网页检索。自动更新的真实改版验证同样需要独立证据，不能因静态索引成功自动宣布通过。

现有自动创建的私人 KB 保持原状；它存在有效知识、任务、索引及审计依赖，不建议删除或移动。
