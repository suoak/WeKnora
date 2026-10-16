# 全员候选与回滚验收汇总

日期：2026-10-16。独立项目运行09e6d86db/Frontend832b6652a/DB3028；新JIT源码f40b1602f未部署。[最终同源码四项CI全部通过](https://github.com/suoak/WeKnora/actions/runs/38900142644)。正式采用一次性全员开放，此处独立测试不是线上分批灰度。

| 验收 | 结果与证据边界 |
|---|---|
| 不可变镜像、实际OCI revision、3028 clean | PASS；镜像及升级/最终收据 |
| 原账号、管理标记、成员与ACL、文件、有效分块/向量引用 | PASS；私有基线比较，未公开身份或内容 |
| 原管理员业务API及3份文档、向量召回 | PASS；可丢弃DB副本，未调用外部模型 |
| 真实存在的跨空间未授权KB及SwitchTenant | PASS，viewer返回403；不是随机不存在资源 |
| pilot兼容Mock | PASS；首次/再次同user_id、管理标记及成员不变 |
| enterprise_all Mock | PASS；同一账号路径、不依赖全员JSON，CLI dry-run/apply/replay均符合断言 |
| Access-only、SwitchTenant、退出、多设备隔离 | PASS；无Refresh响应/Cookie/DB行，切换不延长期限，退出Token401 |
| 停用同步与撤销 | PASS；Mock剩余设备Access即时拒绝；PG事务撤销Session及Token |
| 1000/10000归属与增量 | PASS；打包机PG 2932/20123ms，1000次索引检查302/296ms；非生产SLA |
| 12路首次绑定及唯一性 | PASS；一个身份、审核、enrollment，原权限不变 |
| 过期、未来核验时间、源/表不可用、邮箱回收及历史归属冲突 | PASS；新鲜度和冲突测试拒绝对应账号，无回退到不可信邮箱绑定 |
| 撤销后再登录 | PASS；墓碑不被源再次active或JIT覆盖 |
| APP-only回退832 / 保留3028 | PASS；实际回退健康，旧APP副本登录/KB/文档/向量/拒绝测试通过；随后恢复新候选 |
| 新JIT最小功能 | 同源码Linux隔离与Mock PASS；首开、幂等、零权限、旧账号不降级、回滚、旧角色矩阵、12路竞争、24人/8并发通过；正式CI另见最终收据 |
| Chrome无凭据试点路由 | PASS，4类请求均受控SSH隧道；日常SSO Chrome上下文尚未证明 |
| 入口/出站安全 | PASS，loopback18090、APP/PG不发布端口，未经批准bridge直连拒绝，SID公共Metadata/JWKS TLS通过；真实认证接口未调用 |
| 默认项目、原79库和原文件 | PASS，容器ID/默认DB3027、原79库及原文件不变 |
| 真实SID原账号、新员工JIT、所有真实角色 | NOT_EXECUTED / NOT_REAL_DATA_VERIFIED；本轮未授权 |
| 生产网络及容量 | NOT_VERIFIED；未访问生产，规模/峰值和生产规格待确认 |

## 本轮安全状态

干净试点库身份、审核、企业Session、归属及审计均0；有效Token/API Key及已启用外部连接器均0。真实OIDC关闭、protocol_only保持true；邮箱/自动绑定/SSO政策保持关闭，Session external、pilot默认、oauth_form及prompt=false未变。所有Mock账户、批准引用和会话只在可丢弃副本中，已删除。

新JIT使用003029，其代码、配置和正式政策与当前09e6d86db运行版本分开，不通过在旧版本设置开关冒充支持。

## 执行中问题与处理

首轮原数据副本脚本遗漏受保护postgres.env，文档/向量已通过但跨空间夹具中止；仅清理该组可丢弃数据库、补齐配置后重新完整通过，未触及干净目标库。初版镜像遗漏迁移资源，已补齐并核对同一编译二进制，最终验收使用dbaff4ef镜像。

JIT早期测试发现无用密码hash延长锁占用，已引入内部外部认证专用注册并复验普通密码注册未改变。Windows service静态链接有本机MinGW/DuckDB限制，实际Linux隔离与CI不以该失败冒充PASS；初始浏览器load等待超时经DOM就绪+实际按钮/Portal断言处理，本地Vue测试及CI Browser通过。这不证明已解决历史STATUS_BREAKPOINT，真实浏览器故障仍需未来授权事务证据。

## 证据

- [最终运行与完整性](evidence/enterprise-final-audit-20261016.json)
- [独立业务及Mock收据](evidence/enterprise-validation-20261016.json)
- [APP-only回滚](evidence/enterprise-rollback-20261016.json)
- [网络及SID公共连通性](evidence/enterprise-network-verify-20261016.json)
- [无凭据浏览器路由](evidence/enterprise-browser-route-20261016.json)
- [最终JIT源码Linux隔离测试](evidence/enterprise-jit-final-isolated-tests-20261016.json)
- [最终JIT源码CI](evidence/enterprise-jit-final-ci-20261016.json)
