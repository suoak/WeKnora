# P0 测试站修复与重新验收

隔离站：http://172.30.232.42/ 。已部署前后端提交：

`111b87e3db87fe359bb43cd1155a6d668dac5d0f`

没有合并 main，没有部署生产。PR #50 的五个受保护文件相对 origin/main 没有变化。

## 真实根因及修复

原始任务已被领取，连接和 KB 权限检查、Feishu 元数据查询及导出下载已经成功。第一处阻塞是共享 staging 卷根目录由 root 持有（0755），而实际 APP/Worker 进程为 1000:1000，mkdir 被拒。归档 Asynq 任务保存了该错误，attempt 反复 recovery，却没有总重试上限；前端只展示“导入中”。

已仅修正该隔离卷根目录为 1000:1000、0700，无递归修改、无任务/租约重置。原始任务自动恢复。代码同时增加启动时实际写入探测、错误分类、临时失败最多 3 次自动重试、终态 attention、真实解析阶段/索引回执、独立的推进时间、保守停滞检测及日志警示。

另一个真实检索问题是个人 MCP 错误拒绝了 KB 明确绑定的系统内置 Embedding 模型。修复继续使用原模型服务的可见性检查，允许精确绑定的 active builtin；不同模型 ID、其他租户普通模型和错误维度仍拒绝。

## 原始批次结果

下列 ID 均为 SHA-256 前 12 位，非原始 ID。

| Batch | Item | Knowledge | 结果 | 接收到成功耗时 |
| --- | --- | --- | --- | --- |
| 89e9b3e0c05c | 3910d0e328c5 | 3663d886f314 | succeeded | 约 27.9 分钟 |
| 002abcce1bcd | 05484e5c0022 | 070925df4104 | succeeded | 约 161.8 分钟 |
| 4d5f533801f3 | c37d3d796379 | 070925df4104 | succeeded，同库复用 | 约 167.6 分钟 |

实际只有一个不同的 Docx 资源，分别入库到两个 KB，不能将两个 Knowledge 计为两篇不同文档。两个记录均为 parse completed / enabled / summary completed，各 6 Chunk、6 条 1024 维向量，BM25 各命中 5 条，personal_index_receipt verified，发布 outbox processed。原先预留的 Knowledge ID 保持不变。

两个目标 KB 均未启用 Wiki 生成，本轮 Wiki 收尾为 disabled，不将其表述为已生成 Wiki 页面。Wiki 资源链接已真实解析为 Docx；链接类型与知识库 Wiki 生成功能是两个独立验收项。

升级后，test 现有个人凭据的 Go MCP keyword、semantic、hybrid 检索全部命中这两个记录；真实 Python MCP hybrid_search 工具也全部命中。没有以合成内容替换真实文档。真实 Portal 网页检索尚需 test 网页会话验收。

## 新流程

知识库列表入口必须明确选择已有可写库；具体 KB 页面默认并固定当前目标。缺少 kb_id 返回 HTTP 400，不调用 EnsurePrivateKnowledgeBase，也不切换目标。OAuth 返回保留链接及目标草稿；提交仍重新校验真实写权限和资源版本发布批准。

历史页自动刷新，重新打开可读取已持久化任务；显示排队、检查准备、真实下游阶段、推进时间、错误原因、索引数量与检索入口。只有验证过索引和发布的条目显示完成，去重不等于成功。已完成记录隐藏旧重试时间。页面关闭不影响 Worker。

## 回归及部署证据

五组精确提交检查均 success：

- V2 / 前端 / PostgreSQL / race：[40210899377](https://github.com/suoak/WeKnora/actions/runs/40210899377)
- 原生解析与全应用：[40210900188](https://github.com/suoak/WeKnora/actions/runs/40210900188)
- 综合安全及完整后端：[40210903229](https://github.com/suoak/WeKnora/actions/runs/40210903229)
- Portal / 权限 / MCP：[40210906836](https://github.com/suoak/WeKnora/actions/runs/40210906836)
- Python MCP 四版本及构建：[40210909759](https://github.com/suoak/WeKnora/actions/runs/40210909759)，未发布。

前端 1591 pass、1 skip、0 fail；V2 PostgreSQL 53 pass、0 skip；PostgreSQL race 28 pass、0 skip；原生 V2 61 pass、0 skip。真实浏览器覆盖明确选择目标、没有目标禁止提交、10 条混合链接一次提交、无 preview、390/1280 宽度、自动阶段更新、失败提示及隐藏历史重试时间。浏览器 HTTP 为模拟，不能代替真实 Sheet/Bitable 入库。

原隔离项目就地升级，独立数据库恢复比对通过，备份 SHA-256：

`fa3f03e8c232143699cb4de75c9fd6e9698916bbf11708865a77a21e7b62a7c2`

APP / Worker2 均 healthy，六个 Worker pool 各有两实例。现有 test 用户和连接仍存在，JWT/AES 等秘密没有轮换，Cursor 没有清空。合成同租户管理员对私人 KB、文档、检索均被拒；这是应用隔离回归，不是真实跨员工飞书 ACL 验收。

部署后还使用真实网页登录会话验证新接口：可写目标 API 只向私人库所有者返回该库；8 次并发缺少 kb_id 的请求均为 HTTP 400，所用隔离账号私人 KB 数量增量为 0。该接口回归没有创建真实飞书业务任务。

## 尚待用户网页提交的真实验收

用户指定已有目标 test001，并已明确允许两篇新增 Sheet/Bitable 按其现有范围共享。它们的 Wiki 元数据读取均 HTTP 200 / Feishu code 0。正文导出、解析、索引、发布及检索尚未宣称通过。

用户已选择升级后自行网页提交。飞书导入与审批接口明确仅接受网页登录；没有使用 MCP 凭据绕过该授权。请 test 在 test001 粘贴两条链接并一键提交；若缺少版本批准，从最近任务的高级设置登记已批准的资源版本并恢复原批次。后台继续逐项核查真实阶段与结果，不能以元数据成功替代正式入库。

自动创建的私人 KB 仍含有效知识、任务、Chunk、向量和文件引用，本轮保留；不删除、不移动、不重新生成 Knowledge ID。任何清理需另行核对全部索引、文件、outbox、来源与审计依赖并获得批准。

HTTP 隔离测试风险提示保留，生产 HTTPS 策略保持不变。

脱敏逐项证据见 [p0-real-batches](evidence/p0-real-batches/README.md)，诊断过程见 [11_REAL_BATCH_P0_DIAGNOSIS.md](11_REAL_BATCH_P0_DIAGNOSIS.md)。
