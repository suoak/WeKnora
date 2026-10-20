# RC3 main 合并与 CI 审查

Draft PR [#28](https://github.com/suoak/WeKnora/pull/28)，代码 head `5156e4b47643535d6ff473c9890be814bd6f7ffe`。main 审查点 `854f3b956ae3600e12a3ffd2fc40d4fe624ced74`。RC3 相对冻结父版本 176 个提交文件（含报告/清单）；相对 main 还包含原 OA/KnowHub 历史链，不能仅将 RC3 的18文件作为整个 PR 范围。完整 PR diff 与迁移库存需按实际 head/base 审核。

## GitHub 实际执行

| Workflow | 结果 | 说明 |
|---|---|---|
| [Portal Final Gate](https://github.com/suoak/WeKnora/actions/runs/39362117336) | PASS | Portal contracts/RBAC/race/branding、PG fresh/existing upgrade、M1 Linux backend/MCP compatibility 三 job 均 success |
| [Frontend](https://github.com/suoak/WeKnora/actions/runs/39362117304) | FAIL | Docker multi-stage build success；tests 失败于两项 style guard、一项 i18n 静态审计；后续步骤不能按通过 |
| [App](https://github.com/suoak/WeKnora/actions/runs/39362117306) | FAIL | TestSIDTLSUserInfoParallelDifferentTransactions；依赖的 startup smoke SKIPPED |
| [Go Lint](https://github.com/suoak/WeKnora/actions/runs/39362117307) | job FAIL | 外层 success 来自 continue-on-error，不代表 lint 通过；现行 main 差异范围中的历史 OA 命令文件存在 lll 长行问题 |
| [Datasource Sync Execution Gate](https://github.com/suoak/WeKnora/actions/runs/39362117322) | SKIPPED | 当前分支条件不触发；使用独立实际提交 PG/Asynq 定向 Race 补充，不伪造 workflow success |

原始 job 日志保留 Git 外，受控脱敏摘要见 `evidence/github-ci-status.json`。报告补充提交可能触发新一轮 CI，以上结果严格属于代码提交5156；不能将旧结果冒充新 head 执行结果。

## OA 并发失败定位

试点首绑路径在 `sourceid_binding.go/ResolveSourceIDEmailIdentity` 先查 review，未命中后查 enrollment；另一个授权事务在两次查询之间提交 approved review+enrollment，当前事务会在 pilot scope 的 enrollment 分支保守拒绝 `sourceid pilot reviewed identity required`。已有后续分支存在重新确认 approved review 的逻辑，但该分支没有同等对账。没有证据显示它绕过审核；表现为并发首绑误拒绝。

同一测试及 repository 文件在 RC1/RC3 字节一致；实际挂载冻结 RC1 源码、Race 重复100次为95 PASS/5 FAIL。generic launcher 的 commit 字段曾沿用 RC3 metadata，**不能据它声称测试挂载的是 RC3**；实际 mount 与两文件 SHA 已另行保存纠正。

后续最小修复方向为在该明确竞态窗口重新核对同 issuer/employee/sub/user 的 approved review，并再次执行 pilot account 校验；缺失/撤销/冲突仍拒绝，Session 层保留原锁与再检查。需要可确定复现的并发与撤销负向测试后再决定代码变更。当前没有修改冻结 OA 绑定逻辑来赶绿，也没有用预绑定掩盖测试；因此 **main 合并门禁仍不满足**。

## 自动构建与部署

仓库 `docker-image.yml` 的 main / v* 推送会构建推送镜像。release 分支推送与 Draft PR 不等于授权正式发布；PR build 不推生产镜像。仓库未发现直接 SSH 生产部署，**不能证明生产不存在外部定时拉取、Watchtower 或人为 latest 更新**，此关系仍需运维确认。

本轮已按用户要求建立可追溯代码提交及 Draft PR；没有合并 main、打正式 Tag、创建 Release 或生产部署。生产继续现有 Windows 上传镜像流程。先修复/正式处置硬 CI 失败，再锁定最终源码重新构建验证；不可把软失败 workflow success 当作放行依据。
