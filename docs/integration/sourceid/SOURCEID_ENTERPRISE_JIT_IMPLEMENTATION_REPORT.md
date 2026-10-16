# 企业新员工 JIT 最小实现

日期：2026-10-16。源码候选：`f40b1602f69ce5d88e1e87dc3cefae36e9bde356`，分支 `feature/knowhub-sourceid-enterprise-jit`，基于已验收09e6d86db。**已实现但默认关闭；当前运行试点仍为09e6d86db，不包含此新JIT，003029未在运行库执行。**

## 一条OA入口，两类账号处理

严格OIDC、state/nonce、RS256/Issuer/Audience/时间及UserInfo sub校验不变。已有归属按原 enterprise_all 方法关联原 user_id；JIT不改变原角色、成员或ACL。已有账号没有可信归属、邮箱重复、soft-deleted/停用、主体冲突、旧审核/撤销等情况，不能降级开户。

只有没有历史账号且独立批准的企业JIT身份/开户资格成立，才能注册tenantless普通账号。不创建个人owner空间，不自动加入任何空间或知识库，不设置系统/跨空间管理员标记；后续走正常邀请授权。当前SID身份可信并不自动证明实时在职，普通SSO与JIT政策必须正式接受该残余风险。

## 代码与事务

- `internal/config/sourceid_enterprise.go`、config.go：新增JIT_POLICY_CONFIRMED、JIT_POLICY_REF、JIT_EMAIL_DOMAINS；JIT_ENABLE默认false。仅enterprise_all可开，缺任何审批/域契约拒绝。域为精确企业域，不是逐人名单或角色限制。
- `internal/application/repository/sourceid_jit.go`：先走历史归属，禁止失败后JIT；新开户事务核查所有规范化邮箱（包括软删除）、RJGH/sub/审核/撤销/归属冲突，复用原绑定，原子写新用户、开户来源、归属及审核审计。失败整体回滚。
- PostgreSQL新注册短事务锁保护与其他注册的并发，唯一约束作最终保护；已有历史账号登录不获取该注册表锁。同一身份12路竞争只创建一个用户。
- `internal/application/service/sourceid_jit.go`、user.go：复用Register的tenantless流程，内部不可由HTTP输入的专用context选择外部认证账号。直接写不可用密码标记，避免为随后废弃的密码计算bcrypt；公开密码注册及其他Provider仍按原bcrypt流程。新账号无法用本地密码登录，OidcOnlyLogin标记开启。
- `internal/application/service/sourceid.go`：严格协议/政策通过后调用JIT，复用原本地Access-only Session；响应正确标记首次新开户，后续不是新用户。未新建公开API。
- `003029_sourceid_jit_registration`（PG/SQLite）：只记录真正由JIT创建的新账号来源，复用003028归属表和原用户系统；不改变历史用户或权限。新增迁移仍需独立批准后部署。

新账号来源标记与历史归属证据分开。只有已由JIT创建、同一批准政策、原user_id/sub保持、未停用或撤销者，可在新的完整OIDC登录后更新其JIT身份快照；页面刷新、Access和Refresh不会续期。权威目录接管后回到统一目录新鲜度检查，JIT不覆写目录证明。新旧账号均保留本地退出、撤销、绝对/空闲期限和无Refresh。

## 新增配置（仅候选，不加载真实环境）

JIT_ENABLE=true前，还必须有JIT_POLICY_CONFIRMED、真实JIT_POLICY_REF、完整获批JIT_EMAIL_DOMAINS，以及enterprise_all、oidc_sso_approved、local_approved和原会话/邮箱/归属批准。没有真实政策引用，不以synthetic测试值放行。

正式启用前必须完成历史账号全量核验，不能用“当前邮箱未命中”反向证明没有历史账号；遗漏/变更未知时不批准JIT政策。管理员和普通账号使用相同原账号流程，JIT仅用于真正的新账号。

## 已完成验证与证据边界

最终源码在固定Linux工具链、全新隔离PostgreSQL中通过配置、生命周期、幂等、邮箱变化、源接管、错误sub、撤销、disabled拒绝、旧账号不降级、事务失败回滚、权限保护、12路竞争和24人/8并发样本；实际Handler首次开户、无Refresh、auth/me、SwitchTenant拒绝、退出与旧角色矩阵通过。迁移治理、vet及server构建通过。

独立注册服务测试证明：普通密码注册仍能bcrypt验证；外部认证专用账号不能密码登录，也不能创建个人空间。Frontend/Browser回归包含真实Vue+Mock SID的JIT零默认知识库权限、Portal、页面恢复、退出和授权拒绝。

同一完整提交的 [GitHub CI 38900142644](https://github.com/suoak/WeKnora/actions/runs/38900142644) Linux、PostgreSQL、Frontend、Browser四项全部成功，见 [验收收据](evidence/enterprise-jit-final-ci-20261016.json)。Windows本地Handler/仓储及调整初始页面等待后的Vue测试通过；完整service测试受到本机旧MinGW/DuckDB静态链接限制，以同源码Linux隔离与CI执行为最终证据，没有替换打包工具链或弱化验证。

真实SID新员工开户未执行；真实历史归属数据未导入；正式容量、政策、新JIT候选部署和003029现场迁移尚未批准。JIT无匹配普通账号进入Portal但没有已有知识库权限，不应为制造“可用”而自动加到敏感空间。
