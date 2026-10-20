# RC3 最终 CI 与业务验收收口

运行源码：`1e2ded5b67ce9487966210d31011cf9c5b8cdfb9`。PR #28；尚未合并 main，未操作生产。下述证据仅证明对应源码和测试实例；报告提交可能仅改变文档，当前 PR HEAD 检查仍须全部核对。

## 结果与身份

- 测试实例：172.30.232.42 / aqyfzx10，原飞书 v7 测试部署。
- APP：`sha256:0c84cac5af5103b73102c6261f32130d3e51eeed1b8cbf77fda61de20c4bd983`。
- Frontend：`sha256:6a2def24a8dcf69cecac633cfb4e0c2752e472fe500a34ce464edb6db13ffa22`。
- 完整源码 Manifest：5783 文件，SHA256 `864b5c39e54331359a51641ff38108c3ccdf266daea97392fc4a637c20a2ff70`，见 `evidence/final-committed-source-manifest3.json`。
- APP 与 Frontend 均由上述提交的受核对输入重新构建；Frontend 构建使用 Node 22.22.1 / npm 10.9.4，正式 GitHub 前端门禁使用 Node 24。AnyDoc 静态库固定 SHA256 `d5543b2cf68400c048992a192475aa70e895fda0abc495b85afb68dfd699122a`。
- OA：显式 `code_flow_tls_userinfo` / `continuous_test`，测试无计划截止时间；保持 HTTP 回调，Token/UserInfo 为可信 HTTPS。没有启用固定公钥或失败自动降级。生产不得照抄测试配置。
- DB3033 clean；PostgreSQL、Redis 容器未重建，无新迁移、ForceVersion、队列清理或旧知识重建。旧飞书同步屏障保持开启。

## OA 首次绑定竞态

首个 review 查询未命中后，另一请求可能已经提交 approved review 与 enrollment。原请求随后看到 enrollment，却在 pilot 分支直接拒绝，造成合法并发首次登录失败。

修复仅在这个窗口重新读取 approved review，并按 review → enrollment 的一致顺序锁定、核对 issuer、RJGH、sub、原 user_id、审核来源、审批状态及批准的账号/邮箱归属，再在同一事务解析已有身份。审核撤销、错误身份、其他用户 enrollment、失活用户、未经审核记录仍拒绝；Session 保留原有再次审核校验。没有改 RJGH/RJEMAIL、JIT 默认角色、认证策略或登出语义。

确定性测试先让败方完成真实 missing-review SELECT 并等待屏障，之后胜方才正常提交，再恢复败方；不是先绑定再运行。旧实现的合法并发场景稳定失败。SQLite 与真实 PostgreSQL 均覆盖正常及 14 类边界。PostgreSQL CHECK/FK 保持启用，损坏数据注入与约束拒绝分开验证。没有删除红色契约或降低断言。

## 跨导入者 generic 400

根因是正式路由已认可同空间 contributor 的 KB 写权限，而 PersonalRecovery 又额外要求 KB 创建人/管理员，提前拒绝；handler 将 access.ErrForbidden 映射为 generic 400。该案例不是数据库唯一约束冲突，也不是跨用户 OAuth 凭证复用。

恢复链路保留用户/成员实时状态和统一 KB editor/write 授权，移除重复的创建人限制。授权拒绝明确映射 401/403/404。相同 application/resource/KB 被另一人导入时，维持既有 `400 feishu_already_imported` 业务合同，不返回其 Import/Attempt/owner/KID；同人重试继续幂等，另一个有权 KB 可独立首次导入。没有为测试改变预期状态码。

最终镜像上 Docx、Sheet、Bitable 三例均得到明确重复错误；原 KB 的调用者私有 Import 数量始终为 0，没有复用其他人的记录。8 项越权读、写、检索和管理请求均为 403。

## CI 与 lint

无限制 main 差异 lint 初始实际为 1705 项，而非截断展示的 174 项。清理主要是格式、120 字符行宽、注释、未使用参数和显式忽略原本最佳努力的清理/测试响应写入错误。未提高基线、未改审计阈值。保留拒绝旧 DialTLS 覆盖的安全检测及有解释的局部 staticcheck 指令。

删除的是从未调用的 workbook-media 校验器及其辅助函数，活动 workbook/附件校验保留。DocReader 不可用分支原先忽略知识状态保存失败，现在返回该持久化错误。除此之外的清理错误处置保持原有语义，没有恢复 Replace 或破坏性 partial-chunks 清理。

正式 CI 两次发现 `gofumpt` 未同步项，workflow 因 continue-on-error 仍显示成功；两次都按子任务 FAIL 处理。后来检查全部 237 个 PR Go 文件，而不再逐批依赖只显示前三条的日志；最终格式差异 0，2986 个 Go/依赖输入与隔离检验树一致，未限制数量 lint 为 0 issues。最终 GitHub lint 子任务本身 SUCCESS。

Frontend 改用原有 12px/150ms token。i18n 审计识别 imported local useI18n bundle，要求全部五种语言具备本地键；未知 bundle 失败封闭。没有增加全局基线或删除缺键断言。

| 验证 | PASS | FAIL | SKIP | 范围与证据 |
|---|---:|---:|---:|---|
| Linux 原生 AnyDoc 全量 Go | 14023 | 0 | 80 | 完整提交归档 48f3e5dac；final2-full-go.jsonl |
| OA 确定性 SQLite + PG Race，count=5 | 150 | 0 | 0 | oa-deterministic.jsonl |
| 并发首次登录 Race，count=100 | 100 | 0 | 0 | oa-parallel.jsonl |
| 跨导入者/错误映射专项 Race | 8 | 0 | 0 | duplicate-final.jsonl |
| 扩展 OA/飞书/Repository/配置 Race | 687 | 0 | 13 | final-broad-race.jsonl |
| 最终 1e2ded5b6 原生归档定向 Race | 47 | 0 | 0 | final3-committed-gate.jsonl |
| 最终 1e2ded5b6 GitHub 前端测试 | 1577 | 0 | 1 | job 118485411921；另 type-check/build 成功 |
| 最终 1e2ded5b6 正式 GitHub Job | 9 | 0 | 3 | final-github-ci.json |

计数包括子测试及重复运行，不能相加为独立测试数量。48f3e5dac → 1e2ded5b6 的 11 个文件仅格式/等值整数写法变化；规范化声明 AST 全部相同，见 `final-format-only-ast-proof.json`。最终 1e2ded5b6 自身的 GitHub Go format/vet/test/build、Portal/RBAC/Race、迁移、MCP 代码兼容及容器启动门禁均 SUCCESS，原生定向 Race 也在最终归档运行。没有把父提交的全量 native 测试冒称为在另一个 SHA 重跑。

3 个跳过项属于分支条件限制的 Datasource Sync Execution Gate，不计 PASS；隔离 PG/Redis 的恢复契约另有实际执行证据。Python MCP 真实调用为用户决定 WAIVED，保留生产旧服务，不以 Go MCP 测试替代真实 Python 调用。

历史能力键失败及修复记录保留在 RC3_VALIDATION：仅修测试 CRLF/逗号规范化并增加真实错误键拒绝测试，未改业务能力键。公共 HTTPS DNS 用例在 Linux 全量真实运行通过，预检获得三个公网地址；未固定 IP、修改 hosts 或放宽 loopback/私网/恶意重定向拒绝。历史 ::1 的外部根因没有被证明。

## 真实测试与数据保护

最终镜像由同一获授权普通员工经正常 SID 浏览器流程登录。user_id、RJGH 绑定元组、邮箱、角色及成员关系与私有基线相同；用户/身份/审核记录仍为 4/2/2，未重复创建或提权。公开证据只保留必要脱敏摘要。

在独立测试 KB `33520acb-583a-4125-82a6-c07484d44917` 首次导入三格式，最终均 completed/verified。Portal 召回数 Docx/Sheet/Bitable 为 5/3/5，Hybrid 各 10，均核对预期 KID、非空 Chunk 和飞书来源。每种重复导入保持相同 Import/KID。已有三篇飞书知识另行检索通过。最终真实 Docx RAG 正常完成，无截断，引用 4 条且包含对应 KID；前一纯格式父版本的 Sheet 短答及 Bitable 小问题也真实通过。

退出返回 200，旧 Access 再访问为 401；本轮没有签发 Refresh Token，真实 Refresh 撤销 NOT_AVAILABLE，不能算通过。原邮箱首次关联和新员工首次 JIT 本轮没有新的授权身份，NOT_EXECUTED；已有普通员工重复登录、绑定保持和权限拒绝已真实通过。

Bitable 四表汇总出现模型输出截断，原始失败保留；更小的表名问题完成并引用正确来源。这不等于复杂汇总已完整通过。Sheet/大表长回答、>500 行/记录及高级字段完整矩阵继续 DEFERRED，不新增模型/索引重构。

第一次最终浏览器检索探针遇到一次无 HTTP 结果的 Error，当时页面有导航；不能确证具体原因。该错误未计 PASS，原错误文件保留。重新检查同一批 KID 后所有原断言通过，没有重建知识或修改断言。索引等待阶段未报成功，直到三条均完成。

部署前数据库、文件、配置和镜像均备份，并在 network=none 的独立 PG 恢复验证。原 16 Knowledge、21564 Chunk、5295 Embedding、6 KB、352 Wiki 的规范化行摘要与恢复备份一致（排除可变访问/更新时间），后续新测试内容另计；详细证据 `final-all-existing-data-preservation.json`。无生产写入。

当前没有执行异常回滚。回滚脚本/镜像及备份恢复路径已准备和核验，不能把“未执行回滚”写为已完成运行回滚演练。测试可回滚到 APP a40dd80dfb4a… / Frontend 6b51e4484b03…，不还原业务数据库、不删除用户或新导入。

## 完整范围与发布判断

相对 main `854f3b956ae3600e12a3ffd2fc40d4fe624ced74`，锁定源码涉及 962 个路径、165 个历史提交，完整保留原 954 路径；额外为 4 个测试和 4 个报告/证据文件。逐路径 blob/SHA256/最后修改提交见 `final-locked-pr-audit.json`，不宣称每份历史文档每一行都重新人工审阅。所有 37 个迁移相关路径未被本轮更改，36 个 SQL 对应已验证范围；没有 3032。

代码和对应运行版本已达到正式 Review 的技术依据。PR 当前 HEAD 的检查及人审仍须满足，**没有授权直接合并 main**。main/v* 会触发 Docker Hub 镜像推送；仓库内无 SSH 生产部署不能证明没有外部自动拉取，需运维确认。

生产继续 NO-GO，最少还需：维护窗口/负责人/应急人及旧来源最大陈旧期限；HTTP OA 生产风险接受和真实 time_limited 配置/有效期；main/latest 外部更新关系确认；正式发布与回滚批准及受控 main 合并。业务已接受短期停更 7 来源/354 Knowledge 的方向，不等于期限和执行人已指定。生产 RC24、DB3023、Python MCP 未更改；迁移与物理 ANN 等价性使用此前已通过证据，不重复生产实验。
