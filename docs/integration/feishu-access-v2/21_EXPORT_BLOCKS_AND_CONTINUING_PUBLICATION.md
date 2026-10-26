# 导出优先、Blocks 兜底与持续共享策略的真实验收

2026-10-26，独立分支 `feature/knowhub-feishu-access-v2`。本篇续接报告 20；**16:15 无变化同步通过，不等于真实改值更新通过**。所有时间为北京时间，原始 JSON 回执使用 UTC。没有合并 main、部署生产或修改 PR #50。

## 用户目标及最终交互

用户要求普通员工完成一次授权后，选已有知识库、粘贴链接、一键提交；后续更新由后台处理，不逐版本登记共享批准。本轮据此新增独立的来源持续共享策略，不能把“一键导入”或 KB 写权限解释为共享批准。

策略仅可由管理员或明确获授权的隔离运维入口登记，默认不存在。它绑定已有来源、个人账号和连接、授权代际、应用配置版本、资源类型/身份、目标 KB、来源配置和共享范围摘要，记录授权依据、操作者与期限。当前隔离登记入口只接受 `test001` 原有两个来源，期限 24 小时；源码通用上限 90 天。

后台仍逐次核验源端访问、目标权限、实际版本、staging、索引回执和原子发布。策略不会清空 Cursor、创建知识库、切换应用身份、扩展文档选择，也不会恢复 paused/hold/revoked。撤销原共享批准、更新个人授权代际、改变来源配置或共享范围、策略撤销/到期均拒绝。它不为新的批量导入自动补发版本批准。

## 两篇表格的真实改值结果

用户已确认修改两篇测试文档。16:30 自然调度均发现 1 个变更对象，真实导出、下载与表格核查已执行；安全入库的第一处阻塞是 `feishu_sharing_approval_required`。用户后来说明点击的是“一键导入”，该操作没有登记新版本批准，与后台只存在 10:03 原版两条批准一致。

| 来源 | Sync log 指纹 | 16:30 任务最终状态 | 耗时 | 新版发布 |
| --- | --- | --- | --- | --- |
| Sheet | 19d30539cb0d | failed，6 个执行代际 | 676.976 秒 | 拒绝 |
| Bitable | 29842fb91de8 | failed，6 个执行代际 | 648.710 秒 | 拒绝 |

[真实改值执行快照](evidence/recovery-edited-natural-20261026.json)记录执行中状态；[后续终态与权限快照](evidence/changed-version-authority-20261026.json)记录最终失败与后续 17:00 两个取消任务。原程序把共享拒绝包装成通用入库接收失败，错误地重复了导出；本轮修复为明确 `sharing_approval_required`、短中文提示、终态失败及 `SkipRetry`，保持已接受 checkpoint。数据库及真实队列重试用例分别验证这一语义；临时基础设施失败仍走既有有限重试。

原 Knowledge ID、Chunk、向量集合与 Cursor 保留，没有将尚未批准的新值发布到 Portal/MCP。本轮不能宣布新值检索成功。

持续策略登记尝试因 Bitable 已处于 `hold` 被安全拒绝，事务整体回滚，未留下半个策略。[拒绝回执](evidence/continuing-policy-enrollment-refused-20261026.json)。没有直接写 SQL 伪造批准，没有解除 hold。

## 新增只读 Docx 与 Blocks 兜底

上游 PR [#2334](https://github.com/Tencent/WeKnora/pull/2334)已于 2026-07-27 合并，提供官方 Blocks API → Markdown；核查的上游 main 为 `97d7f0ad7b2b9fbd8bd717d737b9e79be6ace4c9`。上游默认 Blocks，保留导出；本分支按用户要求实现**个人 Docx 优先导出，失败后核验源端权限，再考虑 Blocks**。未用上游未合并的 #3312 替换 URL/SSRF 校验。

使用同一个个人 OAuth，自动合法刷新；没有申请编辑权限或切换应用身份。仅创建导出任务的已分类失败可进入权限核验；超时、限流、认证/撤权、未知故障等保留原重试/停止语义。飞书当前用户的 `view`、`export`、`copy` 必须明确返回 `auth_result=true`；失败、缺字段或拒绝均不读 Blocks。读取后再次核验权限和资源版本。

新个人 Blocks 读取不会把 50,000 块限制、循环/不完整分页、缺失表格子节点等当作完整正文。当前转换器无法完整保存的图片、附件、嵌入表格或未知富文本明确拒绝，不沿用旧连接器的截断/占位成功，不绕过图片资产发布规则。成功导出路径不变。

17:31 的[真实新流程回执](evidence/export-blocks-real-probe-20261026.json)：

- 同账号对照 Docx 导出成功：创建与轮询 HTTP 200/code 0，下载 HTTP 206，4.485 秒。
- 用户新增 Docx：Wiki/元数据 HTTP 200/code 0；创建导出任务 HTTP 403/code 1069902；接着 `check_personal_view_permission` HTTP 400/code 99991679。耗时 1.668 秒，没有调用 Blocks，没有创建 Knowledge。内部分类为 `source_policy_verification_required`。
- 账号 access 曾过期，合法刷新成功，refresh 仍有效；故障不能归因于必须编辑或必须重新连接。
- 当前应用清单中 `docs:permission.member:auth` 返回 `grant_status=1, scope_type=tenant`；用户原 grant 含 `docx:document:readonly`、`docs:document:export` 等，但没有该权限。用户明确说明应用已开通、不需重新发布，17:50 复查也确认权限存在，不能把用户令牌缺 Scope 直接等同于应用未开通。下一步先核验个人授权请求及实际 grant，不再先要求重新发布。部署端 `FEISHU_PERSONAL_PERMISSION_AUTH_SCOPE_ENABLED=true` 可让下一次正常用户授权请求该 Scope；默认关闭以免未经验证的 Scope 影响既有连接入口。是否只需补一次用户授权仍需真实回调证明。

官方[判断用户权限](https://open.feishu.cn/document/server-docs/docs/permission/permission-member/auth)支持用户身份并分别核验 view/export/copy 等动作；[创建导出任务](https://open.feishu.cn/document/server-docs/docs/drive-v1/export_task/create)接受 `docs:document:export` 或 `drive:export:readonly`。不能仅凭错误 1069902 或元数据可读断言文档所有者禁止导出；也不能以 Blocks 能返回数据推断源端允许复制发布。

## 第二次停止事件：保持停止，未沿用旧批准解锁

新事件在 **17:01:42.216** 写入 `automatic_membership_probe_context_deadline`，两个控制器随后写停止审计，队列暂停、租约执行被 fence。17:00 的两个同步终态为 canceled。与 11:31 的消费者启动顺序问题不同，本次是 membership revision Redis GET 在 500ms 控制步上下文中超时；尚没有证据证明消费者丢失。

[第二次停止审计](evidence/second-automatic-stop-audit-20261026.json)记录原 trip、进程、incarnation、租约及审计时间。控制器的失败关闭仍有效，没有删除新锁、放宽超时、关限流、切 namespace 或自动恢复队列。本次旧恢复批准限定第一次误触发，不被当作任意后续事件的解锁授权。

追加只读 Redis/主机快照记录累计 AOF delayed fsync 为 36、当前后台写状态正常、无已保留的 slowlog/latency 事件；这些是**后验快照，不能证明 17:01 的磁盘或 CPU 根因**。暂不将该事件称为已修复。Bitable hold 也保留，后续须按原权限恢复机制独立核查。

## 测试、部署与仍需完成的验收

本地个人连接器回归：[324 pass / 2 skip / 0 fail](evidence/export-blocks-local-tests-20261026.json)，含 11 类导出优先/Blocks 授权、撤权、版本变化、分页及正文完整性用例。Windows 完整 service 回归因本机 MinGW 缺少 DuckDB 的 `rstrtmgr` 链接库未执行成功，以 Linux PG/native 门禁为准，不把编译失败计作测试通过。

最终候选产品源码 `56fc87ee576098cd326ce4331189edbf0c0e2f63` 额外要求 copy 权限，对应 12 类兜底用例已本地通过。已推送 V2 分支并启动该 SHA 的五组门禁；五组全部完成前不能宣布回归通过。早期候选取消或失败的 CI 不计作最终证据。测试站当时仍为原已部署 `2a7f651e6ed078fcf5113905a9358a82084d7d17`；本篇须在门禁结束与实际升级后补记，不能预先宣称已部署。

剩余业务证据：用户 Scope 发布后的真实 Docx 权限/Blocks/发布/检索；第二次停止的完整根因与单独受控恢复；Bitable hold 核查；两来源持续策略实际生效后的改值发布、Knowledge ID 不变及 Portal/Go/Python MCP 新值对账。以上未完成前不进入最终发布。
