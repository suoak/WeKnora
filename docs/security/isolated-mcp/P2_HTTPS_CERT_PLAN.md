# 生产 HTTPS 与证书方案：待拓扑确认

目标仅为 `https://knowhub.ruijie.com.cn:8443/api/v1/mcp/personal`。既有 HTTP 网站及 `http://10.51.134.114/api/v1/mcp/personal` 不切换、不重定向。本轮没有安装、复制、转换或上传正式证书及私钥。

## 已核验的本地证书事实

用户给定的 `E:\ruijie.com.cn-20260721颁发\_.ruijie.com.cn_nginx.tar` 含 PEM 叶证书加 GlobalSign RSA OV SSL CA 2018 中间证书及匹配的 PEM 私钥。SAN `*.ruijie.com.cn` 覆盖 `knowhub.ruijie.com.cn`；有效期为北京时间 2026-07-21 09:53:26 至 2027-02-05 09:53:26，RSA 2048/SHA-256。P0 在内存核验叶/中间签名、Windows ROOT 内 GlobalSign Root CA - R3 信任及证钥公钥匹配，没有输出私钥。完整证书审计、局限及原始归档清单见 [P0审计](P0_AUDIT_2026-11-10.md)。P1 的 Nginx 1.30.3 测试镜像支持 SSL，但不能代表生产前端镜像能力。

到生产上线时必须重新检查证书是否仍有效、完整链、私钥匹配、终端算法兼容和客户端实际信任；本地归档有效不等于生产 TLS 已安装或已配置。

## 根据实际拓扑择一

| 生产现场事实 | 终止位置与发布方式 | 仍须确认 |
|---|---|---|
| 已批准边界代理承载隔离 VIP，具备 L7 精确路径/方法/媒体 ACL、证书受控托管和受保护后端通道 | 在该代理独立终止8443，证书只由边界证书流程安装；边界把精确外部 URI 固定改写到已核实 APP upstream 的 `/internal/mcp/isolated/personal`。无需在前端发布8443。 | 代理原始 URI 匹配、固定后端路径、Host/SNI、后端 TLS/身份、日志脱敏、并发限速和 WAF 保留策略 |
| 边界仅四层透传，已核实既有前端 Nginx 是实际入口且允许独立监听 | 保留现有80，前端新增受控地址的8443监听；证书/Key以只读挂载注入，单独 server 默认拒绝其他 URL；固定改写到已核实 APP upstream 的 `/internal/mcp/isolated/personal`。 | 实际前端镜像 SSL 模块、Compose 端口映射和容器重建窗口、回滚旧镜像/配置、Nginx `-t` |
| 设备能力或实际链路不明 | **不选终止点，不安装证书，不开放8443。** | 网络/运维共同补证并批准设计 |

正式证书目标目录由实际托管团队确认。若在前端主机，可提议受控目录 `/etc/knowhub/tls/isolated-mcp/`，目录0700、私钥0600，容器仅只读挂载；实际 UID/GID/SELinux 权限通过测试验证。配置中只写证书路径，绝不存明文私钥或密码。不要把私钥放 Git、镜像构建上下文、测试制品、环境变量、普通日志或聊天。PFX/JKS 归档不供 Nginx 直接使用，不在本任务解密。

正式配置由生产拓扑和 [测试 Nginx 模板](p1_nginx.conf.template) 重新渲染：移除测试过期证书端口18443与环回地址，替换获批 VIP/源 ACL/只读证书挂载及真实 upstream；保留精确路径、GET/POST/DELETE、JSON媒体/1MiB、无缓存/重放/正文日志及流式兼容。不能将 `proxy_pass` 指向整个 REST 站点或在 URL 上作宽泛前缀转发。若边界已终止 TLS，后端代理仍按实际可信身份和网络边界限制，避免直接向隔离区开放 APP8080。

生产窗口前由运维输出脱敏配置 diff、证书公开指纹、`nginx -t`、镜像/配置备份及回滚操作记录；生产证书私钥只走批准的安全流程。部署后从真实隔离客户端验证 SNI/SAN/链、TLS版本、精确 MCP 路径和非 MCP 默认拒绝，随后核对旧 HTTP 网站与内网 MCP。任一证书/链、权限或内网回归失败即按 [P2 回滚方案](P2_DEPLOYMENT_AND_ROLLBACK.md) 关闭新入口。
