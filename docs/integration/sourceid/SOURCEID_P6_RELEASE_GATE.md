# 生产部署前安全检查与回退

当前没有发布批准。本轮不部署、不修改生产路由/数据库/用户绑定、不合并主分支。P6准备、CI成功和持有Client Secret均不等于READY_FOR_RELEASE。

## 开始真实测试前

1. 签署正式契约；全部示例/推测值移除；会话字段与SessionIndex等价证明、通知来源认证缺一即BLOCKED_EXTERNAL。
2. 确认隔离库/业务数据和测试实例的commit；批准callback进入该实例，单纯hosts映射不提供SID服务器访问路径，也不证明生产隔离。正式域名分流/替换必须单独提交方案批准。
3. 完成敏感日志门禁：后端敏感OIDC路由不采集body/query、代理不记录query/Referer/header/Cookie/body，APM/调试/error链路同样排除。用非业务synthetic marker验收所有日志，任何残留判FAIL。目前源码及默认代理存在风险，未修改/未通过此门禁。
4. 证明SID→网关来源真实性和网关→后端可信链路，覆盖并非转发内部secret头，后端不可绕过；应用直接peer CIDR准确、非全网或宽泛容器段。CIDR与随机secret只是本地门禁，不替代SID原生认证。
5. 受限secret注入、应用/DB/审计最小权限；不打印完整env、Compose展开配置、容器inspect、shell history、浏览器HAR。Agent具备同用户或管理员权限时ACL无法对Agent形成可靠保密边界，使用独立服务身份/外部secret manager与授权执行器；不把凭据文件放工作区。
6. 获得HTTP风险验收（授权码/cookie交换明文）；若不接受则停止，先新审批HTTPS URI，再同步固定callback校验、代理、Securecookie与SID注册，并重新验证。
7. 测试员工/原ID/权限已审核；映射dry-run与实际apply分别审批；应急本地管理员实测可用。首次真实员工登录与真实SID登出各自单独确认，不合并推定。

## 发布前额外门禁

- A–I真实测试及必要协议适配CI通过，保存环境/版本/审批/脱敏证据；执行网络失败、通知503/重试、重启后撤销验证，注明已运行流/离线客户端无法立刻终止的边界。
- 生产备份并演练恢复（users、auth_tokens、成员/共享/知识库ACL、SIDidentity/enrollment/session/logout/transaction、migration版本）；认证数据备份受限。
- 所有节点共享DB与受控JWT_SECRET，同版本前后端整体实例切换，禁止旧fragment前端与新cookie回调混用；明确回滚镜像/迁移版本/缓存策略。
- 配置变化需要重启/重建读取：OIDC/SID配置启动时加载，无已实现热加载；仅编辑env或docker compose restart不会给现有容器更新创建时的环境，须在批准窗口重建实例。Secret轮换/回退也同理；不显示展开配置。
- 上线监控只含请求ID、状态、耗时、通知认证结果及脱敏计数；设置通知失败/身份冲突告警、送达SLA与人工处置人；不以“未知index也200”掩盖大量通知未匹配。
- tombstone保留策略取得SID index不复用证据；不自动清理/恢复旧未撤销会话。审批人确认所有外部阻塞关闭，生产动作另行批准。

## 分层回退（待审批执行）

1. 保存当前配置版本/运行镜像和安全备份；暂停新的SID登录测试入口，确保本地应急入口存在。
2. 在批准环境、经DBA审阅后先撤销应处置的SID会话及其非空session_id令牌；恢复旧DB快照不得复活令牌。关闭 `SOURCEID_ENABLE`、`SOURCEID_LOGOUT_ENABLE`、`SOURCEID_CONTRACT_CONFIRMED` 和本部署 `OIDC_AUTH_ENABLE`。
3. 重启/重建受控实例使关闭配置生效。禁用开关不是令牌撤销操作；验证旧SIDaccess/refresh拒绝、其他本地账号/Key保持。
4. 回退旧二进制前必须撤销全部它可能忽略的非空session_id令牌，必要时全部SID会话；保留本地token/用户/成员/知识库。应用、前端、代理以兼容整体回退，清理旧前端缓存。
5. 优先保留新增表/列，仅配置/应用回退。确须003024 down时先备份和批准，down会撤销SIDtoken再删除SID表/列，审核映射/会话审计会丢失；安全归档后再执行。SQLite需支持DROP COLUMN。不得手动跳migration版本。
6. 错误绑定走 [审核映射回滚](SOURCEID_P6_ACCOUNT_MAPPING.md)，工具无自动解绑；精确ID与row_count审核，不批量重建users或邮箱重绑。
7. 恢复完整一致的数据状态同时保留撤销证据；验证本地管理员、旧SIDtoken拒绝、API Key/MCP/成员/知识库权限与备份基线一致，记录回退结果后再关闭事故。

以上均为审核用步骤，未在任何真实环境执行。既有 [回退方案](SOURCEID_OIDC_ROLLBACK.md) 继续适用。
