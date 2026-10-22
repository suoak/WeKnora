> **Superseded policy:** First-approval and real-time employment requirements in this historical report have been replaced. See [55: final dynamic Portal policy](55_DYNAMIC_PORTAL_PUBLIC_REQUIREMENTS.md). Historical test passes do not validate the new candidate.

# 第七批：首次确认与普通资料继承

## 已实现源码

`internal/application/service/portal_publication_review.go`：

- `portalService.PublicationInventory` 自动枚举当前空间 KB，汇总文档数量、未就绪数量、外部/未知渠道线索、策略状态、revision 和继承设置。存在策略表时读取原记录；3033 未安装分类表时显示 `pending_review`。不读取正文、不据此认定内容敏感。
- `PortalFirstConfirmation.Apply` 以空间、KB 和当前完整文档集合为一个确认包，校验资源所属 tenant/KB；复用 `access.ChangeEnterprisePolicy` 的可信主体、独立 authority、revision/CAS 与原策略事务审计。整个包在外层事务中原子提交，任一旧版本或授权失败全部回滚。
- 同一人不得提出和批准。首次普通确认不能覆盖已有 deny/mcp_blocked/restricted、混合或来源未知策略。撤回可只指定 KB，且复用收权优先逻辑，不等待完整客户端快照。

`internal/application/repository/enterprise_inheritance.go` 的 `InheritNewOrdinary`：

- 锁定真实 KB；Portal Published、空间当前批准、KB ordinary/local 已批准、`inherit_new_ordinary=true` 为前提。
- 只允许批准后新增的一篇本地普通资源；重建“排除此篇新增文档”的旧 KB 集合指纹，验证集合未发生其他变化。
- `OrdinarySourceVerifier` 必须对当前文档及 epoch 给出可审计规则引用。没有 verifier、未知/外部/敏感来源、现有文档策略、旧成员集合不匹配都拒绝。
- 插入同一 `knowledge_access_policies` 表并 CAS 更新 KB 版本及 revision；与 `enterprise.policy_inherit` 系统审计原子提交。不建立第二个 public 事实来源，不冒充历史审核人执行了新人工审核。
- 文档内容/Chunk 后续变化由 3035 epoch 使旧批准失效，重复导入不能覆盖原策略。

**目前没有将任意 Web/API 上传都视作可信普通来源。正式分类器、上传/同步可信装配及并发重试契约尚未完成，正常生产写入路径仍不自动继承。** 同时新增多篇导致旧集合指纹不匹配时拒绝；不能把该实现称作完整批量导入继承。该限制需下一步以同事务写入事件/批次版本化契约处理。

## 产品入口及权限

现有 Portal 管理页面增加 `PortalKnowledgePublication.vue`，不创建团队 MCP 管理页。使用原 SystemAdmin 路由读取 `/api/v1/system/admin/portal/spaces/:tenant_id/knowledge-publication`。此权限只允许管理清单展示，不授予公开批准能力。

界面分别显示 Portal 发布、KB 总数、已确认记录数、待审核/受限、外部来源线索与默认关闭的公共入口。记录已批准不等同当前可检索。正式 authority 尚未绑定，故没有可点击的公开批准按钮。最近确认责任人/时间/原因的完整管理展示仍待补齐，原数据库审计已保留。

## 实测边界

合成 SQL 测试 `TestPortalM7FirstConfirmationAtomicAuthorityAndWithdrawal` 覆盖缺 authority、独立审核、旧 revision 事务及审计回滚、实时公共范围、KB 单项撤回和旧批准不能复活。

`TestEnterpriseSnapshotM7OrdinaryInheritance` 覆盖 ordinary 成功、缺 verifier、未知源、成员集合变化、已有 deny、重复继承与 Chunk 变化失效。测试结果及候选见 [51 测试报告](51_SEVENTH_BATCH_TEST_RESULTS.md)。

历史生产 48 个 KB 的确认状态仍为零，未执行任何生产确认包或继承写入。审核清单见 [50 存量清单](50_SEVENTH_BATCH_REVIEW_INVENTORY.md)。

## 回滚

停用新增管理清单及继承调用装配，不删除原审计。策略撤回使用原 CAS 操作；生产未运行该功能，不需要回滚生产数据库。整体混合敏感 ACL、外部源细粒度权限及文件链接治理继续 HOLD。
