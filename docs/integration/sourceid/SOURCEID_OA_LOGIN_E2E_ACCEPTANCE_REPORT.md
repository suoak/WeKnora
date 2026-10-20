# OA 登录合成验收报告

状态：IMPLEMENTED_GATED / SYNTHETIC_VALIDATED；本轮无真实 SID 登录、真实绑定或部署，不是 READY_FOR_RELEASE。

## 已获得的真实测试结果

实现验证 SHA：`431226c3b1bd5338ec06c1bd0520e637d5fed398`。[最终 CI](https://github.com/suoak/WeKnora/actions/runs/38717758880) 已完成，linux、postgres、frontend、browser 四个 job 全部 SUCCESS；包括 race 检查、go vet、服务器/工具构建、前端类型检查、认证回调测试和构建。

| 测试日志 | PASS 条目 | FAIL 条目 | SKIP 条目 |
| --- | ---: | ---: | ---: |
| 认证/SourceID 专项 | 276 | 0 | 9 |
| 认证/MCP 回归 | 4808 | 0 | 21 |
| 飞书/联合专项 | 233 | 0 | 1 |

数字包含父测试及子测试，且不同 job 有交叉，不能相加作为独立用例总数。普通 Linux job 跳过的 SourceID PostgreSQL 和浏览器用例已由专门 job 执行通过；其他依赖特殊环境的 Postgres/真实 Parser 等跳过项不算通过。完整列表见 [最终机器证据](evidence/phase-b-local-ci-final-20261015.json)。

首轮浏览器集成因测试工具问题 FAIL，不能算作通过，保留 [历史机器证据](evidence/phase-b-local-ci-history-20261015.json)。随后修正 HTTP 就绪检查、严格截止时间和进程清理，将 URL 改写改为专用 Chromium 的单域名 loopback 解析，并避免页面跳转后重复读取已经丢弃的 DevTools 响应体。没有放宽业务安全控制。

`TestSourceIDLocalBrowserBusinessLogin` PASS（15.93 秒）。浏览器断言覆盖 Portal 渲染、刷新后登录态恢复、auth/me 200、原租户/普通 contributor 角色和知识库夹具访问、退出后 401、无效员工已有 SSO Cookie 时重新授权拒绝。

共两笔成功合成事务：一笔直接 Handler 校验、一笔真实浏览器流程，各只兑换一次新 code；拒绝授权不兑换。诊断产物未发现合成 Secret、Access Token、完整 Basic Header 或 JWT 原值。见 [浏览器机器证据](evidence/phase-b-local-browser-20261015.json)。

本机配置/仓库 SourceID/OIDC 测试通过；Windows 完整 service 测试受原有 DuckDB C++ ABI 链接问题阻断，完整服务和浏览器验收以 Linux CI 为准。未修改项目依赖或编译基线绕过链接错误。

## 自动化范围

- 服务/仓库：首次邮箱匹配、已有 RJGH 绑定、邮箱变更保持原 ID、多设备隔离、原租户保留、Access/Refresh 有效性、绝对/空闲/准入到期、绑定/管理员/本地退出撤销、重放撤销与 PostgreSQL 并发。
- 准入：三个策略的 Mock，ALLOW/DENY/UNKNOWN、过期、不可用、未安装适配器；正式首期只采用授权准入。
- 浏览器：真实 Vue 登录页→Mock authorize→真实 callback Handler→Token/RS256/nonce/UserInfo→自动绑定→本地会话→Portal→auth/me 200；页面刷新后恢复原会话；原 contributor 成员关系及隔离知识库夹具访问；退出后 auth/me 401；员工停用、已有 SSO 时重新授权拒绝且不发送 Token 请求。
- Portal 其他接口有界夹具响应，知识库接口使用隔离夹具；此测试不是完整容器部署、文档处理、模型推理或 RAG 质量验收。
- 本轮配置/仓库、完整认证/MCP、飞书兼容、真实迁移顺序、前端回归和构建已由上述 CI 执行通过。

浏览器通过 Chromium 专用进程的 host-resolver-rules 将唯一 knowhub 域名映射至 127.0.0.1:18195，其他外部网络请求阻断；不查正式 DNS、不更改全局 hosts、不访问生产。只有随机 loopback Mock 与临时 SQLite 数据库。自动断言不输出 JWT、Cookie、授权码或身份原值；测试失败也只输出安全错误类别和脚本行号。

## 必须区分的结果

真实 frontend 与真实认证 Handler 的集成 PASS，不等于打包机全容器端到端验收；Mock 拒绝无效员工不能证明正式 SID 的停用传播时效或已有 SSO 复核规则。STATUS_BREAKPOINT 未进行真实员工复验，不宣称解决。

## 后续真实测试账号方案（尚未执行）

单独批准后，由受保护渠道录入可信邮箱到独立测试账号；创建普通 contributor/viewer，准备测试知识库，记录原 user_id、tenant_id、空间角色和知识库 ACL 基线。重复邮箱、管理员、冲突或停用不自动放行。真实邮箱/RJGH 不进入 Git、CI、普通报告或日志。

审批与 SID 时效确认、3027 部署批准、真实测试账号准备完成后，才可以申请一次真实 OA 登录。不能为进入 Portal 简单打开所有开关。
