# 权限预检和授权协作

状态：设计；本轮没有发送通知、修改协作者或恢复来源。

| 分类 | 依据 | 产品处理 |
| --- | --- | --- |
| readable | 当前指定身份实际读取对应内容 API 成功 | 仅允许进入下一步，发布仍需 KB/共享批准和执行前再验证 |
| identity_no_read | 指定身份被明确拒绝读取 | 记录待授权，不能借用他人 Token 或改用应用 |
| application_resource_grant_missing | 应用资源 ACL 不足且有可靠错误依据 | 生成应用授权指引；不把所有403都归为此类 |
| api_scope_missing | 官方错误码指明应用/用户 scope 缺失 | 管理员发布权限 + 用户重新授权；与资源授权分别处理 |
| export_or_download_prohibited | 实际导出/下载被策略拒绝 | 终止导入，不改用 blocks 抽取以规避源下载策略 |
| owner_collaboration_required | 当前用户可读但没有授权管理权 | 汇总资源与目录，交有权负责人处理 |
| protected_or_revoked | 本地正式保护或来源撤权 | 阻止 admission/恢复；NTOS1.0R5-SPEC 不变 |
| unknown / transient | 无法确定 scope/ACL、资源不存在或供应商临时错误 | 未验证，不标 readable；有界重试或人工核对 |

读取 metadata、列目录、取 blocks、导出、读媒体、高级权限行列是分开的结果。预检报告保存指定 identity/reference generation、资源 revision 和检查时间，不能用过期报告替代执行时读取。错误消息使用本地稳定类别，不能回传 provider 原始 body、Token、完整下载 URL 或凭据片段。

协作单按 owner/tenant/KB/资源权限限制可见，保存 requester、受理负责人、目标资源、拟议授权范围、当前阻塞类型、处理状态、有效期和审计事件。负责人由明确分配或现有可信目录选择，不能从标题猜测。列表仅分享用户已有权披露的元数据；无访问权限时不能向外泄露文档标题。

共同父目录可以提出目录级授权建议，但目录归属、继承规则、外部成员限制和应用群组授权方式须由有权者核对。B 阅读者可用个人身份访问允许读取/导出的内容，但不能代替 A 给应用授权。应用权限不足时不能自动调用成员管理接口。高级 Bitable 权限缺失或空结果必须人工核对，不能将空表判为完整成功。

处理流：blocked → assigned → owner_action_reported → rechecking → ready / still_blocked。负责人点击“已处理”不是放行依据；用原用户/原应用身份重新预检，检查批准范围和来源状态后，才允许显式 retry。复用 document_deferrals 和 pending cursor：仅释放与授权问题、原 generation 和原资源相匹配的可恢复项；原 lease/claim/epoch 仍有效才继续，否则创建新的安全 attempt。不得清 cursor、解除人工 hold 或恢复 revoked/paused 来源。

通知首期提供站内协作单与复制链接。飞书消息没有在本轮获准或实现，不能显示“已通知”；后续接入明确消息能力和接收人、通知审计及幂等后再发送。源管理者授权和 KnowHub 共享发布批准是两份不同的授权。
