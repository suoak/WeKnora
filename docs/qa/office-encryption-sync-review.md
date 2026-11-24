# Office 加密检测同步来源专项评审

状态：待评审，不可据此发布同步行为变更。基线为 suoak main 50f82ae496d78238c0af7332739e7a9d3a49dbb1。

## 风险与拟议改动

`DataSourceService.ingestItem` 对普通 Confluence、Notion、GitLab 等来源仍可能先 DeleteKnowledge、HardDeleteKnowledge，再 CreateKnowledgeFromFile。创建服务加入强制加密拒绝后，如果保持此顺序，旧版本可能丢失。

拟在 ingestItem 入口完成现有来源配置检查之后、任何 StageFeishuLegacyItem、旧记录查询和删除之前，对已有 item.Content 调用 officeguard.Validate。加密时返回业务错误 2400，保留旧记录、索引、文件和队列，不执行本条目的发布替换。其他条目沿用现有处理逻辑。预检不新增暂停、删除、清空队列或发布动作。

飞书 safe legacy staging、个人飞书 dispatch、原文消费者和 exact publication 有独立隔离与签收协议，不能通过普通 CreateKnowledgeFromFile 推断所有路径均受保护。需在各自有权读取到原始字节之后、暂存持久化之前评审接入，保持扫描、凭证、代际、签收和发布条件不变。

仅有 URL 的导入在建立任务之前尚无文件字节。不得为检测另行进行未授权抓取，也不得绕过 SSRF、受控下载器及来源权限。其下载后校验不能承诺在首次数据库记录/入队之前完成。若要求 URL 来源也满足此顺序，需另行评审下载与暂存模型。

## 验收要求

- 旧版本已存在时导入加密新版本，旧文件、知识行、分块、索引、任务及签收均不变。
- 一个同步批次中正常文档继续，拒绝条目不被 stale sweep 当成源端删除。
- 重试保持来源身份和幂等语义，不扩大任何授权。
- 飞书 staging 与 exact publication 的 PostgreSQL 集成测试在隔离测试环境通过。

未经本专项评审及上述验收，统一创建服务的拒绝规则不能作为可发布的同步完整方案。

## 待批准的入口改动

以下代码仅作为评审提案，尚未写入数据源服务。需要在现有来源配置/引用检查之后、`safeLegacyIncrementalEnabled` 路由和来源更新查询之前接入，并增加“拒绝条目不触发删除、暂存或 sweep”的业务回归。

```go
if len(item.Content) > 0 {
    if err := officeguard.Validate(ctx, bytes.NewReader(item.Content),
        int64(len(item.Content)), item.FileName); err != nil {
        return false, err
    }
}
```

该改动只覆盖已取得原始字节的 ingestItem；不能把它当成个人原文消费者、独立暂存/重放入口或 URL 下载链路的全覆盖方案。
