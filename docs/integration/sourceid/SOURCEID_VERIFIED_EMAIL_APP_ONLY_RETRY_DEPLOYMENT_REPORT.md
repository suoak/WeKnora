> ??????????????5f988be51/832b6652a/DB3030?????????[????????](SOURCEID_VERIFIED_EMAIL_TEST_SITE_UPGRADE_ACCEPTANCE_REPORT.md)?

# verified_email_policy APP-only重试实际记录

北京时间2026-10-18。仅172.30.232.42、knowhub-company-test；本次只替换APP一次，**零schema迁移命令**。最终结果：**再次被验收脚本误报触发回滚，未完成目标APP发布。**

| 用户要求 | 当前真实状态 |
|---|---|
| APP是否运行5f988be51 | **否**。目标APP曾健康启动、不可变镜像校验及全部接口检查通过，之后因下述误报自动回滚 |
| 当前APP | f40b1602f69ce5d88e1e87dc3cefae36e9bde356，镜像`sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413` |
| Frontend | 832b6652adf541892545a7eac8b68733f846ab95，原容器、镜像不变 |
| 数据库 | **3030 clean**；无up/down/force、无切库 |
| 原数据 | 3用户、5成员关系、1有效知识库、3文档、2717有效非空分块及2717 embedding引用保持一致；UID、管理员/owner、空间成员、ACL、文件内容对照通过 |
| 80/8080 | 正常；80登录页及静态资源200，8080登录路径401；两端health/config均200，无参数callback302及`invalid_state` |
| 匿名访问 | auth/me、知识库、飞书管理员/个人和MCP接口均401；没有放宽认证来迎合断言 |
| verified_email_policy | 新APP代码**尚未保持运行**；trusted_ownership、confirmed=false、空政策引用，真实业务门禁全部关闭 |
| 回滚 | **本次实际APP-only回滚及回滚后数据/接口核对通过**；DB3030保留，网关版本标记仍为company-test-f40-db3030 |

## 唯一一次重试及真实停止原因

替换前，Windows/Linux各5项合成验收用例及现站18项路由探针通过，包括端口差异、回调安全分类、异常状态码及失败即停止。本次修正了前次8080/login误判；没有修改后端认证代码。

新建受保护备份后，只用基础Compose→既有app-pin.json执行一次`up -d --no-deps --no-build --pull never --force-recreate app`。目标镜像`sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b`，OCI revision `5f988be51779f1a22bd9d027b7503fe6a8749f62`；运行配置与升级前一致，APP健康、18项80/8080路由和原数据/文件对照均通过。

随后，脚本直接按数组顺序比较其他容器的Docker挂载清单，触发断言失败。只读定位发现两个无关测试容器仅`.Mounts`返回顺序不同；**容器ID、镜像、端口和全部挂载内容均未变化**。这是部署检查器的第二个错误，不能解释成候选APP故障，也不能解释成其他服务被重建。

脚本已自动恢复f40，保持DB3030及全部文件。之后仅只读检查和校准，**未进行第二次部署**。网关新版本标记在所有检查通过前不会修改，本次未到达该步骤，没有用新标记冒充发布成功。

停止后将挂载/端口清单改为按内容规范化比较，并补充资源ID、镜像、挂载源/目的地/读写、挂载删除及端口暴露变化的拒绝测试。Windows/Linux各6项测试通过，现场其他容器语义比较通过。**这些新增校准发生在回滚后，没有重新执行部署。**

## 备份与安全状态

本次新备份目录：`/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51/private/retry-20261018T001316Z`。在APP停止写入后成套保存DB3030 dump（21,717,702字节）、文件tar（2,570,240字节）、受保护配置、Compose顺序、旧镜像/pin、容器和数据/文件/防火墙基线。前次备份未覆盖。

最终核查：OIDC=false、protocol_only=true、contract/email/auto/JIT=false、Session external、Refresh=false、prompt=false、AUTO_MIGRATE=false、Basic oauth_form、confirmed=false；无新增绑定、JIT、ownership、企业Session或新政策Session。其他容器/卷/端口及防火墙规则与新备份前一致。无真实SID请求、身份写入或生产操作。

未执行真实密码登录、SID认证、有效MCP Key/飞书业务授权或完整RAG；401仅证明匿名接口仍受保护，不能当作完整业务验收。

## 下一步状态

**首次真实OA业务登录仍为NO-GO**：本轮目标APP没有保持运行。需要另行明确允许使用已修正容器清单比较器再次执行APP-only替换；不得重新迁移DB3030。本轮已停止。

候选实际发布成功后，仍需公司可信邮箱自动关联政策及真实引用、Access-only会话期限与风险接受、A正向验收范围及一笔真实SID/关联/Session写入窗口分别批准；JIT继续关闭。不需要逐人ownership JSON，不能将候选60/60/30/60分钟视为已批准。

脱敏证据：`evidence/verified-email-retry-*-20261018.json`。没有保存Cookie、Code、Token、完整认证URL或员工身份原值。
