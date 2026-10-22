> ?????????????? APP ?? UNSAFE ?????? [??????](FEISHU_INCREMENTAL_STAGE_C_FINAL_DELIVERY.md)?

# 飞书安全增量与 Go MCP V1 当前联合验收

状态：IN_PROGRESS。本文记录当前候选，不能解释为生产恢复同步或发布授权。

## 候选与已验证范围

- RC3 HEAD：`e62b17e2d305358467821aeff1cce63ce1d61254`，所有新增源码尚未提交。
- 联合冻结源码：`b47025118adfc4cff2d1f75cabc6f98265a0bb9ca1ad816f409099081947f0fc`。
- 联合业务源码：`1d533edfe33957dc6b74960594ff7d12ab33e4cfe670f631d10074fc9e9bad1a`。
- 独立飞书补丁源码：`02a8c5eb187764a93381bc5d794d69a86fd046145fce7ab29c18f686b2434c2c`。
- 独立补丁业务源码：`2ac380a033af80f022d98a3b45cd5e30645b148d07cde8c76c46cce94787392b`。
- 冻结后的测试脚本修正使用单独的 SHA256 命名工件，不覆盖原源码包，不改变 APP 镜像。
- 独立实例：`knowhub-v1-feishu-b47025118adf`，打包服务器回环地址 `http://127.0.0.1:59079`，需要 SSH 转发；不代表已开通企业网络。

| 门禁 | 当前状态 | 证据及边界 |
|---|---|---|
| 独立补丁与联合源码开发质量 | PASS | 各 21 条命令退出 0，含完整 Go、race、vet、构建、迁移；SKIP 保留，不等于验证通过 |
| 真实 ParadeDB 候选过滤回归 | PASS | `canonical-probe2`；合法标题/正文格式、SourceID、旧内容、向量与 BM25 过滤 |
| APP/前端构建、默认关闭启动、单独启用 | PASS | `evidence/feishu-joint/build-b47025118adfc4cf`、`deploy-b47025118adfc4cf` |
| 联合基础业务、迁移、Python SDK、备份恢复 | PASS（合成范围） | `acceptance-b47025118adfc4cf`；实际 PG 17.9 / pg_search 0.22.2 / vector 0.8.1；模型服务为合成 1024 维，不是正式模型 |
| 飞书真实 APP/Worker 全生命周期 | IN_PROGRESS | 初次 Wiki 导入、稳定 KID、更新和实际 MCP 检索已有证据；完整 Wiki/Drive、拒绝、恢复尚未收口 |
| 当前候选浏览器与 Codex 五工具 | TODO | 不继承旧候选 PASS；将在本实例有效来源阶段执行 |
| Worker/Redis 故障及恢复 | TODO | 待来源生命周期成功后执行 |
| 独立 RC3 飞书镜像运行验证 | TODO | 已构建，不等于已通过独立 APP/Worker 验收 |
| 正式 SID、正式模型、真实飞书测试应用 | BLOCKED | 未配置独立授权条件；合成测试不能代替 |
| 生产恢复自动增量 | BLOCKED | 未获授权，安全门禁尚未全部通过，生产停更不解除 |

## 保留的失败及正在处理的问题

1. 原候选与当前候选首次 HTTP 冷启动失败、旧的检索候选泄漏/检索空结果，均保留原始日志；当前修复通过后的证据不覆盖历史失败。
2. 当前 `feishu-app` 在故障注入后等待 180 秒超时。夹具块接口返回 500，但导出回退接口缺失返回 404；实际队列又保留五次重试，不能把尚未终止的任务当作成功。夹具补齐一致故障，官方 Asynq Inspector 仅提前执行指定合成任务的 Retry，不减少次数、不修改 payload、不绕过身份校验；自然退避时间 NOT_TESTED。
3. `feishu-app-a2` 实际 HTTP 403 被脚本当 JSON 解析，形成 JSONDecodeError。已核对原始 APP 日志：请求遭前次合成来源 HOLD 保护拒绝，并非 200 内容响应。脚本现在保留真实状态及纯文本拒绝。
4. `feishu-app-a3` 恢复夹具版本早于已发布版本，触发 ownership fence 并超时。下一次测试使用更新版本，并经真实 Owner API 与 Worker 恢复；不直接修改数据库解除 HOLD。
5. 当前统一 MCP 的 Resolve 遇到一个 HOLD 来源可能拒绝整个请求，包含无关 KB。这是已发现的可用性限制，不能描述为“仅拒绝该来源而其他来源完全正常”。安全上保持拒绝默认，是否需在发布前收窄其影响仍需结合联合验收评估。

## 发布边界

生产不变，不解除停更，不 commit/push/merge/release。独立热修复还需自身镜像运行、兼容及恢复验证；联合候选尚未完成整体验收。旧 APP 对 schema 3036 的回退不能预先宣布安全，不以 down 3036/3035/3034 为默认回滚。原 V1 冻结包及其运行实例保留。

“我的全部知识”仍受现有个人 Key 私人空间 scope 约束，未来加入空间不会静默扩权；本轮不新增动态私人授权模式。WorkMate 会话 tools/call 的历史 FAIL 暂缓，不作为本轮修复对象。
