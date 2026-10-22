# M3 文件、缓存、衍生内容与外部来源

**M3-B 尚未完成业务链闭环。** 已有安全组件与合成测试，不代表历史 Wiki、公开缓存、CDN、对象存储及外部同步已收口。

## 衍生内容

`access/document_resources.go` 的 `BuildDerivedText` 只向生成回调提供通过 `ReadText` 读取的当前正文；保留所有输入文档的 opaque read Grant、内容 digest，生成前后及每次 `DerivedText.Read` 逐一核实来源。一个来源撤权或版本变化即拒绝整个合成结果；没有来源证明的 nil/历史缓存拒绝。该证明仅在内存中有效，未实现 Wiki/FAQ/图谱/历史消息的持久化来源模型和存量映射。

| 资源 | 下一步可信依赖要求 | 当前处理 |
| --- | --- | --- |
| Wiki 页面、目录、索引、修订和摘要 | 每个输出版本 → 全部来源 doc + generation/incarnation + 操作；目录标题/计数同样受控 | 未认证路由 HOLD；现有 source_refs 不能直接认作完整来源证明 |
| FAQ / 生成问题 / Summary | 直接 Chunk 归属与来源文档有效版本；生成输入全量来源 | 受控检索不放行生成问题等索引行；现有直接 FAQ API HOLD |
| 图谱节点、边、社区摘要 | 多文档信息的依赖并集，来源缺失即 HOLD | 未实现实际图谱适配 |
| 关联/neighbor Chunk、历史引用、消息/导出 | 每个包含信息的来源都可当前读取，不仅初始命中文档 | 原路径未认证；不能沿引用复制匿名 URL |
| 搜索/模型缓存 | body hash + 全部来源授权快照；命中也同步重验 | 内存 DerivedText 测试；尚未接入 Redis/公共缓存 |

缓存键至少绑定 principal、Key、真实 Tenant/KB/document、operation、政策 revision、文档 epoch 和来源集合；缓存键本身不授予权限。撤权提交后新请求先读取权威策略，再决定是否可使用缓存。清索引/删缓存是后台清理，不是权限生效前提。旧缓存无可靠来源时不可回填猜测来源，不可提供给模型。

## 受控资源 broker

`ResourceBroker.Read` 默认关闭；可信 Store 从 opaque ID 解析精确 Tenant/KB/doc/storage reference，必须有 file 操作 Grant、现有文件权限/旧链接收口 verifier、匹配的文档版本、原始文件 SHA256。限 8MiB 缓冲，比较实际字节 digest，重新解析引用防止中途重绑定，I/O 后重验全部权限，再返回 bytes。不得签 bearer URL、返回存储路径、重定向对象存储。新隔离 HTTP 边界仅认证 JSON，不可拿它直接开放文件；真正 broker HTTP 适配仍待开发，必须 private/no-store、nosniff、受控 Content-Disposition，无共享缓存和 Range/流式绕过。

原文件、预览、图片、附件、导出分别需要精确资源引用和 file 授权，不能由 read/search 自动推导。可变对象的同 key 替换不一定更新 SQL，因此仅有 document epoch 不足；broker 额外验证内容 hash。历史资源没有可信绑定/hash/收口证明，继续 HOLD。

## 旧匿名链接与 CDN

`router/files.go` 的既有 presignedFileHandler、短链、静态文件和相关代理仍不是主体 broker。已签发的 HMAC 链接在原校验规则下仍可能有效，缓存还可能超出有效期。本批没有轮换生产凭据、删除对象、改缓存或 CDN，也未证明云存储可即时撤销。

拟定收口路径：盘点全部签发者/历史引用与存储 ACL；使 backend/edge 不再接受旧匿名路径；对直接对象 URL 另验证 bucket/CDN deny/invalidation/缓存绕过和 stale 响应；对不支持撤销的对象隔离命名空间/代理访问并重新验证；只有可证明所有旧路径已不可使用后才允许新资源引用。实际执行涉及生产变更，当前不执行。不能仅缩短 TTL，不能把“等待一天”当作缓存收口证明，不能把新的应用路由 403 当作既有直连对象链接已撤销。

在缺乏上述证明时，相关 KB/文档 rich 内容、资源和历史引用继续 HOLD；新文本组件保守拒绝资源链接标记，不宣称它是完整 HTML/Markdown 安全净化器。

## 飞书 / GitLab

已核对 `datasource/connector/feishu/{wiki,drive}`、`connector/gitlab` 与 `service/datasource_service.go` 的 FetchIncremental/FetchStream、流式 Emit/Checkpoint、同步与删除路径，清单见 [source-permissions](evidence/batch3/source-permissions.log)。应用账号能拉取 ≠ 员工能读取。现有内容同步/游标/modified time 不是可信 per-user ACL feed。

新 Reader 只接受已审核 local 来源；即便历史策略标记 verified，也因尚未接通实时源权限而 HOLD。内容写回 DB 会变化 epoch；源权限变更而内容未变不会凭空触发 epoch，必须先有可信 ACL connector。后续需：source stable ID → immutable local ID/ACL 保留映射；权限 webhook/周期复核及失联即 HOLD；删除、撤权事件先同步拒绝再清索引；重导入/新 ID 不继承旧审批；增量同步不得覆盖可信 ACL；重新索引不重新授予权限。没有执行真实来源同步或提交生产文档。

已实现/合成验证与未接通内容严格分开见 [22](22_M3_TEST_RESULTS.md)。回滚：保持新组件未注册，隔离资源引用作废；不要通过恢复匿名链接回滚。生产影响无。
