# 全员候选独立环境升级报告

日期：2026-10-16。结论：**09e6d86db 独立环境升级、003028 迁移和 APP-only 回滚验收通过**。生产不采用分批灰度；此独立项目仅用于正式一次性全员发布前验证。

## 实际运行身份

| 项目 | 实际结果 |
|---|---|
| 服务器 / Compose | 172.30.232.42 / knowhub-oa-sso-pilot |
| APP OCI revision | 09e6d86db731953834916e58ac5f3f5985ad0aa1 |
| APP 不可变 image ID | sha256:dbaff4ef55df5759c30e3535bf8be89882bd7ac74f1010d96875b7a3dbb21a6e |
| Frontend revision（未重建） | 832b6652adf541892545a7eac8b68733f846ab95 |
| Frontend image ID | sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1 |
| 数据库 | knowhub_oa_sso_pilot；3028、dirty=false |
| 文件卷 | knowhub-oa-sso-pilot-files；APP 只读挂载 |
| 入口 | 服务器 127.0.0.1:18090；APP/PG/Redis 不发布主机端口 |

源码由 git archive 导出精确提交；构建源包 SHA256：380cafb905b2df85f06c7a16e3d2378eec30c09598aa7163973907a2f9bb72de。使用既有离线工具链 Go1.26.7、固定 builder sha256:2b63ff41b02b21d7b302c2d3ba6b4ab43990181fc7d2221585d2cfa5161553fb、缓存依赖及原生库，构建 network=none。CI 为同源码 Go1.26.8 四项通过，不能把两种编译器版本写成相同。

镜像包含 APP、sourceid-ownership CLI 及实际 003028 迁移文件。初次封装只替换二进制，随后补齐迁移资源；受控比较确认编译二进制相同，最终部署与回归使用上表最终 image ID。初始镜像收据保留，不将其 ID 当作最终运行镜像。

## 备份与迁移

备份目录：`/home/aqyfzx/ai/knowhub-enterprise-candidate-09e6d86db/private/before-upgrade`，0700；配置、数据库及文件备份 0600。冻结独立 APP 写入后，生成最新 database3027.dump（21678166 字节）、files.tar（2570240 字节）、Compose、环境与容器元数据备份，比较数据和文件前后不变。

标准 migrate 使用候选源码迁移目录执行 `up 1`，仅从 3027 到 3028；没有强制改版本、dirty 恢复、降迁、删卷或切库。003028 仅新增可信归属/审计表、唯一约束及规范化邮箱非唯一索引。数据库与原权限内容一致，索引 valid/ready。

独立 APP 采用两层 Compose，顺序为：原试点 compose.json，最后 enterprise-app-pin.json。仅执行 APP 的 `up --no-deps --no-build --pull never --force-recreate`。试点 Nginx 只 reload，不重建。所有其他试点容器及默认项目容器 ID 均保持。

## 安全及验收

最终 APP healthy，/login、/health、OIDC config 均 200；OIDC_AUTH_ENABLE=false，PROTOCOL_ONLY=true，CONTRACT_CONFIRMED=false，EMAIL_BINDING/AUTO_BINDING/SSO_POLICY_CONFIRMED=false；Session 保持 external，ACCOUNT_SCOPE 未配置即 pilot，JIT 未配置即关闭。oauth_form、prompt=false、Refresh=false、AUTO_MIGRATE=false 保持原值；app.env 与升级前逐字节一致。

原账号与知识库、pilot/enterprise_all Mock、Access-only、SwitchTenant、退出、停用撤销、规模及索引通过，详见数据及验收报告。干净试点 identities/reviews/sessions/ownership/audits/有效 Token/API Key 计数均为零，未导入真实身份或 Mock 绑定。

SID DNS/TLS、Discovery/JWKS 公共接口通过，未调用真实 authorize/Token/UserInfo。APP 非必要外网及默认服务80/8080出站被拒绝，未经批准 bridge 来源直连 APP/PG 被拒绝。无凭据临时 Chrome 的页面/资源/API/空参数 callback 经 SSH 同源隧道进入试点；结束关闭临时浏览器/隧道，不修改日常 Chrome 或全局 DNS/代理。此检查不证明日常 SSO Chrome 已就绪。

## 回滚实际结果

实际 APP 回退至 832b6652a，DB 保持 3028，健康及代理通过；旧 APP 在 DB3028 可丢弃副本完成原管理员登录、文档读取、向量召回和越权拒绝。随后恢复本次候选，未降迁或恢复旧 dump，其他容器及配置未变化。

回滚 pin：`enterprise-app-rollback-pin.json`；当前 pin：`enterprise-app-pin.json`，均位于上述候选目录。严重身份问题先关闭业务门禁并定向撤销 Session/Token，再仅 APP 回退；保留归属、绑定、审计及新增表，不抹除新业务写入。

默认80/8080项目 APP479416c90 / Frontend1ba24ef5f / DB3027 clean 未变；原业务库仍79 clean、原文件未变。未操作生产服务器。后续 JIT 是独立源码候选，本次运行 APP 不包含 JIT 新实现，003029 未在试点执行。
