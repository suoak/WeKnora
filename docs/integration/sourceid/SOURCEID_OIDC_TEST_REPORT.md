# 开发与验证报告（2026-10-12）

工作分支：feature/knowhub-sourceid-oidc。
审计基线：ac7dbe5e2607af5ab4e74c062137e0cad48d0e50。
验证实现 SHA：8ae91bc4439dcc93be11844ada5ec1479f2730bc。
[最终 GitHub Actions 运行](https://github.com/suoak/WeKnora/actions/runs/38242327216) 全部成功。
后续交付报告提交仅修改本目录文档；以 git rev-parse HEAD 获取文档交付 SHA，运行代码与上述验证 SHA 一致。

## 已交付的支持范围

复用既有 OIDC、Discovery、SSRF HTTP、RSA JWKS 和本地 JWT。新增 SourceID 企业身份/审核映射、持久化会话/登出 tombstone、一次性 state、nonce、可选 S256 PKCE、固定审批回调、cookie 结果兑换、XML/显式压缩解析、审核 CSV 工具及 PG/SQLite migration。
新增接口为 POST /api/v1/auth/oidc/backchannel-logout、POST /api/v1/auth/oidc/result；既有 config 接口增加可选的公开 redirect_uri，不公开 secret。
完整变更文件清单见 [SOURCEID_CHANGE_MANIFEST.txt](SOURCEID_CHANGE_MANIFEST.txt)，机器可读验证统计见 [SOURCEID_VALIDATION_SUMMARY.json](SOURCEID_VALIDATION_SUMMARY.json)。

旧账号按审核工号映射和 issuer+sub 复用原 User ID，不按邮箱自动绑定，不修改权限/成员/知识库/API Key。唯一约束和数据库事务保护并发绑定。
未知员工默认拒绝；新增员工通过现有管理员开户服务、原默认空间策略，再导入审核映射。本版不支持未审核员工自动开户，也不支持静默 sub 轮换/岗位多身份切换。

外部会话按 issuer+SessionIndex 匹配；本地 SID 退出只撤销当前会话族。刷新和空间切换保留会话 ID，并在数据库中检查撤销；重复通知幂等，未知通知写 tombstone，防止晚到 callback 复活。
本地密码登录/既有本地退出语义保留。OIDC 邀请和目标页暂存保留，配置/网络失败留在登录页显示安全提示。

## 自动化结果

环境：Ubuntu 24.04 / Linux amd64 / Go 1.26.8 / GCC 13.3.0 / CGO_ENABLED=1。

| 检查 | 结果 |
|---|---|
| SourceID/认证重点用例，8 包，race | 63 个顶层测试、98 项含子用例通过，0 失败 |
| Repository/Service/Handler/Config/Middleware/Utils/MCP/MCPServer 整包回归，8 包 | 3143 个顶层测试、4624 项含子用例通过，0 失败 |
| 临时 PostgreSQL 17，实际 migration up/down、并发绑定/刷新/登出，race | 2 个测试通过 |
| SQLite 实际 migration up/down | 包含在 Repository 测试中，通过 |
| 前端认证/刷新/落地测试 | 21 项通过 |
| 前端类型检查与生产构建 | 通过 |
| Go vet、修改文件 gofmt、git diff --check | 通过 |
| 服务端和审核导入工具编译 | 通过 |

运行范围有重叠，不能将各行相加视为新增测试数量。整包回归包含既有测试；PostgreSQL 专用测试在普通运行中跳过，在独立 PostgreSQL job 中实际执行。
云端验证提交树；开始时已有的其他未提交/未跟踪工作完整保留，不混入这次验证提交。

关键新增用例覆盖：审核映射冲突、旧 User ID/管理员属性保持、邮箱变化、停用账号；状态/结果单次消费；精确撤销、设备隔离、重启后持久化撤销、晚到 callback；刷新重放/并发登出；JWT issuer/audience/expiry/nonce/subject 类型/azp 异常；UserInfo sub 精确一致；空间派生令牌撤销；XXE/畸形/超大/重复 XML/压缩炸弹；直接 peer CIDR，不信任 forwarded header；接口未授权/重复参数/跨站兑换；泛用 OIDC dev proxy；CSV dry-run/幂等/整批回滚/超限不截断。

首次云端发现过期签名令牌注销兼容性回归，已恢复既有本地语义；第二次发现持久化测试夹具校验了另一条默认停用用户，已让夹具保存实际启用用户，并新增停用账号拒绝已有令牌测试。保留全部既有回归，没有删除失败用例。
Windows 全量 Go 验证受旧 MinGW/原生 PostgreSQL parser/DuckDB 环境影响；不声称 Windows 全量通过。用户授权后使用上述云端结果，未修改生产环境或关闭 TLS 验证。

## 证据

Actions artifact：sourceid-linux-8ae91bc4439dcc93be11844ada5ec1479f2730bc，ID 11744092389。
GitHub 公布 SHA-256：2d1d457b204da027f9aaf51aeea8d7861741009d203fbf8485502df8c69c28a2；本地下载归档已核对一致。
内含 head、环境及 focused/regression JSONL。仓库仅保存统计，不复制完整日志或认证数据。
Mock 仅使用 httptest、临时 SQLite/云端 PostgreSQL，没有调用真实 SID 登录/退出接口，没有使用真实 SID/生产凭据。

## 外部阻塞与交付状态

| 状态 | 结论 |
|---|---|
| IMPLEMENTED_LOCAL | 上述安全支持范围的代码、Mock/数据库验证和云端回归完成 |
| BLOCKED_EXTERNAL | 是：真实生产协议/网络信任契约待确认；飞书全文及参数表已读取 |
| READY_FOR_SID_INTEGRATION | 否：还需真实配置与确认的协议字段 |
| SID_INTEGRATION_VERIFIED | 否：未执行真实 SID 登录/登出 |
| READY_FOR_RELEASE | 否：真实联调、传输风险验收、生产备份/部署/回退演练未完成 |

尚缺正式 issuer/Discovery/JWKS、签名算法、客户端认证、PKCE/nonce 支持；员工自然人与岗位身份规则、状态字段；OIDC 与 SessionIndex 的来源及等价证明；通知源认证、参数位置/Content-Type、压缩、响应/重试和外部 ID 生命周期。
Token Response 专有会话扩展、原生通知签名尚未实现，若真实协议要求，应补适配并重新验证。body/form-urlencoded、XML 或显式 base64+raw DEFLATE、空 HTTP 200 是本地支持范围，不能作为真实契约已确认的证据。
已读三份用户 HTML。用户完成授权后，飞书 CLI 已读取企业 OIDC 文档 revision 20 全文和全部 13 张嵌入参数表，详情见 [SOURCEID_PROTOCOL_REVIEW.md](SOURCEID_PROTOCOL_REVIEW.md)。其中员工属性示例为通用学工号/在校状态，不能认定企业自然人工号及启用状态。Profile 表单传 token 的方式已补充显式请求适配；正式 Token/Profile Content-Type 和生产端点仍需确认。

审批 HTTP callback 保持原值；授权码/cookie 明文传输风险已记录。未来 HTTPS 须取得新 URI 审批，并同步更新批准 URI 校验、入口、配置和回调注册。
SourceID 默认关闭；不推送/合并主分支、不部署生产、不操作生产数据库。P6 步骤见 [SOURCEID_OIDC_DEPLOYMENT.md](SOURCEID_OIDC_DEPLOYMENT.md)，回退见 [SOURCEID_OIDC_ROLLBACK.md](SOURCEID_OIDC_ROLLBACK.md)。
