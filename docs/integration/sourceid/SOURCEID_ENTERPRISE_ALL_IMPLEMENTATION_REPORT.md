# 全员 OA 接入最小扩展

日期：2026-10-16。基于 `832b6652adf541892545a7eac8b68733f846ab95`，独立分支 `feature/knowhub-sourceid-enterprise-all`。代码候选：`09e6d86db731953834916e58ac5f3f5985ad0aa1`；[CI 四项通过](https://github.com/suoak/WeKnora/actions/runs/38887398840)。**实现不等于正式启用；现有试点仍运行832b6652a，未执行新迁移或真实绑定。**

## 架构与范围

保留 `SOURCEID_ADMISSION_MODE=oidc_sso_approved` 及原Access-only Session，引入独立账号范围：

| 范围 | 归属证据 | 人数限制 |
|---|---|---|
| `SOURCEID_ACCOUNT_SCOPE=pilot`（默认） | 受保护试点名单 | 原100账号/64KiB保护保留，仅用于灰度 |
| `SOURCEID_ACCOUNT_SCOPE=enterprise_all` | 数据库可信归属记录，按issuer+RJGH查询 | 没有固定业务账号数量上限，不要求逐人JSON名单 |

全员模式还必须明确 `SOURCEID_ENTERPRISE_POLICY_CONFIRMED=true`、真实 `SOURCEID_ENTERPRISE_POLICY_REF`、经批准 `SOURCEID_ENTERPRISE_MAX_AGE`。缺少任一项拒绝放行；不删除pilot校验，不使用角色分流。

认证仍为原服务端OIDC流程：state、nonce、RS256、issuer、audience、时间、UserInfo/sub → 可信归属记录 → 原有全角色绑定 → 原本地Session。无Basic、Callback、PKCE或Refresh策略改动。普通用户、owner、组织/系统/跨空间管理员适用同一代码路径。

## 实现位置

- `internal/config/sourceid_enterprise.go`：默认pilot、enterprise审批/新鲜度门禁；JIT开启时明确拒绝，未提供虚假可用开关。
- `internal/application/repository/sourceid_enterprise.go`：可信归属查询、批量只读账号join、幂等增量同步、绑定/Session并发保护、停用撤销。
- `internal/application/repository/sourceid_binding.go`：原有全角色唯一匹配/审核/撤销实现直接复用，没有重复用户系统。
- `internal/application/service/sourceid.go`、`sourceid_local_session.go`：按范围选择证据；enterprise每次Access及SwitchTenant沿用会话检查时复核本地归属有效性。
- `cmd/sourceid-ownership`：仅命令行，默认dry-run、受保护stdin、汇总输出，不新增公开API。
- 新增 **003028**：归属证据/审计表、唯一约束、规范化邮箱非唯一索引。当前运行3027未改变。

登录每次只读取一条归属记录，不加载全员文件。`issuer+RJGH`、`issuer+user_id`、`issuer+规范化邮箱`数据库唯一约束；已绑定后核验原user_id和sub，不因邮箱变化重绑。归属记录不能自动迁移给另一个user_id，也不能回收给不同RJGH。

新表是既有账号的归属证据，不是HR在职目录，也不是第二套用户系统。`status=active/disabled`表示可信来源授予/撤销此归属准入；不能猜测SID的STAFFTYPE等字段。普通SSO仍不保证实时员工在职，源数据新鲜度、同步延迟和会话策略需正式批准。

## 同步及异常

每行事务提交；同一版本相同内容幂等，不同内容或旧版本拒绝。批内相同记录幂等、冲突重复拒绝；每批每个身份应提供一个最新事件。成功行不因另一行异常回滚，失败计数返回非零，可修正后重放。

归属无证据/过期/目录数据库不可用、重复邮箱、账号停用、主体冲突、已撤销绑定均拒绝对应账号。源同步中断但已有记录仍在批准新鲜度内，可继续使用；超出max_age/valid_until后，新登录及后续Access拒绝，不无限沿用缓存。disabled同步在同一事务撤销该issuer/user的企业Session和派生Token，密码登录和其他Provider不受该定向操作影响。

同一员工归属行锁串行化首次绑定及Session创建。新增唯一约束作最后保护。同步不修改users的角色/管理员标记/tenant_id或成员关系，不预先写入真实OIDC sub、身份绑定或业务会话。

## 当前完成与未完成

已实现：双范围、批量证据维护/自动账号join、幂等/增量/审计、原账号全角色关联及归属停用撤销；合成/CI结果另见[验收报告](SOURCEID_ENTERPRISE_ALL_ACCEPTANCE_REPORT.md)。

未启用：真实全员模式、真实目录数据、真实绑定、正式会话政策。新迁移和候选APP尚未部署。新员工JIT只有[独立设计](SOURCEID_ENTERPRISE_JIT_ACCOUNT_PLAN.md)，没有自动开户实现，不能通过设置true宣称支持。

仍需数据负责人提供可信历史归属与增量来源；缺少历史证据的账号进入异常处理，不要求所有管理员逐个额外申请，也不阻塞其他有证据的账号。

配套：[批量归属方案](SOURCEID_ENTERPRISE_OWNERSHIP_SYNC_PLAN.md)、[JIT方案](SOURCEID_ENTERPRISE_JIT_ACCOUNT_PLAN.md)、[切换/回滚](SOURCEID_ENTERPRISE_ALL_RELEASE_OPERATION_CARD.md)。
