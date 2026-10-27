# 按资源选择内容获取方式：权限边界与完整性评估

2026-10-26，V2 独立分支。结论：保留官方下载/导出优先，将权限证据、获取方式和内容完整性拆开判断。`view=true, copy=true, export=false` 目前仍拒绝 Blocks 持久化导入；公开官方文档尚不足以证明这种用途获准。本次仅完成审查、方案与拒绝路径回归，未放宽运行时门禁、切换既有来源或部署服务。

## 1. 依据及仍未证明的边界

飞书官方[判断用户权限](https://open.feishu.cn/document/server-docs/docs/permission/permission-member/auth)分别定义 view/export/copy 动作，`auth_result` 才表示当前身份的实际许可。官方[云文档权限概述](https://go.feishu.cn/s/69PgltOmw0s)及[权限设置](https://go.feishu.cn/s/6h10y50zc04)将复制内容与创建副本、打印、下载分开设置，企业及知识空间也可能施加限制。

官方[获取文档所有块](https://go.feishu.cn/s/68niwzFig0s)描述了内容读取接口及版本权限：最新版本需要阅读权限，历史版本需要编辑权限。这证明 API 的读取要求，**没有证明 export=false 时可将正文、表格及资产持久化到另一个知识系统**。也没有证明 copy=true 包括下载图片、附件、转交外部模型处理或按目标 KB 可见范围再发布。当前技术审查不能替代飞书对这种组合的正式说明及企业合规判断。

因此，copy=true/export=false 是“可研究的受限组合”，不直接判为可导入，也不与 copy=false/export=false 混为相同错误。继续核验应取得针对该 API 使用场景的官方说明或书面答复，明确覆盖持久化、解析/Embedding、图片附件和后续同步，再核对企业规则及 KB 发布授权。企业批准不能覆盖飞书明确不允许的行为；仅仅成功调用 Blocks、管理员勾选开关或用户有 KB 写权限均不能充当依据。

报告 [22](22_FIRST_CONSENT_AND_EFFECTIVE_PERMISSIONS.md) 的真实文档为 `view=true, copy=false, export=false`，不属于待研究放行组合，继续拒绝；本轮没有再次读取其 Blocks 或正文。

## 2. 现有代码的实际行为

审查位置：

- `core/personal_resource.go`：个人身份先走 `downloadResource`，成功时保留原导出文件及 DocReader/AnyDoc 路径，失败才进入 `personalBlocksFallback`。
- `core/personal_blocks.go:personalBlocksPermission`：依次核验 view/export/copy，要求三项全 true。遇到 false 立即返回，因此 export=false 时不会继续收集 copy 结果。普通布尔值也无法完整表达“缺 Scope、源端拒绝、策略待确认”。诊断工具已独立收集三项，不代表运行时已有完整权限快照。
- `personalBlocksFallback`：目前仅 Docx、仅创建导出任务阶段的若干分类失败可尝试兜底；认证、超时、429、资源不存在等保留原有停止/有限重试语义。它仍将部分 permission/scope/permanent 类错误列为候选，未来选择器需要更细的原因白名单，不能将所有 403 或永久错误一概视为可换接口。
- `finishPersonalBlocks`：读取后再次核验三项权限、源版本，并拒绝有待处理附件、空正文等情况。

三项全 true 是本分支当前的保守准入条件，不能描述为飞书规定所有 Blocks 调用必须拥有导出权限。它确实排除了潜在合法的“仅允许内容复制”场景；在该场景边界确认前，不应简单改成 `view && copy`，也不应改成 `view && (export || copy)`。

当前转换器也不是完整富文档实现。虽然分页、循环、重复块、缺子节点、50,000 块上限、未知富文本等已有拒绝保护，图片、附件和嵌入表格仍被个人路径的支持白名单拒绝。原生表格的 `cellText` 只连接直接子块文本；复杂嵌套、合并关系、任务完成状态、列表层级等尚未完整建模。不能仅凭“所有块均已返回”或 Markdown 非空就声称保真完成。

## 3. 腾讯上游的参考范围

本次代码对照固定为本地已取回的官方 `upstream/main` **`e1f9a6166a593a4804a2f99f1d942919c9cc8ea7`**，提交时间 2026-10-24；不宣称它是本次网络核验后的最新 HEAD。审查文件：

- [shared.go](https://github.com/Tencent/WeKnora/blob/e1f9a6166a593a4804a2f99f1d942919c9cc8ea7/internal/datasource/connector/feishu/core/shared.go)：资源级 parse_mode、`FetchDocxWithBlocks`、图片资产和附件处理。
- [markdown.go](https://github.com/Tencent/WeKnora/blob/e1f9a6166a593a4804a2f99f1d942919c9cc8ea7/internal/datasource/connector/feishu/core/markdown.go)：树结构、富文本、嵌入对象与资产引用。
- [blocks.go](https://github.com/Tencent/WeKnora/blob/e1f9a6166a593a4804a2f99f1d942919c9cc8ea7/internal/datasource/connector/feishu/core/blocks.go)：块分页、嵌入表格和记录获取。

已合并的 [PR #2334](https://github.com/Tencent/WeKnora/pull/2334)是 Blocks 下钻能力的起点；后续固定版本已有更丰富的树渲染和图片内联。可借鉴解析与关联方法，不能将上游接入方式当作源端授权依据。

上游当前对照版本仍包含本任务不能直接接受的降级：默认 Blocks、图片失败/超限时占位、附件按白名单及大小过滤、表格/块超限后截断并注明等。它们适用于另一种产品完整性取舍；本分支需要“任何必需内容缺失均不发布”的独立结果清单，不能原样复制这些成功语义。也不采用上游默认模式来修改所有已有 DataSource。

## 4. 后台按资源选择的决策表

以下为建议方案，未将待确认组合启用。始终使用原个人 OAuth、当前用户、同一规范化资源及合法 KB 目标。

| 情况 | 自动处理 |
| --- | --- |
| 官方下载/导出成功 | 保留原文件，走现有 DocReader/AnyDoc、结构化表格校验、staging 与发布；不因部署新选择器重跑旧来源 |
| 超时、429、明确暂时性错误 | 沿用预算、来源限流、退避和有限重试；不立即换 Blocks 规避错误或预算 |
| access 到期且 refresh 有效 | 通过既有 UserToken 合法刷新并有限重试原请求 |
| Scope 不足、refresh 失效或授权撤销 | 分别提示应用配置/用户授权问题；不补造 Scope、不切应用身份、不把改 API 当作权限修复 |
| view=false 或权限证据缺失 | 不读取正文；保留明确拒绝/待处理类别 |
| view=true，copy=false，export=false | 硬拒绝；不调用 Blocks、其他正文 API或网页抓取 |
| view=true，copy=true，export=false | 默认“源端策略尚未确认”；仅在官方边界、企业政策及目标发布授权均有可审计依据后，才可设计受控准入 |
| view/export/copy 全 true，导出失败 | 仅对已确认属于接口能力/格式不支持的失败，且没有更高层源端禁止时，考虑能力已验收的 Docx Blocks；无法解释的 403 仍停止 |
| Blocks 存在不支持块、缺分页/资产、解析失败或版本变化 | 不发布，不把占位、截断或零资产计数作为成功；可恢复故障按原预算重试 |

表格中后三项针对失败后的 Blocks 路径。已有导出成功路径继续遵循飞书实际导出许可及现有发布检查；不把新的三项全 true 限制追溯套到所有已正常导出的资源。

具体类型：Docx 作为首个受控 Blocks 能力；Wiki 先解析实际对象后按真实类型选择，节点本身不授予整个空间。Sheet/Bitable 继续优先 xlsx/csv 及现有结构化校验；在官方读取 API 的所有字段、记录、关系和附件覆盖得到验收前，不切到 GFM 扁平文本。普通文件继续官方文件下载。普通文档链接不扩展到文件夹、Wiki 空间、关联表或其他用户文档。

## 5. 建议的代码边界

1. `inspectPermissions` 返回三态权限快照：每项 allowed/denied/unknown、错误类别、检查时间、当前连接/授权代际、资源身份与源版本。收集完整三项用于分类，保留逐请求限流；检查失败绝不转换为 false 后“猜测放行”。
2. `selectAcquisition` 接收导出阶段/错误、权限快照、源端策略依据、企业规则、KB 发布授权和类型能力。输出 export / blocks / retry / blocked 及稳定原因码；纯决策代码不下载正文。未确认场景继续 blocked，不能被全局布尔开关放开。
3. `fetchAndVerify` 生成共同的内容包：源版本、正文表示、结构树、表格模式、图片/附件清单、各项校验结果、内容摘要和解析器版本。官方原文件与 Blocks 包均进入既有后端流水线，不另建同步引擎。
4. `commit` 继续复用 attempt、staging、outbox、文档版本保护与原子发布。提交前重验身份代际、有效来源权限、KB 授权、资产齐备与源版本；临时文件不能先进入可检索范围。

copy=true/export=false 若将来获准，其策略依据必须限定具体使用目的、资源/资源类型、身份、KB 受众、资产能力、期限及审批记录。官方明确不允许的使用场景不能靠企业策略覆盖；批准撤销、受众变化或证据失效立即停止，不要求普通用户逐篇选择获取模式。

## 6. Blocks 完整性验收门禁

| 内容 | 必须证明的结果 |
| --- | --- |
| 正文与树 | 完整分页、无重复/循环/孤立/缺子节点；按父子顺序保留标题、层级列表、任务状态、代码、链接和必要富文本语义；未知类型/元素明确拒绝 |
| 原生表格 | 行列、合并单元格、表头语义及单元格嵌套内容保留；GFM 不足以表达时使用经既有解析器验证的结构化载体，不能默默截列或移除子内容 |
| Sheet/Bitable 嵌入对象 | 独立权限与范围、全量分页、字段/记录 ID、类型、关联、日期时区、公式/显示值的语义对应；不自动追踪无授权的关联资源 |
| 图片 | 官方同身份素材下载、权限和文件类型/大小校验、原始资产摘要及位置关联；必要的 OCR/VLM/图像处理完成后才能完成该资源。OCR 文本不等于完整图像语义；不以占位符或关闭多模态后跳图作为成功 |
| 附件 | 资产数量与引用全覆盖，官方下载校验与现有安全解析入 staging；不跳过小文件、超限或非白名单文件后声称完整。不能安全解析的必需附件令父资源需要处理 |
| 版本 | 文档 revision_id 与现有源版本证据配对；分页与资产获取前后重验，变化则放弃候选并有限重试。不要为冻结已变成历史的版本要求只读用户申请编辑权限 |
| 发布 | 父正文、必需资产及索引全部验证后一次发布；子资产沿用父资源权限/版本，保留原父 Knowledge ID，派生资产标识稳定，失败时不先删除上一版资产 |

官方[素材概述](https://go.feishu.cn/s/63soQp6NY0s)单独描述图片/文件 token 和素材下载；高级权限 Bitable 还存在独立的 extra 鉴权要求。copy=true 不能代替这些资产的下载许可。若资产无法合法获取，完整性门禁会使整篇资源保持需要处理，不发布“只有正文”的成功版本。

## 7. 防止重建与同步影响

获取方式、转换器版本与完整性摘要记录到本次 attempt/内容清单，不修改规范化 ExternalID、不重建来源、不向存量 Config 写入 parse_mode、不清 Cursor，也不把获取方式当作新的飞书源版本。保持当前 source/Knowledge 绑定和增量去重。

只有来源真实变化或独立授权的定向修复才进入新候选处理。相同源版本不能仅因全局开关、换解析器或暂时导出失败而触发全库重新 Embedding。若修复确有缺失内容的已入库文档，单独限定来源与版本，沿用原 Knowledge ID，并做父子资产和 Portal/MCP 版本对账。

自动更新停止锁、Bitable hold、paused/revoked、24 项存量缺口、PR #50 和受保护来源保持原状态。本次没有 SSH 配置变更、恢复队列、登记新共享策略、自然同步复验或测试站升级；不能把本次策略审查计作自动更新验收。

## 8. 本次已验证及后续实施顺序

增加 `view_denied` 与 `copy_and_export_denied` 回归，保留原 `export_denied`（view=true、copy=true）场景。三个边界均断言没有调用 Blocks；其他已有用例覆盖同身份、导出优先、Scope/字段缺失、撤权、版本变化、图片/未知元素/缺分页/循环及暂时故障。

本次运行结果见[回归与源码指纹](evidence/resource-acquisition-strategy-review-20261026.json)，不将合成测试写成新增真实导入成功。运行时拒绝逻辑未改动。

后续顺序：先补齐权限快照与错误分类；再完成有明确许可的 Blocks 内容包及资产完整性；通过定向真实富文档验收后启用按资源自动选择。受限 copy-only 场景必须先取得上述正式依据，再评估是否能准入；该等待不阻塞独立的自动更新停止锁诊断与受控恢复验收。
