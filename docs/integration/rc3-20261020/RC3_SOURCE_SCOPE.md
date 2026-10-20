# 最终收口索引（2026-10-20）

最终运行源码 `1e2ded5b67ce9487966210d31011cf9c5b8cdfb9`；正式 CI 9 SUCCESS / 0 FAIL / 3 SKIP，普通员工 OA、三格式索引/检索、幂等、跨导入者明确错误、越权拒绝和退出已真实复验。完整结果、来源 SHA、限制及回滚证据见 [RC3_FINAL_CI_FIXES.md](RC3_FINAL_CI_FIXES.md)。下文保留此前版本的历史快照，不应将其旧 FAIL/PENDING 状态或旧镜像误当作最终结果。

生产仍 NO-GO；尚未合并 main。新的审核报告提交只改变文档，当前 PR 检查须另行核对。Bitable 复杂汇总截断、>500 样本、首次 JIT/首次邮箱关联和未签发 Refresh 的真实验收没有因此变为 PASS。

---

# RC3 源码收口范围

联合分支：`release/knowhub-oa-feishu-rc3-20261020`。
冻结父版本：`5f988be51779f1a22bd9d027b7503fe6a8749f62`。
本次只读拉取核实 main：`854f3b956ae3600e12a3ffd2fc40d4fe624ced74`，是冻结父版本的祖先。

候选继承已部署 OA 方案 B continuous_test + 飞书 v7 / RC1，再继承 RC2 克隆迁移与诊断测试。未将 P1-3/B1/E0 实验或 3032 纳入候选；未整体合并上游。

`evidence/source-manifest.json` 固定 5685 个源码文件，SHA256：`bbdf9b880ec27277bb0c2b7ea385d9d23fd1e28e7bc2bc1d29e7d9e0958c391d`。文件清单含 RC2 继承库存，新增 RC3 Go/TS；报告、构建产物及凭据不属于源码清单。清单内 `changed_from_rc2` 明确 18 个文件。

RC3 相对 RC2 的产品改动只有旧来源停更屏障（service、scheduler、新 datasource 策略文件）和邮箱前缀展示（UserMenu、UserProfile、工具函数）。其余为真实 PG/Asynq、调度/配置测试及 CI 编译卫生处理：

- 重启测试通过原构造器重建 service，避免复制含 sync.Map 的实例，不改原恢复断言。
- 两个同目录独立诊断 main 文件加 ignore build tag；显式 go run 仍已验证，不混入普通包编译。
- 能力键测试解析先去 CRLF 空白再去逗号；新增 LF/CRLF 相等及真实错误键检查，不修改后端能力或前端能力业务逻辑，不删除失败断言。

`application_grant` 是管理员统一应用及源作者授予应用文档访问的路径，不是提交链接员工的个人飞书身份。KB 写权限仍需单独校验，入库内容遵守 KnowHub KB ACL。业务已接受凭链接导入应用可读资源的发布策略。

`user_oauth` 有历史实现，但真实 A/B、HTTP 平台回调接受等验收不齐，首发关闭，不能用 application_grant 的通过结果替代。个人变化版本仍安全拒绝，不使用先删后建 Replace。

本轮源代码扫描未发现新增私钥或 JWT；凭据样式命中已人工复核为样例/配置引用/错误码，不发布真实受控配置。实际数据库备份、文件、Token、Cookie、SSH 凭据及原始请求仅留在受限 Git 外路径。

最终代码提交为 `5156e4b47643535d6ff473c9890be814bd6f7ffe`，Draft PR [#28](https://github.com/suoak/WeKnora/pull/28)。实际镜像以该提交的 `git archive` 构建，不直接复制未核对的工作树。

最终 Git 源码 Manifest 为 `evidence/committed-source-manifest.json`，5685 文件，SHA256 `5be1a6ed2a3778e7caf34ed161755c0b71e2c7464c918d77175b05cc94a7f8b7`。上面的 bbdf 清单保留为**提交前工作树字节**证据：两份邮箱展示 TS 文件只有 CRLF/LF 差别；所有 Go 文件字节一致。Manifest、源码提交和镜像身份分别记录于 `RC3_CHANGE_MANIFEST.md`，不能混用。

后续本目录验收报告/脱敏证据提交不改变运行源码。测试 APP 已部署，但完整 CI 尚未通过，见 `RC3_RELEASE_GATE.md`；不据此合并 main 或发布生产。
