# 文档展示与 OA 会话调整

用户在 2026-11-16 明确要求修复目录宽度、摘要默认展开、提交人展示和 OA 频繁登录，并指定“合入 main → 集中部署 → 线上验证”。本轮用户选择的期限为“最长 24 小时，闲置 8 小时”。此请求是本次会话期限调整的依据。

生产核查得到旧配置：local_approved / oidc_sso_approved，Access、Absolute、AdmissionMaxAge 均为 60m，Idle 为 30m，Refresh=false。

集中部署采用以下配置：

| 配置 | 值 |
| --- | --- |
| SOURCEID_LOCAL_ACCESS_TTL | 24h |
| SOURCEID_LOCAL_ABSOLUTE_TTL | 24h |
| SOURCEID_LOCAL_IDLE_TTL | 8h |
| SOURCEID_ADMISSION_MAX_AGE | 24h |
| SOURCEID_LOCAL_POLICY_REF | user-request:knowledge-ui-session-20261116:24h-8h |

保留现有身份关联、准入模式、准入策略引用和 Refresh=false。新策略引用会使旧会话要求重新认证一次；新登录创建的会话使用新期限。不修改旧 Session、Token 或员工身份记录来延长有效期。闲置按现有服务端认证 API 活动计算；后台请求可能更新活动时间，但不能突破 24 小时绝对期限。

提交人从创建文档时的认证请求记录用户名快照，并在文件列表、卡片和详情中展示。历史记录没有可信归属时显示未知，不使用当前操作者、知识库创建人或空间归属推断。手工正文更新保留原提交人，替换上传不能通过 metadata 覆盖它。

目录右边框可拖动，也可聚焦后使用左右方向键调整，Home/End 跳至最小/最大宽度；宽度保存在当前浏览器。小屏幕维持上下布局。摘要在详情中始终完整显示。
