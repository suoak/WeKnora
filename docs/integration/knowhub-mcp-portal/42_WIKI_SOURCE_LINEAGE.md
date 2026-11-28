# Personal MCP Wiki 来源引用

`wiki_read_page` 的每个页面可返回 `provenance`，用于从当前已验证的 Wiki 内容继续读取实际来源。

1. 使用 `wiki_search` 查找页面。省略知识库筛选时，搜索当前授权范围内可用的 Wiki。
2. 将搜索结果中的 `slug` 和准确的 `knowledge_base_id` 传给 `wiki_read_page`，避免同名页面歧义。
3. 查看 `pages[].provenance`。`revision_sha256` 标识通过来源校验的页面内容版本；`sources` 只来自该页面记录的 `SourceRefs`，不把生成时的整个知识库输入闭包当作引用。
4. 对需要核验的来源，使用 `sources[].knowledge_id` 调用 `read_document`。按其分页协议继续读取，使用 `chunks[].chunk_id` 对照返回的原文 Chunk，核对事实。`reference` 与 `chunk_id` 沿用原文读取接口的引用组合。

| 字段 | 含义 |
|---|---|
| `page_id`、`revision_sha256` | 当前封存页面及内容摘要，不冒充历史整数修订号 |
| `status` | `complete` 表示记录的引用清单已完成绑定检查；`partial` 表示有引用无法验证或超过安全读取上限 |
| `source_count`、`recorded_chunk_count` | 页面记录的去重来源数及 Chunk 数；不完整时可能大于实际返回数量 |
| `sources[].title` | 从当前规范文档字段取得的名称，忽略来源引用中的任意显示名称 |
| `sources[].source_version_sha256` | 页面生成凭据绑定的文档输入版本摘要 |
| `sources[].attribution_level` | `chunk` 或 `document`；文档级摘要没有 Chunk 记录时，不补造 Chunk 引用 |
| `sources[].chunks[]` | 当前存在、启用、未删除的文本 Chunk，且其父文档必须是该页面记录的来源 |

来源归因表示页面记录的生成来源，不等于逐句事实已被验证。不能仅凭页面或文档标题认定答案有依据；客户端应继续读取相应原文。

来源信息和页面正文经过相同的最终页面、来源与凭据实时检查。若原文已修改、撤权或页面已归档，应重新获取页面和来源，不能把旧缓存当作当前授权。分开的读取调用不会固定一个跨请求历史快照；此接口也不提供缺少可靠来源记录的历史版本回溯。`partial` 或未返回来源时，应保留该限制，不能推断来源不存在或把整个知识库补成引用。
