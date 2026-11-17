# Confluence 资源选择性能优化

## 行为

- Confluence 选择器首屏只读取一页空间，默认 50 个，用户点击“加载更多”后才请求下一页。
- Server/DC 的空间顶层页面及页面直接子项同样逐页加载，每页默认 50 个；Cloud 空间顶层页面也按页读取。
- 按空间名称搜索使用 Confluence `/rest/api/search` 的空间实体查询，单次只取一页；不在本地扫描全站空间。
- 展开节点时携带空间 Key，后端从 Confluence 验证空间 ID 与 Key 的对应关系后读取直接子页面，避免重新枚举所有空间。Cloud 直接按空间 ID 验证。
- 搜索、分页保留已选空间和页面，已选项显示为可移除标签。未加载的已选页面也会使所属空间显示半选状态。
- 列表使用虚拟滚动，只渲染视口附近的行；已选标签每页最多 50 项。
- 批量展开最多并发 4 个子项请求，跳过搜索范围之外的空间。收起、搜索或关闭窗口会停止未发出的批量请求。
- 点击已选页面标签可读取该页面的祖先路径并定位，不枚举全站空间或其全部兄弟页面。Server/DC 的已选空间如果尚未进入缓存，需要先浏览或搜索到该空间；页面定位不受此限制。
- 选中整个父节点后取消某个子项，会先补齐路径上尚未加载的兄弟分页，再一次性提交选择变更。补齐失败保留原选择，避免漏掉后续分页中的兄弟节点。
- 分页缓存按地址、版本、用户名和凭证指纹隔离，缓存有效期从请求成功完成时起算。相同分页请求合并，缓存最多 256 项；旧全量缓存最多 128 项。
- 关闭窗口或新搜索开始后，过期请求的结果不会覆盖当前窗口状态。

## API 兼容

旧调用保持数组返回：

```text
GET /api/v1/datasource/{id}/resources
GET /api/v1/datasource/{id}/resources?parent_id={resource_id}
```

Confluence 前端使用新模式：

```text
GET /api/v1/datasource/{id}/resources?paged=true&limit=50
GET /api/v1/datasource/{id}/resources?paged=true&q={space_name}&limit=50
GET /api/v1/datasource/{id}/resources?paged=true&cursor={next_cursor}&limit=50
GET /api/v1/datasource/{id}/resources?paged=true&parent_id={resource_id}&space_key={space_key}
GET /api/v1/datasource/{id}/resources?paged=true&parent_id={resource_id}&space_key={space_key}&cursor={next_cursor}
GET /api/v1/datasource/{id}/resources?paged=true&reveal_id={selected_page_resource_id}
```

新模式返回 `{ "items": [], "next_cursor": "..." }`，最后一页省略 `next_cursor`。游标与搜索词、父节点、空间 Key 绑定，只携带分页位置，不允许调用者指定出站 URL。分页大小限制为 1–100。

## 验收

1. 同时发布后端与前端，打开 Confluence 选择范围。确认首个 `resources?paged=true` 完成后即可操作，服务端只调用一页 `/rest/api/space`。
2. 加载更多并选择两个空间，搜索第三个空间，确认前两个选择仍保留。
3. 展开某个空间，确认调用单空间详情与该空间直接子页面接口，不调用全量空间列表。
4. 编辑含有未加载页面选择的数据源，确认已有选择不会被清空；取消所属空间的半选状态可清除这些页面选择。
5. 验证搜索失败后的“查看全部”、加载更多失败后的重试，以及关闭并重开窗口。

空间搜索依赖 Confluence 的搜索索引及空间实体查询支持。接口拒绝搜索时可使用“查看全部”分页浏览。Cloud 页面下跨文件夹等透明容器的混合层级仍保留现有完整投影逻辑，该分支可能等待多页；Server/DC 的直接子页面已全部使用分页。此变更不修改实际同步流程。

本地合成回归验证请求数量与状态正确性；实际首屏耗时需要在部署环境测量，不能由合成测试推算。

## 本地验证结果

- Confluence 连接器完整测试通过，包括分页、搜索转义、Cloud 游标、权限缓存隔离、缓存容量、请求合并及路径参数校验。
- 资源接口与同步日志接口专项测试通过，包括旧数组返回、分页参数校验和跨租户拒绝。
- 选择器及多语言扩展回归共 38 项通过，覆盖子页面分页、定位、选择范围保护及批量展开并发限制。
- 两项真实浏览器回归通过：1000 个合成子页面、取消一个后保留 999 个、虚拟列表 DOM 行数小于 80、已选标签不超过 50 个、滚动到底部、手机宽度无横向溢出，以及跨页已选页面定位不读取兄弟列表。
- 前端生产构建通过。本机默认 Less 工作线程曾超时，验证构建使用 `build({ css: { preprocessorMaxWorkers: 0 } })`，没有修改项目构建配置。
- Go 服务、Handler 与 Confluence 包编译通过。Windows Go 测试需补充 `-ldflags=-extldflags=-lssp`；更广的后端测试受既有 SQLite 临时文件占用与 DuckDB 的 `rstrtmgr` 链接库缺失影响，未认定为全量通过。

未部署到线上，也未使用会话中提供的访问令牌调用线上 API。

浏览器回归入口：`cd frontend` 后运行 `npx playwright test --config tests/datasource-picker/playwright.config.ts`。测试使用合成的 1000 个子页面，检查取消一个子项后保留其余 999 个、视口 DOM 行数上限和滚动到列表末尾；不连接真实 Confluence。
