# 打包机飞书与 SID 阶段 B 联合部署报告

2026-10-15；状态：**联合代码已部署，企业身份自动绑定尚未放行**。

仅操作 `172.30.232.42`。没有访问生产服务器，没有新的 SID 员工登录、飞书真实授权或真实账号关联。

## 版本与验证

- 组合 Commit：`2fcbcaaedfa75fb955bd44dd7fe464ded64eec7c`，包含 SID 阶段 B `523d7ab49` 与飞书个人文档导入代码。不混入工作区未验收的 MCP 变更。
- [GitHub CI 38690335917](https://github.com/suoak/WeKnora/actions/runs/38690335917)：Linux、PostgreSQL、frontend 全部成功。
- 联合专项测试 197 PASS；认证专项 232 PASS/6 SKIP；完整回归 4,764 PASS/18 SKIP。这里包括子测试计数，不将跳过项算作通过。PostgreSQL 独立 job 5 项通过，包括真实 PostgreSQL 并发绑定及 `3024 → 3025 → 3026` 迁移链。
- 前端类型检查、认证/失败回调测试及构建通过。本机继续使用原 Go 1.26.7、Node 24.20.0、AnyDoc/DuckDB 和离线依赖基线，无新增依赖版本或系统运行库升级。
- 源码归档 SHA256：`1b1bbb76d79f320a2ed259b7ab8f1acc21e319b9c5fb6ba11aef0203240cc1be`。镜像按精确 Git 归档构建，不使用脏工作区。

## 实际部署

目录：`/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05`；Compose 项目 `knowhub-sourceid-business`。

|服务|运行镜像|镜像 ID|端口|
|---|---|---|---|
|app|knowhub-sourceid-business/app:2fcbcaaed|sha256:93f5c831b6e061832d71aa19fe74835c78e16b637d77e7ce18a4283e9ed62daa|0.0.0.0:8080|
|frontend|knowhub-sourceid-business/frontend:2fcbcaaed|sha256:4dc1ca5eacbb723000d069e947da4876e7a479f3e95ea5cea4c234a9c295dcba|0.0.0.0:80|

两个镜像的 revision 标签均核对为完整组合 SHA。app 容器 `03b06efdafe2`，frontend `dcbeecba4f42`。均运行且 restart policy 为 unless-stopped。

PostgreSQL、Redis、DocReader 以及其他原有运行容器 ID 保持不变。没有删除数据库、数据卷、旧镜像或历史文件，没有执行 prune/down -v。旧 app/frontend `468410e89` 保留。

## 数据库与备份

- 实际迁移：`3024|false → 3026|false`。完整包含 003025 飞书和 003026 SID，没有 Force 或跳版本。
- 使用镜像内原标准 migrate CLI，以单次容器执行两个待办迁移。数据库凭据仅由进程从现有受保护配置加载，没有输出。没有启动应用的其他初始化迁移钩子；默认服务 `AUTO_MIGRATE=false` 始终保留。
- 升级前备份目录：`upgrade-backups/combined-feishu-sid-20261015T062216Z`。
- 数据库备份 `database-before.dump`：355,255 字节，0600，SHA256 `c1e79a79850826495c18c9a82fad120ebd61fb7fd9f03c3df3ccb17568a9c202`。Compose、模式配置和旧镜像也已保留。
- 已在一次性数据库实际恢复备份，并用新镜像完成 3024→3026 演练。首次使用默认模板时遇到 ParadeDB 自带 schema 冲突，在停止原服务之前发现；改用 template0 后恢复及迁移均通过。一次性校验库已清理，原库未重建。
- 迁移前后：用户 1、租户成员 1、历史知识库行 2、历史知识行 2、SID enrollment 1；外部身份/SID session/审核/审计均 0。没有身份写入或账号迁移。
- 备份复核显示升级前有效知识库与知识条目均为 0，原有两行均为历史软删除记录。部署验收创建并清理了专用合成知识库/草稿，可能增加历史软删除行；不宣称这些是原测试知识库迁移或恢复的数据。

## 实际验收

|检查|结果|
|---|---|
|80 登录页、前端健康代理、8080 health|PASS|
|已有隔离合成本地账号登录、auth/me、业务 API|PASS，HTTP 200|
|知识库创建、手工草稿创建/读取与清理|PASS|
|MCP 初始化、tools/list、list_knowledge_bases 调用及清理|PASS|
|本地退出及旧本地 Token 拒绝|PASS，退出后 auth/me 401；不是 SID Token 401|
|DocReader ListEngines 与 DOCX 合成解析|PASS；TXT 未在该组件声明的支持列表内，不以 TXT 拒绝判为回归|
|飞书用户状态接口、知识库导入路由|PASS；应用未配置时 configured/available/connected 均 false|
|非系统管理员访问飞书管理接口|PASS，403|
|飞书未配置时发起授权|PASS，400，未向飞书发起真实授权|
|SID callback 进入业务 Handler|PASS，合成无有效 state 请求返回 302/invalid_state|
|app/frontend 合成敏感值、密码、完整授权头/JWT 日志扫描|PASS，未发现测试 canary 或这些凭据格式|
|浏览器本地登录、飞书菜单及未配置提示|PASS；普通本地登录进入 Portal，再进入文档页，通过键盘展开菜单，显示联系管理员配置提示|
|浏览器失败回调|PASS，invalid_state 返回登录页，Portal 业务 API 请求数 0，观察到的网络对端全部为打包机|

合成阶段 B 测试覆盖首次可信邮箱匹配、已绑定 RJGH 登录、邮箱变化、无匹配/重复/停用/高权限拒绝、撤销与会话失效、并发首次登录及原账号保留。测试在独立夹具中进行，不用当前 owner 账号冒充普通员工，不启用默认服务的真实自动绑定。

浏览器验收脚本最初选择了错误的提交按钮/登录落点，之后明确使用本地密码框 Enter 和正常 Portal 落点；合成知识库启用了 Wiki，需要切换文档 tab 才能看到导入菜单。鼠标自动化曾有指针拦截，最终以键盘展开菜单完成验收；不将这项结果声称为已解决鼠标交互或原 STATUS_BREAKPOINT 问题。最终浏览器检查没有观察到崩溃，专用上下文已关闭。

最后核对：用户/租户成员仍为 1/1；知识库历史行 8、知识历史行 3，全部处于软删除状态，包括原有历史行和本轮清理的合成夹具。有效知识库与知识条目均为 0，与升级前备份一致。SID 绑定/会话/审核/审计与飞书授权/导入记录均为 0。再次扫描两个新容器日志，合成凭据、code/state canary 和完整授权头/JWT 格式均未出现；临时迁移容器已退出，其他原有容器 ID 仍一致。

机器可读证据：`evidence/feishu-sourceid-combined-deployment-20261015.json`。

## 最终安全配置

```text
SOURCEID_PROTOCOL_ONLY=true
SOURCEID_CONTRACT_CONFIRMED=false
SOURCEID_EMAIL_BINDING_ENABLE=false
SOURCEID_AUTO_BINDING_ENABLE=false
SOURCEID_EMAIL_CONTRACT_CONFIRMED=false
SOURCEID_LOGOUT_ENABLE=false
SOURCEID_BASIC_ENCODING=raw
OIDC_AUTH_CLIENT_AUTH_METHOD=client_secret_basic
OIDC_AUTH_PKCE_ENABLE=false
OIDC_AUTH_SCOPES=openid
OIDC_AUTH_USER_INFO_TRANSPORT=bearer_get
AUTO_MIGRATE=false
```

保留用户此前要求长期开启的 `OIDC_AUTH_ENABLE=true` 和现有 `SOURCEID_DIAGNOSTICS_ENABLE=true`，不以这两个开关代表阶段 B 放行。Discovery、Issuer、已审批 callback 均保留原值，凭据未读取、轮换或输出。诊断仍使用已有受保护、限量的脱敏机制。

飞书应用当前未配置；加密密钥仅做长度合规布尔检查，结果合规，没有更换。真正个人授权、文档导入和权限覆盖需系统管理员配置应用并另行验收。

## 回滚

受保护回滚脚本（已通过 bash -n）：

```bash
bash /home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/upgrade-backups/combined-feishu-sid-20261015T062216Z/rollback.sh
```

恢复原 Compose 与旧 app/frontend 镜像，保留新增表及 schema 3026，AUTO_MIGRATE=false；不执行 down 或自动覆盖数据库。003025 down 会删除飞书授权、导入及设置，003026 down 会删除审核/审计，不作为常规运行库回滚步骤。

已验证数据库恢复和副本迁移、CI 夹具迁移 down、回滚脚本语法和旧镜像留存。此次升级成功，**没有在运行默认服务上实际执行旧镜像回退**，不将其标为已演练通过。

## 尚未完成

员工状态字段/允许值、RJGH 稳定性、会话与统一登出契约仍需 SID 确认。真实企业自动绑定、此次部署后的 SID 员工登录、飞书真实个人授权和真实文档导入均 NOT_TESTED。此前一笔 ID Token 失败不能由本次合成验证宣称修复；STATUS_BREAKPOINT 也需区分本次观察与原先真实事务，不能宣称已根治。

本版本可继续在打包机做受控验收，不是生产发布许可或 READY_FOR_RELEASE。
