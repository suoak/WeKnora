# 打包机 OIDC 持续开启状态

2026-10-14 北京时间 20:00，按用户最新授权开启。此记录更新此前部署验收结束时的关闭状态；旧报告及旧 JSON 保留为历史快照。

- 主机仅为 172.30.232.42；业务版本 3be5d3f9268fe8819d66485a2b990c6237827e25，frontend 80 / app 8080。
- Compose environment 持久设置 OIDC_AUTH_ENABLE=true、SOURCEID_DIAGNOSTICS_ENABLE=true；重启后仍生效，优先于 mode.env。程序默认值和原诊断限额没有修改。
- SOURCEID_PROTOCOL_ONLY=true、SOURCEID_CONTRACT_CONFIRMED=false、SOURCEID_LOGOUT_ENABLE=false，继续禁止未经审核的企业身份写入和 JWT 签发。
- 已核对正式 Issuer/Discovery、Client ID、批准 callback、Basic、无 PKCE、openid 和 Bearer GET；Mock 未运行，Mock CA/SSRF 白名单为空。
- Secret 只由服务进程从既有受保护配置加载。只检查非空布尔状态，不读取或输出值，不宣称其有效性已通过 SID 认证。
- 公开 OIDC config 实测 enabled=true；Windows 本地域名解析为 172.30.232.42，带 knowhub Host 的配置请求也返回 200。生产服务器未访问。
- app/frontend 已重建加载开关，PostgreSQL、Redis、DocReader 容器不变，未操作数据库。

现在使用 `http://knowhub.ruijie.com.cn/login`，由用户手动点击“企业 SID”。不要用 IP 地址开始此次认证，避免与批准 callback 的浏览器 Cookie 来源不一致。第一次测试只点击一次，失败后不要反复尝试；后端会自动记录三阶段脱敏诊断，不需要 HAR 或完整 callback 地址。

诊断开关持续开启，但单进程输出仍受首次事件起 30 分钟、10 MiB 上限约束。到达限额不会关闭 OIDC，也不会放宽安全验证。新真实事务尚未由 Agent 发起或观察；用户完成后再提取本笔脱敏结果供 SID 管理员核查。

配置变更前备份目录：`/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05/upgrade-backups/persistent-oidc-20261014T120006Z`。如需恢复此前关闭状态，可复制该目录 compose.disabled.yml 到部署目录 compose.yml，再只重建 app/frontend；本轮没有执行恢复。

证据：[persistent-oidc-enabled-20261014.json](evidence/persistent-oidc-enabled-20261014.json)。
