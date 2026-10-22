# 统一个人 Go MCP、现有入口与隔离只读网络

## 实际代码与 URL

实际路径为 **`/api/v1/mcp/personal`**，注册于 `router.RegisterPersonalMCPRoutes`；方法为 GET、POST、DELETE，传输为现有 mcp-go 的 Stateless Streamable HTTP。它随 KnowHub APP 路由启动，不单独 Listen，不启动额外 Go MCP 进程。

Current APP composition uses `NewConfiguredPersonalServer` and `NewPortalPersonalKnowledge`. Missing enabled/PolicyReference/dependencies keeps the service disabled. No default configuration or production entry has been enabled; no new backend listener is required.

员工最终配置应指向 `https://<KnowHub现有Web域名>/api/v1/mcp/personal`；本文没有猜测一个已开通的新域名。仓库 Compose 将 frontend `${FRONTEND_PORT:-80}:80` 暴露，Nginx `/api/` 转发至 `${APP_SCHEME}://${APP_HOST}:${APP_PORT}/api/`，APP_BACKEND_PORT 默认为 8080。443 应由当前实际 TLS 入口终止；本批未声称开通或验收生产 443/隔离网络。

因此普通内网入口无需新增 Docker Compose 服务、后端端口或一套 MCP。原 `/api/` 规则可承载当前路由。上线前可单独为该精确路径增加关闭 POST 重试、无缓冲/无缓存等配置审阅；现有通用代理已有 HTTP/1.1、关闭 buffering/cache、长超时，但有通用 upstream retry 设置。当前没有修改这些生产文件。

源码：`internal/router/routes_personal_mcp.go`、`internal/mcpserver/personal.go`、`internal/middleware/personal_mcp_auth.go`、`frontend/nginx.conf`、`frontend/nginx-api-proxy.conf`、`docker-compose.yml`。本批没有改动后五项现有入口/部署实现。

## 同一个 Key、身份与检索

内网和隔离访问使用同一个 `user_mcp` Key、同一个 OwnerUserID、原范围和显式 enterprise_retrieve。服务端每次请求重新验证 Key、账号和最新策略；隔离侧不复制员工账号、Key 类型或 KB，不接受客户端工号/邮箱 Header。

统一 MCP 只注册五工具：`list_knowledge_bases`、`search_knowledge`、`grep_chunks`、`list_documents`、`read_document`。服务端 Tool middleware 仍按五工具白名单拒绝其他名称；公共 Grant 不赋予成员、管理、任意文件下载或写权限。未知工具不能转到旧 REST、旧 Python 或团队 MCP。

只读 **允许 POST**：initialize、tools/list、tools/call 都可能使用 POST。GET/DELETE 是 MCP 传输/会话动作，不是知识写入能力。不能用禁止全部 POST 代替只读授权，也不能靠客户端 denyTools。

## 隔离入口方案

当前两侧可使用同一路径；若未来内网服务增加写工具，必须先增加专用只读路径/只读 server tool registry，再允许隔离入口指向它；仅共享现有进程和授权，不共享可能新增的写工具集合。不能先增加写工具而继续称当前隔离路径只读。

隔离网络若单独入口，只需由现有 Nginx 或边界代理单独监听：精确放行 `/api/v1/mcp/personal`，允许所需 GET/POST/DELETE，其他路径默认拒绝。APP:8080、通用 Web、REST、团队 `/mcp/`、旧 Python 端口不能从隔离网络直达，避免绕过代理白名单。连接同一 Key 到内网的原权限不因隔离入口新增而放宽。

验收模板 [portal_mcp_readonly_nginx.conf.template](tools/portal_mcp_readonly_nginx.conf.template) 仅用于测试或后续审阅，不是已安装生产配置。生产需将 loopback 示例改为正式接口/TLS/ACL，保持 APP 后端不可被隔离客户端直接访问；单独 Nginx 监听不需要额外 Go MCP 服务。

## 实际网络测试范围

续作客户端验收路径为：本机页面/实际客户端 → 本机测试入口 → 固定主机密钥 SSH 通道 → 测试机 loopback Nginx → 同一 Go APP 夹具。管理界面另走受控测试管理通道，隔离 Nginx 不放行管理路由。新监听全部是隔离验收临时 loopback，不是生产开放端口。

Nginx 使用测试主机已有 `nginx:1.30.3-alpine`，只读挂载专用配置，先实际 `nginx -t`。网络检查使用同一把 Key 比较 APP 与 Nginx initialize，验证五工具、未知写工具拒绝、伪造身份 Header 无效、其他 REST/管理/团队路径拒绝。实际结果与客户端协议证据见 [55](56_DYNAMIC_PORTAL_ACCEPTANCE.md)，未通过前不计为 PASS。

这证明隔离测试通道和真实 Nginx 路由；**不代表员工隔离网络到正式域名的路由、防火墙、DNS、证书信任、代理鉴权或 443 已验收**。本批没有实际开通这些通道。需要网络负责人在正式上线变更中单独验证，并确认重连、长连接超时和中间代理不剥离 Authorization。

## 回滚

生产未改，无需生产回滚。停止本批测试 Nginx、夹具和本机转发，保留隔离 PG、源码及日志。未来若独立批准上线，收回时先关闭个人入口/撤回公共策略及边界精确路径，保留审计、旧 Python 和个人原权限；不以删除知识、轮换所有生产 Key 或重建员工目录作为回滚。
