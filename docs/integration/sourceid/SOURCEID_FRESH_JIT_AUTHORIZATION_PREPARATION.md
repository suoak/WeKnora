# 首次真实 JIT：授权准备核查

2026-10-18。前序提交 `d386237cd`。**本轮仅准备，未切换、未开启门禁、未执行真实认证。**
范围仍是同一SID身份在全新KnowHub实例首次开户，不是公司真实新入职员工全面准入验收。

## 当前环境：只读复核 PASS

| 检查 | 结果 |
|---|---|
| 原站点 | knowhub-company-test，APP5f988be51，Frontend832b6652a，DB3030 clean |
| 80/8080 | health均200，原环境标记company-test-verified-email-db3030；网关配置未改 |
| 原数据 | 用户3、绑定1、JIT0、文档3、有效分块/向量引用各2717；文件清单一致 |
| 原权限/审计 | 用户、租户成员、绑定审核、Session及关联决策基线一致；容器ID/镜像/挂载/端口未变 |
| 新实例 | knowhub-jit-fresh，已验收同一APP/Frontend镜像，独立DB3030 clean |
| 新库 | 用户、身份、绑定审核、ownership、block、JIT、Session、Token、空间成员、知识库均0 |
| 隔离 | 新数据库和Redis DNS指向新容器；新APP连接原PG/Redis的TCP探测被拒绝 |
| 文件与密钥 | 未挂载原文件卷；新库及JWT/AES独立，受控进程比较，不输出秘密/哈希 |
| 新门禁 | OIDC=false、protocol_only=true、contract=false、自动关联=false、JIT=false、Refresh=false |
| SID无凭据准备 | DNS/TLS通过，Discovery/JWKS200，无关出站被拒绝；未调用authorize/Token/UserInfo |

原环境完整备份仍在`knowhub-jit-fresh-5f988be51-20261018/private/original-backup`。
本轮未冻结原服务或另制在线快照；实际授权窗口启动前必须再做最新成套DB/文件及配置备份，
不能把前序备份称为未来执行时的最新备份。

## 批准记录：四项仍待明确确认

现有真实记录`SOURCEID_FIRST_REAL_BUSINESS_USER_APPROVAL_20261018.md`只批准原账号业务验收，
明确JIT关闭，不是此次开户或临时路由授权。本次用户消息明确称以下参数为候选，
因此不生成已批准政策引用，不填入任何人的签字、批准时间或维护时段。

| 批准项 | 候选内容 | 需归档的最小信息 |
|---|---|---|
| JIT政策 | enterprise_all + verified_email_policy；本人的同SID身份在空白新实例开户；精确域ruijie.com.cn；无部门/角色限制；普通tenantless、零默认授权 | 有权批准者及负责角色、明确批准、真实来源引用与时间、有效范围 |
| 会话政策 | 普通SSO，prompt=false；Access/Absolute/Idle/Admission为60/60/30/60分钟，Refresh=false；不等同实时停用复核或SID统一退出 | 风险接受负责人及明确批准；仅本次窗口，不冒称生产正式政策 |
| 入口维护 | 172.30.232.42原80/8080临时全部切至新实例，独占窗口，结束强制恢复原站点 | 维护责任人、实际开始/截止时间、短暂中断接受与恢复责任 |
| 单笔真实事务 | 本人一次点击并完成认证、创建1用户/绑定/JIT审计/Session、访问Portal/auth/me、退出；不自动重试 | 本人/测试负责人明确授权一笔；第二次真实登录另批 |

同一名有权负责人可以确认多项，但不能用账号角色、邮箱地址、审批候选参数或旧原账号授权
代替明确批准。不需要重新提供RJGH、ownership JSON或员工名单。域名批准只覆盖本次单域测试。

## 本轮新增的控制准备与验证

- `jit_fresh_window_control.py`为**不带真实部署适配器的控制契约**，默认只读，不包含SID客户端。
  要执行必须验证四项批准的来源、责任人和时效，再检查最新备份/恢复能力。
- `jit_fresh_window_control_test.py`：11项测试方法PASS（含各批准项、多个目标漂移和失败分支）。
  覆盖批准不全不变更、目标/隔离不符拒绝、先准备看门狗再配置、先排空再切换、无凭据验证后
  才READY；各准备失败均尝试恢复，不自动重试。证据保存、关闭新门禁、定向撤销失败不会跳过
  网关恢复；任何恢复步骤失败均报告MANUAL_RECOVERY_REQUIRED，不导出原始异常。
- 这些是控制顺序的Mock证据，**不等于真实部署适配器、在线看门狗或完整切换/恢复现场演练通过**。
  实际运行器尚未启动，不应通知用户点击登录。
- 新的未加载配置`private/route/candidate-observed.conf`已经用实际网关镜像nginx -t通过。
  复用已审核白名单，仅采集auth/me、logout、OIDC result/callback/config的固定类别与状态，
  不含完整URI/query、Cookie、Authorization或正文；APP/gateway raw日志不持久化。
  加入worker_shutdown_timeout=5s作为候选，避免只reload而遗留长期旧worker连接。
- 没有将新配置写入运行网关，没有连接网关到新网络，没有重建现有服务，没有打开任何新门禁。

## 获批后的执行检查点

1. 四项真实批准归档。重新核对两个实例及零有效授权事务，保存最新备份、源配置顺序和身份/权限
   基线。确认新库无Mock用户/连接；仅新环境禁用公开密码注册。
2. 在新环境受保护加载有效SID配置及获批政策，实测最终进程值；原环境保持原批准配置。
   启动针对新项目的单事务白名单观察及独立恢复看门狗，并先验证实际部署适配器；不能拿上面
   的纯Mock控制契约冒充在线采集器READY。
3. 在维护窗口内排空/短暂阻止其他请求。临时连接**网关**到新隔离网络，保留原网络供恢复。
   按原挂载inode写入候选、nginx -t后reload；不得替换成新inode导致容器读取旧文件。
   配置失败立即恢复，不手动忽略断言。
4. 无凭据核对原域名80页面/静态资源/API、8080/API、无参数callback、环境标记、新APP/DB/Redis
   指向及原资源隔离。新标记应为jit-fresh-isolated-db3030。SID callback不变，SID官方HTTPS不变。
   若无法证明路由或独占，不进入真实认证。
5. 所有检查通过后才通知本人点一次OA。人工等待最多8分钟且不超授权事务期限；密码页面只是
   中间步骤，等待本人完成并返回。Code90秒且只兑换一次。并行授权或协议异常停止、不重试。
6. 区分成功断言：新user_id不同于原管理员、用户总数1、tenant_id0、管理员及跨租户false、成员和
   知识库授权0、绑定/JIT审计各1、auth/me及Portal正常、无Refresh。不能以不存在资源404证明越权拒绝。
7. 退出后先保存证据，关闭新门禁，定向撤销唯一归属本次的新Session及Access；保留新用户/绑定/
   JIT审计。无法唯一归属时不批量撤销未知会话，报告并优先恢复原入口。
8. 恢复原网关文件、语法检查/reload、排空连接，确认80和8080均回原标记及原APP/DB/Redis；
   再断开仅本轮新连的网络。核对原管理员user_id、owner/角色/ACL、绑定、审计、文档及向量/文件。
   任一步失败告警并进入人工恢复。禁止新数据回灌原库、数据库降迁或删除原数据。

详细方案沿用[SOURCEID_FRESH_INSTANCE_JIT_READINESS_REPORT.md](SOURCEID_FRESH_INSTANCE_JIT_READINESS_REPORT.md)。
本次安全收据：`jit-fresh-authorization-preflight.json`、`jit-fresh-authorization-egress.json`、
`jit-fresh-isolation-preflight.json`、`jit-fresh-observer-config-prepare.json`（本机受控工作目录）。
实际切换、实际在线采集、真实JIT、真实退出、完整入口恢复演练仍为NOT_EXECUTED。
结论：**可以提交四项授权确认；现在不能开始真实登录。**
