# RC3 普通员工真实联合验收

2026-10-20，在现有测试站点使用用户亲自在专用 Chrome 正常 SID 登录的账号，无管理员 Token、数据库改权或跳过验签。账号 is_system_admin=false，个人空间 owner、受邀空间 10000 contributor。两次 RC3 登录 user_id SHA256 相同（见脱敏证据），同一员工绑定未重复创建。HTTP 风险仍在，不是传输安全 PASS。

## 实际结果

| 项目 | 结果与证据 |
|---|---|
| 普通员工 OA 重复登录 | 两次正常进入 Portal；同一 user_id；auth/me 200 |
| RJGH / RJEMAIL 与既有身份 | DB 保持一条该员工身份、原有 JIT 记录及匹配邮箱；未导出属性原值 |
| 有权 KB | 读取原共享 KB 200；通过正常产品 API 创建验收 KB 并写入成功 |
| 无权限访问/管理 | 8 个真实 403：跨空间 KB、文档、Portal/Hybrid、导入/建 KB，以及系统飞书配置/测试 |
| 应用状态 | application_grant 可用；普通接口未暴露 App ID/Secret |
| 新三格式导入 | 全部最终 parse=completed、index=verified、synced，非仅 queued |
| 同用户同 KB 重复导入 | 三种格式均返回相同 Import ID 与 KID，无额外 Knowledge |
| 跨 KB 同资源 | 新验收 KB 各自创建对应知识；原三篇保持不变 |
| Portal / Hybrid | 三种格式均有非空正文、正确 KID 和飞书来源；见下表 |
| 退出 | 两次 logout 200，旧 Access 均 401 |
| Refresh | 当前账号未签发 Refresh，NOT_AVAILABLE，不写成撤销 PASS |
| 原三篇旧文档 | 停更开关 true 下均 completed，来源有效，Portal 与 Hybrid 可检索 |

新验收 KB：`b1bbb346-3b7f-4ec6-8ef3-9908fe9fef2a`，未重建原有 KB。

| 类型 | 新 Knowledge ID | 新 Portal / Hybrid 命中 | 旧三篇 Portal / Hybrid 命中 |
|---|---|---:|---:|
| Docx | `0afa6733-e1d9-4e69-8a10-315a5b0ba599` | 5 / 10 | 6 / 10 |
| Sheet | `900ee6d0-17e5-4225-a16f-ff2b02d3d516` | 3 / 10 | 2 / 10 |
| Bitable | `b8a42836-f031-4331-b25d-7764246bdf7b` | 4 / 10 | 6 / 10 |

实际模型 RAG：Docx 399 字、5 引用，Bitable 747 字、4 引用，无截断且包含正确 KID；Sheet 首次回答 truncated=true，保留 **FAIL/PARTIAL**（150 字、5 引用），随后简短事实问题 285 字、5 引用、无截断 PASS。不能将简短复测替代原截断失败；首发不承诺长表完整复述及大表高级能力。

## 保留失败与测试边界

1. 普通员工向另一导入者的原 KB 提交相同资源，实际 400 `feishu_operation_failed`，原断言期望具体重复错误，**分类失败仍未解决**。请求被拒绝，原三篇数据不变；不能推断其原因一定是重复或写权限，不将此项改为 PASS。需独立核实具体服务错误及跨导入者提示。
2. 首次旧文档检索脚本同时传 KB 和 Knowledge IDs，却断言结果只能来自单篇。源码 `buildSearchTargets` 明确两者为并集、整 KB 覆盖单文档目标，同 KB 其他命中属该请求范围。原失败脚本与响应保留；改用**仅 knowledge_ids** 请求后，原严格单 KID 非空断言全部通过，没有修改产品代码或降低断言。并集请求中未出现其他 KB。
3. 导入首次观察 index receipt 已 verified、parse 仍 finalizing，严格完成断言失败；后续继续等待 parse=completed 才计通过，不以 receipt 代替完整解析。
4. 本轮没有新创建员工身份来伪造 JIT；该账号首次真实 JIT 早于 RC3，RC3 新身份 JIT 记 NOT_EXECUTED。角色矩阵、多用户并发、真实撤权重试等未覆盖项不因管理员/单员工测试自动通过。
5. user_oauth 未启用、未真实 A/B 或飞书平台 HTTP OAuth 回调验收；application_grant 不需要用户飞书 OAuth 回调，不能互相替代。应用可读不证明提交员工在飞书可读；采用已获业务确认的“应用授权资源可由有 KB 写权限员工凭链接导入”策略。
6. >500 行/记录、复杂字段和长表高级能力继续 DEFERRED。Python MCP 专项真实调用按用户决定 WAIVED；保留现有镜像/接口，不视为 Go MCP 已验收或已迁移。

## 数据保护

最终只读对账：4 users、13 Knowledge、21,127 chunks、4,908 embeddings、352 Wiki、2 external identities、3033 clean。新增三篇为本轮正常产品导入；部署前原三篇 Knowledge/Chunk/Embedding 的 9 组状态与摘要全部一致，Wiki 总量保持 352。不得将新导入造成的正常计数增加误写为原库被替换。原始三篇和新增验收数据均保留。

证据位于 evidence：`rc3-permission-summary.json`、`rc3-additional-permissions.json`、`rc3-import-search-summary.json`、`rc3-rag-summary.json`、`rc3-rag-sheet-followup-summary.json`、`rc3-old-knowledge-summary.json`、两份 logout summary、`final-data-preservation.json`。原始浏览器/API/模型内容仅在受控 Git 外目录；专用会话已退出并关闭。
