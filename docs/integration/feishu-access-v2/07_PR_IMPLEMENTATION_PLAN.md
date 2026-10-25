# PR 拆分与本轮交付

分支：feature/knowhub-feishu-access-v2；没有推送、创建远端 PR、合并 main 或部署生产。

| PR | 内容 | 前置和回滚 |
| --- | --- | --- |
| A | 当前审计、官方权限矩阵、只读探针、架构/数据库/batch/权限设计 | 本轮已形成产物；应用 Token/根目录/Wiki 首批真实验证 PASS，员工及文档内容验证、生产运行版本复核 BLOCKED；删除新增开发资产即可回滚 |
| B | versioned connection 引用、双独立开关、完整 runtime/worker/前端及 additive migration | 本轮仅实现身份契约及策略测试；其余未接入。真实 OAuth + 私有/共享发布批准边界明确后实施；默认关闭、旧配置保持原行为 |
| C | 本人 Drive 文件夹树、Wiki 单页、资源预检 | 真实 scope/ACL/分页验证；关新 browse 入口，保留旧凭据 |
| D | 多链接、durable batch/item、幂等、限流/重试及 UI | 复用既有 queue/outbox/prepare，Linux PG/Redis/Asynq gate；关 admission，保留任务审计和已导入知识 |
| E | Wiki/Drive/Links 统一引用/目录与部分失败 | 不直接合并上游 open PR，不重建 cursor/index；按源关闭新类型，旧源不变 |
| F | 授权协作、重新预检与受控延期恢复 | 不自动改权限或发消息；关闭工作流入口，不改 source 保护 |
| G | 解析与结构化保真 | **等待 #50 最终合并和安全发布结果并重新审查交叉影响**；独立 parser config 回滚，保留旧 candidate/index |
| H | 综合验收、灰度、发布 | 所有真实门禁通过后单独审查，发布和回滚按原发布体系 |

每个 PR 独立 review、CI、回滚证据；后续运行时修改前再次获取 main 和 #50 实际状态。当前 PR #50 的五个文件不在本轮修改范围。不得因为身份类型测试通过，就宣布 PR-B、批量导入或飞书真实用户验收完成。

本轮实现清单：

- 新增 internal/types/feishu_access.go / _test.go：凭据引用版本、用户归属、应用身份禁止混入用户 grant、默认关闭、显式模式选择、不做身份 fallback。
- 新增 poc/readonly_probe.py 和 application_credentials_probe.py：固定域名和操作白名单、只读、响应/频率/数量限制、真实 user_info 绑定、逐资源错误码/结构验证、应用凭据验证和脱敏报告。
- 新增本目录8份设计文档、远端基线证据、离线探针测试与预检结果。

未实现：FeishuAppConfig 运行时改造、FeishuConnection schema 迁移、管理员/个人 UI、新资源和批次 API、增量/通知/权限审批。不触碰这些代码可使当前审计/PoC 产物独立 review；但也意味着目前没有可启用的 V2 产品功能。

下一批优先顺序：补真实应用和 A/B/C 测试证据；确认私人 KB/共享批准契约与企业身份映射；在隔离 Linux 栈实现并验证 PR-B 的 SaveApp/Authorize/Complete/UserToken/Disconnect/prepare/authorizeLocal/mutate/publication 全链；再做个人浏览和首次批次，不抢先做解析或关联追链。

仍缺条件：应用其余 scopes/正式回调核对、A/B/C 独立凭据和资源权限真值、HTTPS callback、企业身份绑定合同、私人可见性/共享批准实现，以及专用 PostgreSQL/ParadeDB/Redis/Asynq Linux 测试栈。用户已提供应用凭据且最小应用 API 验证通过，不能将其扩大为文档或用户权限验收。未知条件标 BLOCKED，不用生产资源填补。
