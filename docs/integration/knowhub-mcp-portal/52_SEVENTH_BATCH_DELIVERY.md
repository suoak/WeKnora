> **Superseded policy:** First-approval and real-time employment requirements in this historical report have been replaced. See [55: final dynamic Portal policy](55_DYNAMIC_PORTAL_PUBLIC_REQUIREMENTS.md). Historical test passes do not validate the new candidate.

# 第七批交付：Portal 企业知识业务验收准备

**本批交付是默认关闭的候选源码、真实客户端的已验证部分和可审阅的首次确认清单。企业公共知识正式 MVP 继续 HOLD，混合敏感 KB 完整 M3 继续 HOLD。没有生产开放、部署、迁移或 Git 提交。**

## 交付入口

- [46 业务规则、正式身份 Provider 与 NEEDS_DECISION](46_SEVENTH_BATCH_BUSINESS_AND_IDENTITY.md)
- [47 首次确认、CAS/审计及普通来源继承](47_FIRST_CONFIRMATION_AND_INHERITANCE.md)
- [48 大 KB SQL、真实驱动、1024 维及模型装配](48_LARGE_KB_SQL_AND_MODEL_VERIFICATION.md)
- [49 三个真实客户端的实际覆盖](49_REAL_CLIENT_ACCEPTANCE.md)
- [50 生产 17 空间/48 KB 脱敏待审清单](50_SEVENTH_BATCH_REVIEW_INVENTORY.md)
- [51 完整命令、候选指纹、测试与未过门禁](51_SEVENTH_BATCH_TEST_RESULTS.md)

第一至第六批 00～45 报告及其原始证据保持原样。开始前保存 6343 个文件完整快照及逐文件 SHA256。独立工作树仍为 `audit/knowhub-mcp-b4-rc3-20261021`，Git 基线 `e62b17e2d305358467821aeff1cce63ce1d61254`；未提交源码以本批 SHA256 候选及 [交付校验](evidence/batch7/delivery-validation.json) 标识，不能用原 HEAD 代替。

## 完成状态

| 分类 | 本批结果 |
| --- | --- |
| 已完成设计 | Portal 发布与访问分离；历史首次确认；责任人/审核人分离；可信新增普通来源继承；整库公共 SQL 过滤及撤回；灰度/回滚准备 |
| 已实现源码 | SID 数据与准入契约 verifier；原策略原子确认包；受控新增普通文档继承；批量授权版本快照；实际 PostgreSQL 公共关系过滤与精确向量查询；原 model/store registry 装配；Portal 清单 UI；个人 Key 明确企业能力、统一配置及连接检查 |
| 已验证功能 | 见 51 最终候选实际 exit code；Codex 五工具、公私联合与拒绝；WorkMate真实 initialize/tools/list；合成首次确认、inheritance、1024 维大 KB 及受影响回归 |
| 需要人工/业务决定 | 正式在职及机器账号判定契约；内容负责人/企业公开审核职责映射；48 KB 逐项首次确认；新增来源可信分类与权限变更契约；独立生产变更批准 |
| 未完成/尚未测试 | 正式身份端到端；正常上传/同步自动继承装配；最近审核记录完整 UI；WorkMate tools/call及完整UI导入；Claude登录后联调；真实跨语言模型质量；生产多 Chunk/多模型及全部客户端规模负载 |
| 独立保留 FAIL | Windows 55 项中 symlink WinError 1314；所有历史和本批中间失败，不能以最终聚焦 PASS 擦除 |

个人 Go MCP 保留五个只读工具；没有 ask、上传、修改、删除、Wiki 公共工具或文件下载。旧 Key 不静默获得企业能力；不向员工批量授予目标空间成员资格。公共范围由每次请求和关键模型/响应边界的最新授权决定，个人私有权限独立校验。新入口对未知/混合及超过私有 allow-set 约束的 KB 仍可能 HOLD，不能声称本人所有历史私有知识都已覆盖。

## 两个门禁

| 门禁 | 结论 | 原因 |
| --- | --- | --- |
| 企业公共知识 MVP 正式业务验收 | **HOLD** | 正式在职适配器、角色映射、历史首批业务批准、真实模型质量/生产规模、WorkMate完整调用和Claude联调未齐备 |
| 混合敏感 KB 完整 M3 | **HOLD** | 文档差异化权限、Wiki/FAQ/Graph 来源、文件 Broker/旧匿名链接/CDN、外部源细粒度 ACL、历史引用未闭环 |

生产仍是 17 个 Published active 空间、48 KB、2718 Knowledge；**首次可核验公开确认 0，当前可进入生产公共 MCP 0**。12 KB 有外部/未知渠道线索、4 个大 KB、5 个空 KB；这些类别重叠，未批准不等于已确认敏感。清单没有替任何业务负责人作出批准。

## 下一阶段顺序

1. 明确并接入正式 OA/SID 准入契约及企业内容审核职责；拒绝默认保持，不把 SystemAdmin/Owner 当成隐含企业公开审核人。
2. 在原 Portal 管理流程展示审核责任人、时间、版本、原因和变更记录；完成批准角色下的首批少量普通 KB 确认 UI 闭环。依据清单审批，不自动回填 48 KB。
3. 把新增普通来源 verifier 接到可信写入完成事件，处理并发批量导入及撤回；未知/外部源继续 HOLD。复用现有策略、epochs 和事务审计。
4. 补齐 WorkMate 工具执行与 Claude 已登录受控客户端；覆盖中文问英文、同名 KB、部分失败、大 KB、重连/轮换及更多并发场景。
5. 用批准范围等价的多 Chunk 数据及实际模型完成延迟/质量/并发 SLO，再准备独立审批的灰度变更单。

## 部署和回滚准备

当前生产装配只注册 disabled personal server。发布前须独立核验模型/存储注册、源权限、审批映射、3033→3035 正式迁移、监控、客户端与业务签字。此清单是准备材料，**不授权部署或迁移**。

建议监控：身份/策略/epoch 校验失败、401/403、空范围、partial/failed_kbs、解析及查询 P50/P95、模型边界拒绝、跨租户异常、取消/超时、错误率与容量。不得记录 Secret、正文、完整模型请求或未经批准的来源内容。本批已有测试监测和容量日志，生产告警及 dashboard 尚未装配，不虚报为已部署监控。

可回滚范围：保持入口 disabled；若未来批准灰度后需要收回，先关闭该入口并撤回企业策略，不更改个人原有成员/Key，不切换生产 Python；停止本批新 UI/API 装配即可退回第六批 256 文档试点。保留审计、索引、数据库和旧链接证据，不以删库/删备份作为回滚。源码逆向变更须按本批逐文件差异处理，不能 reset/clean 当前工作树。

结束位置：commit、push、merge、release、生产部署/迁移之前。没有生产 Token 轮换、历史公开或旧 Python 删除。服务器历史遗留文件未清理，容量使用新命名隔离卷和门禁控制。
