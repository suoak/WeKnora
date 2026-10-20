# KnowHub 联合 RC2 发布阻塞收口

结论：**BLOCKED，当前不申请合并 main，不允许生产发布。**

本报告是本轮收口判断；旧阶段报告保留历史原文。采用显式方案 B 的首发不再以等待 SID JWKS 修复或 A/B 全公钥确认为前提，不能将历史阻塞错误沿用为当前条件。生产 B 策略与 HTTP 风险批准仍独立必需。

## 本轮范围和版本

- 独立分支 `release/knowhub-oa-feishu-rc2-20261020`，base `5f988be51779f1a22bd9d027b7503fe6a8749f62`；原脏树、OA 及飞书 v7/RC1 工作树未修改。
- 继承已接受 RC1 的 5,677 个文件，逐一校验 SHA；本轮仅新增一个隔离克隆测试、一个独立迁移工具及交付文档，无认证/导入运行代码、前端或历史 SQL 修改。
- RC2 完整源码（含新增测试和独立工具）Manifest：`d7626f0e6aeab1873752917ae717dc881fceffad6939f0f289b434db27a7f842`，5,679 文件。
- 测试 APP 仍为已验证 RC1 `sha256:67b78b1c5aaf72c3d8e03763bfab57bdeb47bf3cf9d771e8fba60c870246d6cb`；Frontend `sha256:e262122048b835e27c8e523cf98118aa3440120f3ba6c19cd3651460161381b0`。
- 未将原镜像重新贴为 RC2；在关键兼容门禁未通过前未构建/部署新 RC2 镜像、未提交、未推送、未建正式 PR、未合并或创建 Release。

## 关键门禁

| 门禁 | 本轮状态 | 说明 |
|---|---|---|
| 旧 Wiki/Drive 变化版本安全更新 | BLOCKED | 安全拒绝保留，缺少已验收的候选隔离/原子发布；暂停备选尚需业务批准 |
| 生产一致性快照 | PASS | 用户授权后只读备份；238 秒，6.24 GB，独立克隆 |
| 全量克隆 3023→3033 DDL/数据 | PASS | 92 个 COPY 表对账无差异；2.100 秒，重放通过；身份/ACL/KID/业务索引定义不变 |
| 故障及新旧 APP 启动 | PASS（限定范围） | 锁超时 dirty 拒绝、完整 baseline 恢复通过；新旧镜像 health200，不替代会话/任务语义回滚 |
| 逻辑备份恢复后 ANN 等价性 | BLOCKED | 串行重建后 18 点 14 PASS/4 FAIL；同点生产 17 PASS/1 FAIL，4 个退化点生产均成功；不归因于迁移 DDL |
| 独立迁移工具 | PASS | 9 PASS/0 FAIL/0 SKIP；禁止 Force/Down/自动 dirty 恢复 |
| 原恢复/游标 Race | PASS | 16 PASS / 0 FAIL / 0 SKIP |
| 生产结构拒绝后保护 | PASS（安全拒绝） | 7 PASS/0 FAIL/1 SKIP（无既有知识来源）；KID/Chunk/索引/游标保留；不能代替正常更新通过 |
| 测试站点保护 | PASS | 镜像/策略不变；六篇文档 completed/索引保留；health200、未登录 KB/管理401 |
| 最终 main 全 CI | NOT_EXECUTED | 兼容门禁未通过，不虚构最终 CI；v7 能力键历史失败证据保留、未改无关代码 |
| Python MCP 已授权只读工具 | NOT_AVAILABLE | 生产 caller API Key 模式；尚无受控调用者 Key，未用测试旧镜像覆盖生产 |
| 普通员工/JIT/跨空间真实矩阵 | NOT_EXECUTED | 缺少已授权身份；管理员验收不能替代 |
| 生产 HTTP 风险/配置/发布窗口 | APPROVAL_REQUIRED | 测试 continuous_test 不能复制生产；生产需独立批准 |

## RC1 已有证据的继承边界

RC1 已接受 Linux Service 3397 PASS/0 FAIL/6 SKIP，Core/Wiki/Drive+Repository+Database 684/0/38，OA/Repository Race 487/0/35，真实 PG/Redis 恢复子集 23/0/0（包含在 Service，不重复相加），Frontend 构建/类型检查和 29 项定向测试。
RC1 已接受两次真实 SID 登录/退出，以及 Docx、Sheet、Bitable 新导入终态、稳定 KID、Receipt、重复导入、实际搜索及旧数据保护。此处为**既有证据，未冒充 RC2 新一轮浏览器验收**。
Sheet 默认较长回答曾截断，短问句完成不等于默认长回答问题全部解决。大于 500 行/记录和普通员工完整权限矩阵仍 DEFERRED。user_oauth 及 HTTP 飞书个人 OAuth 真实验收未完成，首发不得开启。个人变化版本更新不开放。

## main 与生产发布

main 本轮只读核对为 `854f3b956ae3600e12a3ffd2fc40d4fe624ced74`。全部已跟踪差异、未跟踪文件（特别是 3031/3033 迁移）列入 `RC2_CHANGE_MANIFEST.json`，不能只依赖 git diff 遗漏未跟踪源文件。
Docker 工作流 main/v* 会发布镜像；Python MCP main 发布规则保留。现有生产手工交付、固定 RC24 镜像，未见仓库直接 SSH 部署步骤；仍需运维确认外部自动更新控制面。生产 Python MCP 镜像 `sha256:14de96093a80c84863f73cb273a7982cc99a40989560f7d8432450a831a338b9` 不变。

实际 Docker 工作流目标为 Docker Hub 配置账号下的 `weknora-app`、`weknora-ui`（frontend/Dockerfile）、`weknora-docreader`、`weknora-sandbox`（含 cube/desktop 等变体）；这些流程与 main 无差异。`mcp-server/` 及其工作流也与 main 无差异，当前改动未命中该 PyPI 流程的 paths 触发条件；未来若修改相关路径，main 发布仍需独立核对。

最短后续顺序：

1. 确定旧来源门禁：安全更新完成验收，或由业务负责人批准逐来源暂停及陈旧/撤权风险。不得以安全拒绝代替兼容通过。
2. DDL、数据和启动证据已完成；补齐恢复检索等价性，优先评估保留生产物理索引的备份恢复路径。逻辑恢复 ANN 差异尚未解决，不能凭 valid 索引或数据量相同放行。
3. 准备最小权限 MCP 调用者 Key 和至少一名普通试点员工，或明确批准限已验收绑定身份的首发范围；不擅自创建员工/JIT 身份。
4. 处理既有能力键基线失败的明确维护者决定/门禁豁免（若适用），完成最终 CI、提交/PR、锁定提交 SHA 重建镜像和真实联合冒烟，再申请合并。
5. 获得生产 HTTP 风险接受、OA 生产 time_limited 策略的真实审批引用/截止时间、飞书应用范围、备份与窗口批准；按 Runbook 正式交付。不复制测试实例标记，不自动降级认证策略。

## 安全边界

生产仅只读审计、导出一致性备份和 docker save；无生产 DML/迁移/重启/权限变更。所有还原、故障注入、文件目录、Redis/APP 启动仅在独立隔离资源中。完整备份、数据库文件、原始日志及任何员工数据不进入 Git。

## 本轮实际执行与未执行

新增 Go Race：16 PASS/0 FAIL/0 SKIP；生产结构克隆保护：7 PASS/0 FAIL/1 SKIP（包含父测试计数）。独立工具集成用例：9 PASS/0 FAIL/0 SKIP。迁移六项对账检查全部通过，启动两个检查通过；它们不是额外 Go 测试数量。ANN 失败单列，未混入 Go 通过数。

本轮没有 APP 运行代码变化，因此保留 RC1 运行镜像；没有新造 RC2 镜像身份、重复浏览器验收或冒充最终 CI。已接受的 OA/飞书业务证据沿用 RC1，测试站点六篇成功文档及索引保留。生产仍为 RC24/DB3023，本轮无生产写入、迁移、重启或部署。尚未建立提交/正式 PR，合并门禁未通过。

2026-10-20 09:49（北京时间）最终只读复核：生产 APP `b613ee…` / DB3023 clean 未变；测试 APP `67b78b…` / DB3033 clean 未变。仅本轮独立 `knowhub-rc2-pg-final`、`knowhub-rc2-pg` 已停止，独立 APP/Redis 也在启动验证后停止；完整备份、实验库和失败证据保留，未删除卷。详见 `evidence/final-runtime-and-cleanup.json`。
