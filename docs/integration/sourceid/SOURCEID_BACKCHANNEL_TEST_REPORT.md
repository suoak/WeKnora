# SourceID 后端统一登出真实测试报告

状态：**NOT_TESTED / BLOCKED_EXTERNAL**。没有向真实SID发起登出，没有向真实KnowHub通知路径POST测试消息，未注销任何真实会话。批准地址仅静态核对，实际代理转发/来源真实性均未证明。

| 契约/结果 | 当前实现或证据 | 真实结果 |
|---|---|---|
| 注册URI/网络 | http://knowhub.ruijie.com.cn/api/v1/auth/oidc/backchannel-logout，POST路由静态匹配；本机TCP80可达 | NOT_TESTED（SID发送节点/运行handler） |
| 请求格式 | 单form-urlencoded body字段logoutRequest，无query；raw XML或显式base64+raw DEFLATE | BLOCKED_EXTERNAL（正式参数位置/编码） |
| SessionIndex来源 | 显式可信claim与配置issuer精确撤销 | BLOCKED_EXTERNAL（OIDC等价关系） |
| 通知认证 | 直接peerCIDR与内部网关secret；未原生XML验签 | BLOCKED_EXTERNAL（SID来源认证、网关信任/后端隔离） |
| ACK/重试 | 本地空200，失败403/400/415/503 | BLOCKED_EXTERNAL（SID正式送达契约） |
| 当前会话access/refresh撤销 | 持久化session与token行；派生会话同ID | NOT_TESTED（真实），已有Mock/DB验证 |
| SwitchTenant与重启后撤销 | 已有专项Mock/数据库CI | NOT_TESTED（真实） |
| 其他设备/用户保留 | 非通知index不撤销；同index多本地session一并撤销 | NOT_TESTED（真实SID设备范围） |
| 未知/重复/伪造消息 | 未知写tombstone，重复幂等，来源/格式门禁 | NOT_TESTED（实际入口）；H负例仅计划隔离执行 |
| 审计日志无凭据/XML | 发现通用body采集/默认代理日志风险 | NOT_TESTED/BLOCKED_LOCAL_GATE，不能填PASS |

执行前须Q07–Q13及日志门禁确认，用户另行批准真实SID退出，SID超时也需要批准测试配置/时间窗。按D/E/F/G/H逐项采集SID事件→来源验证→后端持久化→ACK→旧凭据拒绝的脱敏证据；只看到HTTP200不能判PASS。未知/伪造负例只在独立隔离/Mock环境使用非业务fixture，不触碰生产会话。

数据库不可用不能返回成功ACK；SID故障/丢通知时已登录会话不自动被实时撤销，需送达SLA/重试与批准处置。不能把本地session7天自然过期当SID超时通知通过。通知来源未确认时不开放公网、不忽略签名要求、不信未验证来源头。
