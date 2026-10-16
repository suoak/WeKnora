# 全员扩展验收报告

日期：2026-10-16。最终代码提交：`09e6d86db731953834916e58ac5f3f5985ad0aa1`。

**最终 CI：PASS**。[GitHub Actions 38887398840](https://github.com/suoak/WeKnora/actions/runs/38887398840) 对该完整提交执行，Linux、PostgreSQL、Frontend、Browser 四项全部成功。机器可读结果见 [CI 收据](evidence/enterprise-all-final-ci-20261016.json)。

## 本地已完成

- 配置默认pilot、enterprise缺审批/新鲜度拒绝、JIT true拒绝。
- 可信归属dry-run无写入、同版本幂等、增量邮箱更新、稳定原user_id、停用拒绝、绑定撤销后拒绝。
- UID/员工标识转移、重复邮箱、主体冲突、过期/目录不可用拒绝。
- 1000/10000归属记录SQLite测试、1000/10000行CLI流式与脱敏测试；没有固定业务人数上限。
- SQLite规范化邮箱与身份查询索引实际EXPLAIN验证。

本地Windows/SQLite观测：1000条导入约2998ms，10000条约32792ms；1000次身份检查约84/99ms。是合成开发环境观测，不是生产吞吐SLA。增加规范化邮箱索引前同环境10000条约88758ms；不同运行有缓存/负载影响，不承诺固定提升比例。

## 最终 CI 已通过项目

- Linux race及既有OIDC/签名/nonce/state/Basic/Session/MCP/飞书回归。
- PostgreSQL1000/10000规模与增量、索引计划、首次并发唯一性、Session/Token停用撤销。
- Frontend类型检查、23项认证工具测试与构建。
- Browser：真实Vue + Mock SID，强制模式、试点普通SSO、enterprise普通SSO，Portal/auth/me/恢复/无旧Refresh/退出/拒绝。

最终 PostgreSQL CI 合成观测（race 开启，不作为生产 SLA）：1000 条导入 3232ms、10000 条导入 26313ms；对应 1000 次索引检查为 364/365ms。

12 路首次关联竞争通过，只产生一个身份绑定；停用增量在事务中撤销已签发企业 Session 与 Token。真实 Mock HTTP 登录覆盖 viewer、contributor、owner、租户管理员、组织管理员、系统管理员、跨空间管理员与多空间用户，核对原成员关系及管理标记、Access-only、SwitchTenant、退出和会话期限。

前序 CI 发现的租户测试夹具、时间精度夹具及迁移清单问题均已修正。没有放宽数据库外键、Token 安全期限或历史迁移不可变要求。最终结论仅引用上述成功提交与运行。

## 实测边界

原管理员/原知识库的读取及Mock关联在832独立副本已通过。新enterprise候选未部署，原业务真实账号在新模式 **NOT_REAL_DATA_VERIFIED**；本轮全部新归属/身份数据为合成，未导入真实员工数据，未执行真实SID登录。

真实来源未接入、审批未完成、JIT未实现、全员正式容量/外部目录SLA未验证。10000账号合成通过不等同于10000并发登录通过。并发测试只覆盖声明的首次绑定竞争数量，不能虚构生产压测结论。

当前试点仍832b6652a、3027，默认479416c90/1ba24ef5f/3027不变，真实身份及Session门禁关闭；不得提前启用全员模式。
