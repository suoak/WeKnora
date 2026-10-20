# RC3 验证汇总

代码候选 `5156e4b47643535d6ff473c9890be814bd6f7ffe`；实际镜像及清单见 `RC3_CHANGE_MANIFEST.md`。测试环境保留该候选；生产未部署、未迁移、未暂停来源。

## 测试结果（分别计数，不跨运行累加）

| 执行范围 | PASS | FAIL | SKIP / 其他 | 结论 |
|---|---:|---:|---|---|
| Linux RC3 初始定向回归 | 215 | 0 | 0 | PASS |
| OA/Feishu/派发恢复/权限/Core/Wiki/Drive/Repository 等定向 Race | 850 | 0 | 35 SKIP | 已执行部分 PASS；不能等同完整全仓 Race |
| 实际提交归档 RC3 停更与能力键定向 Race | 25 | 0 | 0 | PASS；包含真实 PG/Asynq |
| 最终本地 Linux 全仓 Go（排除仓库现行 CI 的 docreader 范围） | 13820 | 1 | 191 SKIP | FAIL，公共域名解析 ::1，见下文 |
| 最终 go vet | — | 0 | exit 0 | PASS |
| APP 归档构建 | — | 0 | exit 0 | PASS；首次缺原生库失败已保留 |
| 邮箱前缀展示专项 | 11 | 0 | 0 | PASS |
| 最终 Frontend type-check / build | — | 0 | 均 exit 0 | PASS |
| Windows Frontend 全量 | 1571 | 5 | 1 SKIP | FAIL；独立 RC1 对应文件重现 20 PASS/5 FAIL |
| 冻结 RC1 OA 同一并发测试 ×100，Race | 95 | 5 | 0 | FAIL；不能作为 RC3 PASS |
| 生产物理备份实际恢复，18 点与生产对比 | 18 | 0 | 0 | 恢复等价性 PASS；自召回仍原生产17/18 |
| 额外隔离物理 SQL BM25/身份诊断 | 4 组身份一致、2 条 BM25 命中 | 0 条有效探针失败 | 1 NOT_AVAILABLE | harness 总 pass=false 保留，不计整体 PASS |

GitHub 实际 PR CI 见 `RC3_MAIN_CI_REVIEW.md`，**未全绿**。Linux 全量统计按 go test JSON 的 Test pass/fail/skip 事件计数（含子测试），包级失败不再重复加到用例数。所有 SKIP 单列，不视为 PostgreSQL 或功能通过。

35 个 Race SKIP、191 个全仓 SKIP 的原 JSON 日志留在受控打包机 artifacts；摘要记录日志 SHA。另一次完整 Service Race 因耗时与资源中止，记录 NOT_COMPLETED，不用定向 850 PASS 替代。

## 最小停更保护

`FEISHU_LEGACY_SYNC_SUSPENDED=true` 仅覆盖 feishu/feishu_drive/lark/lark_drive；不覆盖 application_grant。未配置保持既有行为，非空非法值失败封闭。API Create/Update/ManualSync/Resume、scheduler 注册/更新/旧 callback、队列任务实际处理入口均受控。旧任务在 connector/知识写入前终态拒绝，保留游标/成功时间/旧结果，来源 paused、SyncLog failed，SkipRetry，重复投递不重复有效处理。DB 确认失败不虚报成功。

独立真实 PostgreSQL 与 Redis/Asynq 验证：持久旧任务进入正常 archived、没有抓取/知识写入、不形成无止尽失败重试，保留原 payload 与游标，重复 TaskID 冲突安全；只操作专用测试队列内自己的任务，没有清空共享队列。含 12 个并发手动触发拒绝及 scheduler 陈旧 callback 测试。测试前失败断言保留，未恢复 Replace。

该屏障不能约束没有新代码的旧 Worker。正式维护窗口必须停止并防止全部旧消费者重启、确认在途副作用终态后才切换；未知结果保持阻塞。生产 7 个来源和 354 条 Knowledge 尚未暂停。

## 已保留失败与环境修复

- 初始 network=none 全仓出现大量 DNS 失败，仅环境诊断，不报告产品通过。后续在受控隔离测试网络运行。
- 早期 Go 全仓失败还包括缺少真实 Python ensurepip；未 Mock。使用官方 CPython 3.11.2 源码中的 ensurepip 与内置 wheel 恢复测试容器依赖（SHA 见 `ensurepip-harness.json`），该测试最终通过。未将测试依赖写入产品源码。
- 最终唯一 Linux 全仓失败 `TestValidateConnectorBaseURLAllowsPublicHTTPS`：`open.feishu.cn` 被运行环境解析到 `::1`，触发现有 SSRF 防护。没有放行 loopback、改安全规则或删除断言。网络条件修复及重跑仍待完成。
- 能力键测试的原始基线失败已保留；修复仅为测试解析先去除 CRLF 空白后处理逗号，并新增真实不匹配拒绝测试。没有修改业务能力键，也没有改成跳过。最终专项通过。
- Frontend 的 2 个 style guard、1 个 i18n 审计失败在 RC1 可重现；另2个 Windows 缺 POSIX bash 失败属环境。没有增加计数基线或删除失败断言。GitHub Linux 仍有前三项失败，待独立最小修复/明确处置。
- GitHub OA 并发试点审核竞态也在冻结 RC1 复现（95/5）；源码一致性和原日志 SHA 见 `frozen-oa-concurrency-baseline.json`。当前保留失败，不热身绑定掩盖首绑并发窗口，不扩大认证方案。
- 普通用户真实跨导入者重复错误分类失败、Sheet truncated=true 均保留，见 `RC3_REAL_ACCEPTANCE.md`。

真实普通员工 OA、三格式最终索引/检索、8 项权限拒绝、两次退出与数据摘要保护均有独立产品接口证据。>500 行/记录 DEFERRED、RC3 新身份 JIT NOT_EXECUTED、Refresh NOT_AVAILABLE、Python MCP 真实只读 WAIVED，不能互相替代。
