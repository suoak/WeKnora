# 飞书 Docx 重复导出的内容身份修复

## 已复现根因

对同一个受控测试 Docx、同一个源修改时间执行两次真实导出，导出任务和下载均成功，但 ZIP 原始字节不同。`word/document.xml`、图片、关系及其他部件一致；差异是 `docProps/core.xml` 的导出生成时间和 `word/numbering.xml` 中随机分配的 `abstractNumId`。两份包的编号定义多重集以及 `numId` 到定义的映射一致。

原 `StageFeishuLegacyItem` 用原始字节、文件名和标题生成 SourceHash。`FeishuLegacyStore.Begin` 将同一修改时间绑定到一个哈希，因此合法的重复导出触发 `source_revision_content_conflict`，失败重试也无法完成。

## 修复和保护边界

- `internal/feishudocx` 生成有版本的完整包摘要，覆盖所有 ZIP 部件。仅归一化已复现的导出时间和编号定义 ID，保留正文、表格、图片、关系、修改时间、编号实例与覆盖规则。
- 畸形或重复部件、危险路径、过量解压、DTD、异常 XML、歧义编号和悬空引用均拒绝。普通上传的 DOCX、附件、旧 Doc、Sheet 和 Bitable 保持原始字节合同。
- `BeginWithSourceIdentity` 从创建私有 attempt 开始保存摘要证明及 manifest SHA；`Prepare` 在锁内验证并保留它。相同版本的真正内容变化继续拒绝，claim、generation、lease 和原子发布检查保留。
- 已发布旧版本只有在受控存储中读取原归档、验证原始 SourceHash、确认完整包等价，并在事务内复验版本和存储绑定后，才能接受重复导出。旧 manifest、receipt 和原始哈希不改写。
- 没有原始归档的旧失败记录无法证明等价，继续拒绝并保留现有知识。需要可验证的旧归档或真实的新源版本；不能通过清空 cursor、删除 attempt 或解除 hold 消除冲突。
- 摘要证明使用现有 manifest JSON 的可选字段，无数据库迁移；旧字段缺省时原 manifest SHA 不变。

## 验证入口

```sh
go test -race ./internal/feishudocx
go test -race -tags sqlite_fts5 ./internal/application/repository ./internal/application/service \
  -run 'DOCX|SourceIdentity'
```

真实 PostgreSQL 测试必须设置指向隔离 `feishu_atomic_test` 的 `FEISHU_LEGACY_TEST_DSN`，测试工具为每次调用创建独立数据库。集成 CI 新增专门的摘要及历史兼容步骤，保留精确提交 SHA 和 JSON 测试产物。

场景覆盖等价导出、真实内容变化、编号关系变化、失败后重试、租约竞争、过期证明、manifest 篡改、真实文件服务读取、缺失归档、原始上传路径、旧版本向量和 BM25 检索。隔离 APP 验收使用原先选定的测试文档，完整同步两次，确认实际再次导出后的版本与父子图保持一致；原失败记录单独验证继续安全拒绝。
