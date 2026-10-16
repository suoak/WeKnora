# 试点到全员的发布与回滚操作卡

当前：默认项目不变；独立试点832b6652a、DB3027、真实身份门禁关闭。全员候选尚未部署，003028未在运行数据库执行。

## 迁移与候选验证（另行批准后）

1. 仅在独立试点/一次性干净副本备份DB、受保护配置和同代文件，核对原账号/角色/成员/ACL私有基线。不得混用有Mock身份的副本。
2. 单独审核3027→3028增量迁移：只新增证据/审计和索引，不更改users及权限；建立邮箱索引时需维护窗口，保留原库/默认库不变。
3. 使用固定候选APP镜像，只升级获批准独立项目；Frontend的Access-only兼容代码来自832b6652a，未修改前端协议。本轮没有新候选镜像构建或部署授权执行记录，不伪造镜像ID。
4. 先保持pilot范围、protocol_only=true及所有绑定门禁关闭，完成数据库兼容、原管理员/知识库的新候选隔离Mock验证；当前原数据证明来自832版，不能直接换算成新模式真实PASS。

## 全员配置差异

继续普通SSO、oauth_form、prompt=false、authorization_recheck=false、无Refresh、SID统一登出关闭，保留所有OIDC校验。

目标新增：`SOURCEID_ACCOUNT_SCOPE=enterprise_all`、`SOURCEID_ENTERPRISE_POLICY_CONFIRMED=true`、真实 `SOURCEID_ENTERPRISE_POLICY_REF`、经批准 `SOURCEID_ENTERPRISE_MAX_AGE`。不需要SOURCEID_PILOT_ACCOUNTS_FILE，无固定全员账号数上限。

原有放行配置仍必须独立满足：oidc_sso_approved、local_approved、SSO_POLICY_CONFIRMED、CONTRACT_CONFIRMED、可信邮箱合同/自动绑定审批、LOCAL/ADMISSION/AUTO_BINDING政策引用和明确期限。取得批准才将PROTOCOL_ONLY=false、邮箱及自动绑定开启。不能简单所有开关true。

60m Access/绝对、30m空闲/60m Admission仍为候选，需正式批准；目录max_age另行确定，不能把目录缓存年龄当作SID实时在职校验。SSO续接、同步传播延迟、90秒Code等残余风险由统一负责人确认，管理员不逐人额外审批。

## 批量归属与真实验收

数据负责人通过受保护渠道交付可信历史归属批次，先dry-run再经批准apply。工具自动join原user_id、同版本幂等、冲突汇总；没有可信证据者统一进入异常，不阻塞其他正常账号。不得从当次登录反向生成历史证明。

先限制独立环境入口和受控窗口，在批准的真实账号中覆盖实际存在的普通/owner/系统/跨空间/多空间角色；同一账户可覆盖组合。没有真实数据者NOT_REAL_DATA_VERIFIED；不能由合成样本宣称全事业部真实验收。

由本人点击OA并完成认证；服务端关联统一run_id，验证Portal、auth/me原user_id、原KB读取及真实未授权资源拒绝、SwitchTenant、无Refresh、退出、到期、定向撤销。真实登录、再次登录和其他账号分别纳入明确授权，不自动扩大次数。

## 快速止血与回滚

1. 关闭真实自动绑定、恢复PROTOCOL_ONLY，停止新企业Session创建，保留证据。
2. 定向撤销本轮企业Session/Token；保留身份、归属、审核、审计，不以恢复旧dump抹除写入。
3. 模式回pilot前恢复经过批准的试点名单并核验路径/权限；轮换本地政策引用或撤销原enterprise Session，避免旧Session跨策略复用。pilot的100上限仍是灰度边界，不是正式人数限制。
4. 应用回832旧镜像时优先保留新增3028表和索引，禁用全员配置，不降迁、不删卷；旧应用不使用新表。回退前在隔离副本验证启动/本地登录/API兼容。
5. 数据库down只用于单独批准的隔离演练，存在审核数据时不直接执行。新增迁移down会保守撤销受影响issuer/user的SourceID Session，不动密码登录Token。

网络SID出站是试点独立网关控制，应用回退不得删除默认项目规则/配置；回退网关时用受保护原Compose、重建试点APP、移除试点网关别名，再reload试点Nginx，避免旧DNS残留。任何操作不触及生产服务器或默认80/8080项目。
