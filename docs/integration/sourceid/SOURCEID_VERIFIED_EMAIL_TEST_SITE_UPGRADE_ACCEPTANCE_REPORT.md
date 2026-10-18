# verified_email_policy 测试站点最终升级验收

**PASS。** 北京时间2026-10-18，仅172.30.232.42、knowhub-company-test。本次唯一一次APP-only替换成功，未触发回滚；**没有执行任何schema迁移、镜像构建或拉取**。

| 项目 | 实际结果 |
|---|---|
| APP | `5f988be51779f1a22bd9d027b7503fe6a8749f62`，healthy；镜像及完整OCI revision一致 |
| APP不可变镜像 | `sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b` |
| Frontend | `832b6652adf541892545a7eac8b68733f846ab95`，原镜像/容器保留；与APP不同revision，未伪称同版本 |
| DB | **3030，dirty=false**，未切库，未运行up/down/force |
| 原数据 | **3用户、5成员关系、1有效KB、3文档、2717有效非空分块及2717 embedding引用**完整；原UID、管理员/跨空间标记、owner及空间成员、ACL、文档和向量引用元数据对照一致 |
| 文件 | 原配套文件卷逐文件长度及内容对照一致，没有改写或重建 |
| 80/8080 | 80登录页和静态资源200；8080登录路径401；两端health/config均200，无参数callback302且为invalid_state |
| 匿名业务访问 | auth/me、知识库、飞书管理员/个人及MCP探针两端均401，未修改鉴权逻辑 |
| 策略代码 | verified_email_policy已实际部署，**默认trusted_ownership、confirmed=false、policy_ref为空** |
| 身份与会话 | OIDC=false、protocol_only=true、contract/email/auto/JIT=false、Session external、Refresh=false、prompt=false；Basic oauth_form、AUTO_MIGRATE=false，未填虚假审批引用 |
| 身份写入 | external identity、ownership、JIT、企业Session、新政策decision/session及有效未消费授权事务均0；未发起真实SID认证 |
| 其他资源 | 除获准APP重建外，其他容器ID/镜像/挂载/端口、网络及Docker卷集合、防火墙规则均无实际变化 |
| 网关标记 | 完整检查通过后才更新为`company-test-verified-email-db3030`，随后重检路由、实际APP镜像及OCI revision通过 |

## 先校准，再消耗唯一部署机会

本次替换前完成以下无服务变更检查：

- Windows/Linux各7项控制测试，包括端口差异、回调安全分类、失败停止、资源集合及只允许APP改变ID/镜像。
- 使用第一次误报的实际401路由收据、第二次真实挂载误报分类、已保存部署前快照与现场回滚后快照离线重放；规范化比较通过。没有假称恢复已删除旧候选容器的完整inspect。本次成功部署的完整前/后资源快照均已另存受保护目录。
- 对真实资源快照做11类离线故障注入：容器ID、镜像、挂载源/目标/读写/删除、端口、未授权资源增加/删除、网络模式和地址变化，**全部正确拒绝**；挂载、端口绑定及网络别名仅顺序变化正确PASS。
- 所有比较逐项审查：配置按键值比较；数据行/文件清单规范化；端口按各自接口契约；资源集合检测增删；防火墙保留规则顺序语义，不随意排序放宽。忽略的是表示顺序，不是真实资源差异。
- 原f40/832/3030 clean、原数据/文件及全部18项路由探针只读预检通过；有效未消费认证事务、其他活跃DB查询及80/8080连接均0，随后才停止APP写入备份。

## 备份、部署与恢复准备

新受保护备份：`/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51/private/reliable-20261018T005053Z`。APP停止写入后生成DB3030 dump **21,717,702字节**及同代文件tar **2,570,240字节**，包含Compose顺序、受保护运行配置、原镜像/pin、网关及数据/文件/防火墙基线；旧备份未覆盖。

严格采用原基础`/home/aqyfzx/ai/knowhub-company-entry-20261017/green/compose.json`→既有`app-pin.json`，仅执行一次app的`up -d --no-deps --no-build --pull never --force-recreate`。Frontend、PG、Redis、DocReader及文件卷不重建；只reload现有nginx解析和批准的网关版本标记。

原f40镜像及`app-rollback-pin.json`仍可用。**本次成功未触发回滚**；此前两次现场APP-only回滚已验证可保留DB3030，不能把它说成本次又做了回滚演练。今后若需恢复，只回退APP及受保护配置，保留3030及审计/数据，不执行down。

现场服务端18项路由在标记更新前后均通过；Windows工作站绕过本进程代理直接访问172.30.232.42的18项同样通过。没有改变Chrome、hosts或全局网络配置，没有使用18090或隧道。未做真实用户登录、有效MCP Key/飞书授权或完整RAG；匿名401及数据完整性不冒充这些业务验收PASS。

独立最终复查再次确认完整镜像/OCI、Frontend版本、DB3030 clean、门禁及零身份写入；观察时APP运行98秒、healthy、restart_count=0。这是本次部署健康观察，不宣称长期稳定性或真实OA业务验收已经完成。

证据：`evidence/verified-email-reliable-*-20261018.json`；全部为白名单元数据、状态码及布尔结果，不含员工身份原值、Token、Code、Cookie或完整认证URL。

## 可以进入首次真实OA登录准备

技术部署门槛已满足，不再需要重复构建、迁移或重写登录功能。下一步仅需分别确认：

1. 公司级可信SID邮箱自动关联政策及真实引用，覆盖所有角色、已知冲突拒绝和未记录历史邮箱转让残余风险接受；不要求逐人ownership JSON。
2. 普通SSO、Access-only本地Session政策及期限（60/60/30/60分钟仍待批准）、退出/撤销和风险责任人。
3. A正向验收范围及**一笔**本人真实SID登录、原账号关联和企业Session创建窗口授权；JIT保持关闭，B跨空间负向未执行不标PASS。

当前仍禁止真实SID认证和身份绑定，全部门禁保持关闭。完成部署验收后停止，等待上述批准。
