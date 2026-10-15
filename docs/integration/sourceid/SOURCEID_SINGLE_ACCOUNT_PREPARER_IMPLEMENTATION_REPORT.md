# 单账号准备器实现与最终OA验收准备结果

日期：2026-10-16。**CLI_IMPLEMENTED / SYNTHETIC_PASS / APPROVED_ACCOUNT_PREPARED / REAL_OA_NOT_EXECUTED**。

原任务只授权开发、合成测试和预演；随后用户明确授权在测试环境写入指定邮箱，并通过保护输入框提交。因此本轮追加执行了**一个普通测试账号写入**。这不扩展为网络规则、企业门禁、额外空间或真实SID登录授权。

## 1. 实现与代码范围

实现提交：`93193446db6450f769edf0da715de8d285726b2a`；最终补充清理批准审计：`2c185e9c076a04201d0e51ee2ea256479250014d`，均已推送开发分支。没有混入工作区MCP、前端或其他未提交改动。

| 文件 | 能力 |
| --- | --- |
| `internal/accountprep/prepare.go` | 单数据库事务、只读规划、普通权限/绑定冲突检查、业务注册与成员复用、事务审计、幂等、定向detach |
| `cmd/sourceid-test-account/main.go` | 默认dry-run；显式apply；viewer默认；contributor必须另给批准标志；保护stdin输入、静态安全错误；无HTTP监听/迁移/SID调用 |
| 对应两个`*_test.go` | SQLite/PostgreSQL生命周期、并发、故障注入、边界、清理及CLI隐私测试 |
| `.github/workflows/sourceid-oidc.yml` | 纳入CLI构建、竞态专项及PostgreSQL并发验证 |

注册显式TenantProvisioningTenantless，复用userService.Register、TenantMemberService.AddMember及现有仓库；所有仓库都绑定同一GORM事务。未调用默认个人空间注册、不改全局invite_only、不增管理权限。PostgreSQL用固定非PII advisory事务锁串行化工具操作，同时锁定目标空间与匹配用户；邮箱按trim/lower精确检索，重复、停用、软删除、高权限、组织管理与身份历史冲突均拒绝。

操作UUID在现有audit_logs写入原用户/空间/成员ID、创建与home变更标志、批准引用；没有新增数据库表或迁移。重复操作核对原用户与成员；同一邮箱换操作ID不会重复建用户；关闭的操作不能再次使用。异常安全返回，不把驱动错误、邮箱或SQL吐到控制台。数据库提交结果网络异常时仍需按原操作UUID查询审计，不盲目生成新操作重试。

## 2. 输入与日志安全

CLI不提供邮箱参数；拒绝可见终端直接输入，使用隐藏输入保护launcher→DPAPI→加密SSH→进程stdin。结构化stdin只允许email/database_dsn，拒绝SID Secret/Token/RJGH等未知字段。数据库配置仅限受控运行进程，不读APP的SID Secret，不把真实值放Git、CI、普通报告或命令参数。

GORM与业务服务日志在本调用上下文丢弃，避免旧错误日志回显邮箱；CLI参数解析错误亦禁止回显原值。实际数据库连接使用仅该连接生效的日志参数，避免SQL/参数错误写入普通数据库日志；未修改全局数据库配置。审计仅存保护对象ID与业务决定，不存邮箱副本或秘密指纹。

写入后在保护进程内扫描APP、Frontend、PostgreSQL有限日志窗口，均未出现该邮箱；原始日志及匹配值未导出。这是有界检查，不等于对全部历史日志的保证。[日志隐私收据](evidence/account-prep-log-privacy-20261016.json)。

数据库访问本身是运维授权边界；`--test-host`是操作防误用标签，并非自动证明DSN归属。实际launcher固定打包机SSH身份、项目、镜像、数据库版本和网络，不允许将任意DSN视为已授权数据库。

## 3. 合成测试与构建

最终专项结果：[测试及保护基线复核](evidence/account-prep-postcheck-20261016.json)，**7个顶层测试、17个子测试、失败0**。

- Windows Go1.26.8：工具包与CLI测试通过；首次遇本机MinGW缺少stack-check链接符，使用本机`-extldflags=-lssp`完成链接验证，未改变业务代码或Linux工具链。
- Linux沿用既有Go1.26.7构建镜像与离线依赖，不升级运行库；`-race`专项、go vet及CLI构建通过。
- 独立PostgreSQL私有Docker网络与临时数据库：生命周期、三并发同操作仅一用户/一成员/一审计通过，测试后数据库容器与网络均删除，未借用业务库作合成测试。
- 故障注入覆盖用户创建后成员插入失败、用户/成员完成后审计失败，均回滚；清理保留账号，已有账号home与权限保持，后来产生身份绑定时拒绝自动清理。
- 拒绝owner、未经批准contributor、非owner操作者、停用/跨租户/系统管理员/其他空间owner/组织管理、重复邮箱、绑定/审核/会话冲突、缺表及操作冲突。
- CLI测试确认未知敏感字段、多段JSON、参数误填及缺少保护数据库配置均不回显输入。

实际创建账号所用93193446d二进制SHA256：`918c292f29a53afd4ff95a9d158a88e4c52b78fe3566592647804ab7b8fce9f2`；最终2c185e9c0候选（补充清理审计、未再次写入实际账号）为`cb7fd6d852eb76b179cef93346367da0401dfb9eb3e75a464566b0d717194500`。独立目录`/home/aqyfzx/ai/knowhub-accountprep-20261016`；以431226c3b的已验证服务依赖基线加四个工具源码构建，逐文件摘要比对通过。**没有升级APP或Frontend。**

GitHub CI：最终提交[38772429095](https://github.com/suoak/WeKnora/actions/runs/38772429095) **全部通过**：browser、frontend、linux、postgres四项success。包括真实Vue+Mock SID浏览器、前端类型/回调测试/构建、竞态专项、PostgreSQL并发、认证/MCP/飞书回归、诊断过滤及server/CLI构建。见[CI收据](evidence/account-prep-ci-20261016.json)。初版38770878548的浏览器、PostgreSQL及Frontend通过，Linux运行中被最终审计补充提交按分支并发策略取消，不能记为初版全部通过。

## 4. 已批准的实际单账号准备

北京时间 **2026-10-16 00:45:12** 完成。证据：[实际写入收据](evidence/approved-ordinary-account-created-20261016.json)。

| 项目 | 结果 |
| --- | --- |
| 写入前 | 重新核验指定项目、镜像、DB3027 clean、用户1/空间1/身份及Session0；先保存最新测试库保护备份 |
| 真实dry-run | 计划创建1用户、添加1成员，无其他变更；通过 |
| 实际apply | 创建1个active普通viewer，加入既有测试空间；未创建个人空间或身份映射 |
| 幂等复核 | 相同操作再次dry-run，already_applied=true、create_user=false、add_membership=false |
| 写入后 | 用户2、空间仍1、owner仍1；身份、审核、SourceID Session、本地企业Session均0 |
| 容器/门禁 | 原APP/Frontend及配套容器不变；oauth_form、protocol_only=true、contract_confirmed=false、email/auto_binding=false不变 |
| 保护基线 | 原始user_id/tenant_id/成员ID/角色仅在服务器private收据；公开报告只输出一致性布尔值 |
| 知识库 | 有效KB仍0；尚未建立非空资源及跨空间对照，不能宣称完整ACL验收就绪 |

数据库备份：工具目录`private/before-approved-account.dump`；定向操作保护收据位于同目录，目录0700、文件0600。公开操作UUID：`cc558c5d-e9fb-4b92-95d8-566c325ce145`。批准引用仅代表用户本次单账号写入授权，不能用于local_approved会话审批。

实际运行采用一次性容器、原APP镜像的运行库和只读挂载CLI，连接批准测试库；进程结束即删除，未将新程序复制进运行中APP、未重启业务服务。没有进行员工SID认证或本地企业登录。随机本地密码未展示或提供给Agent，账号准备不等于员工登录完成。

北京时间00:53:15最终只读复核：active用户2、owner1、viewer1、普通自动绑定候选1、重复邮箱组0；原APP容器ID/镜像、Frontend容器ID/镜像、DB3027 clean及全部门禁不变；实验网络和容器已移除。[最终收据](evidence/account-preparation-final-readonly-20261016.json)。

## 5. 定向清理与回滚

默认只规划。需额外明确批准后，使用原operation、同目标空间/授权owner、保护stdin邮箱与新的真实清理批准引用，添加`--detach --apply`。不输出邮箱或DSN，不从普通报告取个人数据。

清理检查原成员ID/角色/home未改变，账号仍为普通用户且没有SID身份/审核/会话历史；不满足就拒绝并转人工，不能自动删除历史绑定。撤销账号相关本地Token后只移除本操作添加的成员关系、恢复原home；工具新建账号停用而不删除，既有账号仍active。仅同一操作记录可清理，原owner不受影响。清理审计保留，关闭操作不可重新apply。

成员撤回可能撤销该用户其他本地Token，必须在清理审批中明确影响；不能将它当成SID统一登出。创建、清理及后续真实OA绑定是不同批准动作。本轮只在合成数据中执行清理，未撤回已批准的实际viewer。

## 6. 资源、网络与会话审批

详见[夹具、隔离预演与会话安全补充](SOURCEID_TEST_FIXTURES_AND_NETWORK_REHEARSAL.md)：

- 允许KB需真实解析非空合成内容并能检索；当前没有有效KB，尚未完成。
- 禁止KB必须真实存在于未授权空间且无间接共享。目前只有一个空间，额外隔离空间/夹具需单独批准。
- 双栈允许/拒绝来源、后端分段、健康检查及loopback发布绕过的隔离环境预演通过，实验准确清理；不是实际80/8080源隔离证明。
- 实际防火墙链内容因只读权限不足UNKNOWN；其他18080/18081/18082/8082等入口的代理旁路及IPv6仍须正式切换前核查。当前未切换。
- 首期四项10m、无Refresh仍为候选。新增请求在过期后拒绝，不保证已在途流立即取消；长流窗口、重新OA体验、撤销操作者及真实政策引用均须审批。

## 7. 距离一次真实OA验收的最少剩余项

1. **SID三个答案**：停用传播D、已有SSO再authorize准入复核、未消费code的时效/停用边界C。不得猜测。
2. **内部签署**：四项会话期限与无Refresh、流式在途窗口、普通自动绑定范围、真实政策引用、审计与撤销责任人。
3. **资源夹具**：允许的非空可检索KB；另批跨空间禁止资源的最小夹具。账号现已准备，无需再创建第二个员工账号。
4. **实际隔离**：另批规则切换、覆盖所有入口、从批准与独立禁止来源获得实测证据；专用浏览器或只有一个普通账号不能替代。
5. **最后单次授权**：上述齐备后另批企业门禁成套放行及一次真实OA，保持严格RS256/nonce等校验；历史signature故障独立跟踪，不放宽、不重试。

APP431226c3b、Frontend1ba24ef5f及DB3027保持，未执行真实OA业务登录；本轮结果不等于已经允许普通员工自动进入Portal。
