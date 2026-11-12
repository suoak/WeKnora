# 隔离区 MCP 实施与生产变更草案（2026-11-11 修正）

## 实施记录 R3：当前采用既有永久只读 PersonalServer

P1 获用户批准后已在独立 worktree 和获批打包测试主机实施；未创建第二个 Server/认证/统一检索服务。原生产地址不变。保留固定8工具与调用guard，补充协议、日志和输出边界，通过独立测试TLS入口验证。最新实施和证据见 [P1报告](P1_REPORT_2026-11-11.md)，生产变更以 [P2部署与回滚草案](P2_DEPLOYMENT_AND_ROLLBACK.md) 为待审批版本。真实生产源码尚未核实，不能发布本轮 main 全量差异。以下原始方案和修正保留作历史，过时的独立Server/重新开发建议不作为当前实施选型。

## 补证记录 R2：复用方案已取得现有入口协议证据

2026-11-11 使用用户授权Key，原IP入口 initialize、initialized通知、tools/list、list_knowledge_bases全部成功；实测工具集为既有8个查询工具，没有ask或文档写工具。列表返回62项，仅记录条数。[脱敏证据](PROTOCOL_EVIDENCE_2026-11-11.json)

因此继续采用R1的“复用既有PersonalServer、认证和知识服务”方案，不建设新Personal MCP，也不要求现有用户改地址/Key/配置。R1中“缺少授权Key、协议未测”现已由本次补证替代；生产版本、真实代理upstream/容器、日志和网络边界仍待核实。未据此开放8443、部署APP、改动生产配置或进入P1。

## 修正记录 R1：复用现有 Personal MCP 的最小改造

本节为现行方案；下方 2026-11-10 草案完整保留作历史记录，其中“Personal MCP 缺少接线”和“新建独立 8 工具实现”的前提撤回。**现有 URL、员工、个人 Key、知识范围、检索和客户端配置全部保留。P0 只审计和修订文档，P1 尚未获准开始。**

| 入口 | 本轮状态与后续关系 |
|---|---|
| 内网 `http://10.51.134.114/api/v1/mcp/personal` | 用户确认正在使用；保持地址及现有行为，不做迁移、不停用、不改生产配置 |
| 隔离 `https://knowhub.ruijie.com.cn:8443/api/v1/mcp/personal` | 待规划的入口，未开放；使用同一员工同一 Key，授权不得超出现有 Personal MCP |
| Go 后端 | 复用现有 APP 内 PersonalMCPAuth → Auth、PersonalServer、PortalMCPKnowledge；不新建员工目录/索引/Key 系统，不部署第二套 MCP |

### R1.1 源码基线与最小方案选择

实施参考固定为已核验 GitHub main `4b385fd11e2f617eee491781067b2f69cb8aa1be`；当前旧开发分支不再作为 Personal MCP 开发底座。P1 若获准，先在另行隔离的检出位置按**已补证的生产版本**选基线及必要最小补丁，不能直接把 main 全部差异升级生产。P0 本轮只 fetch/read Git 对象，没有切换或合并当前工作区。

main 的 `PersonalServer` **已经**独立注册 8 个只读工具并用 `enforceGuard` 再验证，`ask` 没有注册。已有 1 MiB、并发槽、30 秒 context、目标解析、实时授权及输出前复验；复用这些实现及原有认证/知识服务，不重写。

按补证后的生产代码选择最少改动：

1. 若生产等价实现明确把现有 PersonalServer 定义为永久只读，8443 可精确转发原 personal handler；将“此 handler 永不注册写工具”固化为测试和评审门槛。未来内网写功能必须用其他可写处理器，隔离入口继续绑定原只读实现。
2. 若无法给共享 personal handler 作永久只读保证，则仅新增固定隔离**路由绑定和不可提权策略**（例如 `/internal/mcp/isolated/personal`），复用同一 PersonalMCPAuth、PortalMCPKnowledge 和现有只读构造/guard；8443 对外仍用原约定 URL，固定转发该内部路由。可在同一 APP 生成独立只读实例以隔离未来注册变更和并发预算，不复制认证或检索逻辑，不增加后端监听端口/新 MCP 项目。

第二种是未来写能力尚未定案时的保守建议，**不是当前实现缺失的补建工作**。配置和测试必须保证隔离实例的注册集不受内网 ingest、管理员端点配置或客户端 Header 改变。路由闭包决定权限，不依赖 X-Environment/Host/XFF 作为可信环境凭证。

同一 Key 的 Bearer 转换、默认工作区、dynamic_all/持久化 scope、用户与成员复验完全沿用现有契约；不能强迫内网客户端新增 X-Tenant-ID 或更换 Key。两入口读取同一知识授权，隔离只是进一步限制能力，不修改存量授权数据。

### R1.2 后续 P1 的必要增量

* 代理：增加独立 HTTPS 8443 listener，仅精确 MCP 路径、GET/POST/DELETE；1 MiB、JSON 媒体类型、multipart/二进制拒绝、no retry/no cache/streaming、源 ACL/速率/并发/超时。前端已有 exact personal location 和关闭重试配置要保留；不重建整站 HTTP 配置。
* Go：复用现有只读 registry/guard/body cap/deadline，仅补测试证明确认的 schema、Content-Type、Origin、原始路径和未知字段等缺口；不要先改默认分页、查询能力、输出或身份契约。隔离直达 Go 时仍须执行固定策略。
* 日志：在 body/响应捕获前对隔离流量禁存载荷；复用现有日志框架的元数据模式并核对所有下游日志。`personalPerformanceTrace` 的固定性能指标不等于 `Logger` 和 analytics 全部安全。隔离 usage 的客户端自由文本须固定化/省略；保留的资源 ID/计数应有受控落盘范围。
* 输出：复用 SanitizePortalText、SafeDocumentText 和既有 knowhub 引用；验证全部查询/Wiki 分支不透传附件签名、物理路径或匿名访问能力。不能另接 workspace `rewriteResourceURLs` 作为便捷图片出口。
* 网络：隔离区只允许目标8443，不可到80、8080、工作区MCP、Python MCP、数据库、缓存和对象存储写入口；同时保留内网原有网络使用方式。终止点/NAT/IPv6、Compose 映射和 Key 只读挂载方案待现场证据决定。

### R1.3 当前调用链及生产版本补证方法

源码链已确认：main Nginx 精确 personal location → APP `RegisterPersonalMCPRoutes` → PersonalMCPAuth/原 Auth → PersonalServer → PortalMCPKnowledge。**真实部署链仍需通过指定容器/配置及授权 RPC 相互印证**，不能拿源码图替代现场证据。

获准的只读管理会话中，限定选定 APP/前端容器查询，建议字段如下（由实际容器名称替换示例，不整份输出 inspect/env）：

```text
docker inspect --format '{{.Name}} {{.Image}} {{.Config.Image}}' <approved-container>
docker inspect --format '{{json .NetworkSettings.Ports}}' <approved-container>
docker image inspect --format '{{.Id}} {{json .RepoDigests}}' <approved-image-id>
docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' <approved-image-id>
```

再由只读运维提取：当前 personal 精确 location 的 proxy_pass、upstream 地址/网络、Config 文件来源、APP 构建标识/制品 SHA 与 CI 对应关系、开关的布尔/存在性、Nginx版本/SSL模块。禁止输出完整环境、Secret、完整生产日志；不执行历史生产部署脚本、数据库查询或真实用户 Key 提取。OCI revision 是声明，须与制品来源对应；没有可验证来源时源码版本保留 UNKNOWN。

协议验证须用已授权测试 Key 在**既有 IP URL**完成 initialize → initialized（协议所需）→ tools/list → list_knowledge_bases，必要时再用获准关键词做检索。实际服务特征、关联时间和代理到达的选定容器记录结合，才可确认真实服务归属；serverInfo名称/版本字符串本身不证明镜像 SHA。不跟随跨主机 redirect，不导出 Key 或知识正文。具体步骤见测试方案 R1。

本轮尚无可用授权 Key/客户端会话，也未取得可用生产只读管理会话；协议调用及部署版本补证均待完成。它们是P0开放项，不能把“文档已修正”写成全部P0通过。

### R1.4 配置与回滚草案如何继续使用

下方历史 Nginx/Compose 草案只作为安全项清单：若采用原只读handler方案，其上游 URI 应是现有 `/api/v1/mcp/personal`；若采用固定隔离路由方案，才使用草案中的内部 URI。两者均须按真实拓扑重写和 nginx -t，不能照抄未存在的内部路由上线。实际生产是否已有安全项，要从当前生效配置确认。

回滚只关闭新增隔离通道和撤销本次增量，**不得关闭现有内网 personal入口、撤回现有公共策略、轮换个人 Key 或改变知识授权**。先阻断隔离8443并终止隔离连接，再按获批方案恢复本次新增监听/映射/配置；恢复 APP 仅限本次最小增量且保留其他已发布修复。生产上线、证书复制、8443开放、数据库/Key/DNS/防火墙变更本轮均不执行。

---

## 以下为 2026-11-10 原始草案（历史保留；须依 R1 修订后才可评审实施）

# 隔离区 MCP 实施与生产变更草案

2026-11-10。**仅供评审，未实施、未验证配置。P1 开发须等本轮用户确认；生产须另行批准。**

## 1. 入口和授权设计

对外固定为 `https://knowhub.ruijie.com.cn:8443/api/v1/mcp/personal`。现有 HTTP 网站和内网 MCP 配置保持原有行为，不改变全局 `MCP_PUBLIC_URL` 强制所有内网客户端迁移，也不自动跳转网站 HTTPS。

建议数据流：隔离区批准出口 → 防火墙允许的 TCP 8443 → 已确认的 TLS 终止代理 → APP 内固定隔离路由 → 原有 Key 验证/实时授权 → 独立只读工具注册表 → 现有知识服务。复用同一 APP、数据库、知识库及个人 Key，不建立新 MCP 项目或第二套凭据。

实际 Personal MCP 代码先与生产版本对齐。当前仓库仅有工作区端点 MCP 和 REST 个人 Key 校验，缺少两者接线，不能仅用代理配置达成目标。如果生产有既有实现，必须复用该实现；如果没有，则将最小接线补足范围明确提交 P1 评审。

Go 层选用**固定隔离路由**，例如仅供反向代理使用的 `/internal/mcp/isolated/personal`（拟议，当前不存在），而不是由客户端传 `X-Environment` 决定权限。8443 精确路径固定改写到该路由，无路径参数或动态上游选择。这个路由无论经什么网络访问都只能只读；不改变既有内网路由的能力。

策略由路由闭包在服务端创建不可提权上下文，不能把客户端环境 Header、Host、X-Forwarded-*、工作区参数当成可信环境证据。代理应移除可伪造的环境 Header；它们即使漏删也不得参与鉴权。`X-Tenant-ID` 只作为工作区选择器，经同一个人 Key scope、实时用户/成员/共享授权验证，不构成环境选择。

隔离入口可以从当前个人 Key 的验证函数复用身份逻辑，但不得用端点机器身份替代员工身份。有效能力是个人 Key/用户实际权限与隔离只读白名单的交集；选定 KB 白名单、撤销、失效、工作区禁用均即时生效。不要给所有隔离访问附加 full-access，也不要通过新建端点 token 绕过个人 Key。

独立 MCPServer/注册表只注册以下 8 个工具，handler 复用原有实现：`list_knowledge_bases`、`search_knowledge`、`grep_chunks`、`list_documents`、`read_document`、`wiki_search`、`wiki_read_page`、`wiki_index`。调用 guard 再验证固定列表和能力；不引用随内网扩展的 DefaultMCPEndpointTools。不注册 ask、Agent、资源模板写入、文件、HTTP 代理或其他写工具。

查询工具仍须逐次验证 KB、文档和 Chunk 授权，不能只在 initialize 检查。不得跨请求缓存个人身份到其他用户会话；如果后来启用 MCP session，必须绑定 Key 身份和工作区，并在每个请求重新鉴权。

## 2. Go 必须执行的约束

1. 在读取、解析和执行前限制整个请求体为 1,048,576 字节，未知长度及分块传输同样有效；超限直接拒绝，不能部分执行。GET/DELETE 不接受业务载荷。
2. POST 使用解析后的 `application/json`（兼容合法 charset 参数），拒绝 multipart、octet-stream、压缩内容等。限深、限字段、限查询长度；建议 query/keyword 最大 4096 UTF-8 字节、ID/slug 另设合理上限，P1 验证后固化。
3. 按所用 mcp-go 版本和协商协议处理 initialize、initialized notification、ping、tools/list、tools/call。未知 JSON-RPC 方法/工具不得动态回落 REST。2025-06-18 协议只接受单个消息，不执行批量写调用；其他版本也只接受明确验证的能力。
4. JSON-RPC 错误不回显提交的附件、代码、URL、未知字段或凭据。服务端正常 POST 不因方法而被禁止。GET/DELETE 是否返回流或 405 取决于无状态传输能力，验收允许协议规定的结果。
5. 严格 schema：只接受工具定义的必要字段及已审协议元数据，拒绝未知 file/path/url/content/base64 等载荷字段；不能仅靠字段名称识别文件安全性。正常查询可含代码词语，但不得落入正文写入/日志。
6. 实现 Origin 校验，允许无 Origin 的原生客户端，存在 Origin 时只接受批准值。拒绝未知 Host/路径/查询串；不接受 query-string Key；认证头名称沿用真实 Personal MCP 契约，必要的 Bearer/X-API-Key 适配须防止冲突凭据。
7. 在任何请求体窥探前让隔离路由进入元数据日志模式；身份服务、工具、检索服务、模型、APM、error handler 均不能记录载荷。不要将输入写入 session/message、业务缓存、搜索历史、资源或队列。
8. 输出仅含授权文本、必要引用 ID/标题；禁止签名 URL、匿名下载授权、存储路径等外部能力泄漏。对原文中外链也需有明确过滤策略；不通过 MCP 自动抓取链接。
9. 每身份限速、并发和执行 deadline 与 Nginx 配合，多副本用共享限流；身份存储或授权服务故障时拒绝，不能降级放行。

## 3. Nginx 规则草案

下列为结构草案，**不是可直接上线文件**。须补齐批准源 CIDR、实际上游、证书挂载、Origin 策略和真实客户端 IP 处理后，在 P1 用目标镜像 `nginx -t` 及流式测试。示例按前端 Nginx 终止 TLS 设计。

```nginx
# http {}：独立文件，不要被 envsubst 展开 Nginx 的 $变量。
map $request_uri $isolated_uri_ok {
    default 0;
    "/api/v1/mcp/personal" 1;
}
map "$request_method:$http_content_type" $isolated_media_ok {
    default 0;
    "~*^POST:application/json([ ]*;[ ]*charset=utf-8)?$" 1;
    "GET:" 1;
    "DELETE:" 1;
}
map $http_content_encoding $isolated_encoding_ok {
    default 0;
    "" 1;
    "identity" 1;
}
log_format isolated_meta '$time_iso8601 $remote_addr $request_method '
                         '$status $body_bytes_sent $request_time';
limit_req_zone $binary_remote_addr zone=isolated_req:10m rate=2r/s;
limit_conn_zone $binary_remote_addr zone=isolated_conn:10m;

server {
    listen 8443 ssl;
    server_name knowhub.ruijie.com.cn;
    ssl_certificate /etc/nginx/isolated-tls/fullchain.pem;
    ssl_certificate_key /etc/nginx/isolated-tls/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_session_tickets off;
    # 不启用 TLS early data / 0-RTT。
    client_max_body_size 1m;
    client_header_timeout 10s;
    client_body_timeout 15s;
    keepalive_timeout 30s;
    access_log /var/log/nginx/isolated-access.log isolated_meta;
    # Nginx error log 可能含请求行/参数。草案关闭该 server 原始错误落盘；
    # 如需错误采集，先用经过验证的脱敏管道，禁止 debug/WAF 请求体审计。
    error_log /dev/null crit;

    location = /api/v1/mcp/personal {
        if ($isolated_uri_ok = 0) { return 404; }
        if ($request_method !~ ^(GET|POST|DELETE)$) { return 405; }
        if ($isolated_media_ok = 0) { return 415; }
        if ($isolated_encoding_ok = 0) { return 415; }
        # 必须由真实已批准 CIDR 替换；未补齐时保持拒绝所有来源。
        deny all;
        limit_req zone=isolated_req burst=20 nodelay;
        limit_req_status 429;
        limit_conn isolated_conn 8;
        limit_conn_status 429;
        proxy_pass http://app:8080/internal/mcp/isolated/personal;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host knowhub.ruijie.com.cn:8443;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Environment "";
        proxy_set_header X-MCP-Environment "";
        proxy_set_header X-Isolated "";
        proxy_next_upstream off;
        proxy_cache off;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_connect_timeout 5s;
        proxy_send_timeout 30s;
        proxy_read_timeout 300s;
        add_header Cache-Control "no-store" always;
        add_header X-Accel-Buffering "no" always;
    }
    location / { return 404; }
}
```

还需针对未知 SNI/Host 配置默认拒绝 server，不能让其他虚拟主机成为旁路。TLS 若在边界终止，上述规则须在边界等效执行；不能以 TLS 后的全站通用代理替代。Origin 校验在 Go 做强制校验，Nginx 可增加同等规则。前端 Nginx 收到边界代理时，只有经网络允许的实际代理 IP 可以设 real_ip；不得信任任意 XFF，SNAT 下须按真实 NAT 出口规划白名单。每 IP 2r/s、并发 8 只是 P1 初值，共享 NAT 需按人数测量并结合每 Key 限流调整。

`$request_uri` 原始匹配额外拒绝查询串、尾斜线、编码路径、重复斜线等归一化绕过。GET/DELETE 不带 Content-Type 是草案的收敛约定，若真实 Codex 客户端会携带 JSON Content-Type，应在 P1 验证后允许其规范变体，Go 仍不接受非空 body。不要仅因例子较严就关闭协议所需行为。

不沿用通用 api-proxy.conf 的重试策略，不设置 proxy_method GET。流式读超时为相邻两次读取之间的限制，Go 另设整个操作 deadline。关闭请求缓冲后数据会逐步到 APP，因此 Go 必须在整个 capped JSON 完成校验之前禁止任何工具副作用。[Nginx 代理指令说明](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)

## 4. Compose 与证书变更草案

如果终止点为现有前端：保留现有 80 映射，新增 `批准的内网绑定IP:8443:8443`；不要直接不带绑定地址发布到全部网卡。沿用原服务、网络及 APP upstream，不改 APP 8080 监听或数据库配置。增加如下只读挂载，待批准后才在生产创建目录/复制文件：

* `/etc/knowhub/tls/isolated/fullchain.pem` → `/etc/nginx/isolated-tls/fullchain.pem:ro`
* `/etc/knowhub/tls/isolated/privkey.pem` → `/etc/nginx/isolated-tls/privkey.pem:ro`
* 独立 8443 配置 → `/etc/nginx/conf.d/isolated-mcp.conf:ro`（包含 http 级 map/server，避免全量遮盖 conf.d）

主机目录 0700，Key 0600，Nginx master 所用身份具备最低读取权限；若现有镜像非 root 或启用 SELinux，先测试 UID/GID/安全标签。证书只交付批准服务器目录，不放仓库、镜像层、构建上下文、普通环境文件。P1 优先测试专用证书；若需真实证书副本，应另有明确批准且只存在受控测试证书目录。

新增 Docker ports/volume 通常要求重建前端容器，不能把 `nginx reload` 当成端口发布步骤；可能短暂影响现有 80，因此 P2 必须安排维护窗口、验证重建和旧连接影响。上线 APP 最小路由适配也可能重启 APP，需要与原内网 MCP 的回归和可用性要求一起评审。不能为本次工作顺便升级镜像或修改共享配置。

如果边界设备终止 TLS，就不新增前端宿主机 8443 映射或向它复制 Key；由边界团队按批准路径管理证书和 ACL，后端转发必须限定至固定隔离路由，跨不可信网络需再加 TLS/认证保护。边界真实能力/准入流程待补证，不能现在选定设备。

## 5. 防火墙和 DNS 规则建议

| 方向 | 建议规则 |
|---|---|
| 隔离区批准出口/NAT IP → 实际 VIP/目标 IP | 仅 TCP 8443，状态跟踪允许返回 |
| 隔离区 → KnowHub 80/443/8080、其他 MCP 端口（例如可选 8082） | 拒绝；覆盖所有后端别名/IP、IPv4/IPv6 和旁路路径 |
| 隔离区 → PostgreSQL、Redis、MinIO/S3/COS/TOS、向量库、管理台 | 不因 MCP 增加放行；验证没有已有宽泛规则覆盖 |
| 边界代理 → 固定 APP/前端后端 | 只允许所需后端地址/端口；不允许隔离区直达此通道 |
| 内网原有用户 → 原有 HTTP/MCP | 保留原规则和使用方式 |

同 Key 不是环境标识；若隔离区仍能到达内网入口，未来内网开放上传后即可绕过。本机当前能到 8080 只是检查源的事实，必须由隔离区验证其阻断。防火墙规则由网络管理员最终提供源/目的 CIDR、区域、NAT、规则顺序、命中证据；本轮不下发规则。

DNS 原域名若在隔离区已正确解析至批准终止点，不需要变化；端口不属于 DNS。若隔离区 DNS 不同或存在 AAAA，只提出网络团队处理需求，不能自行修改生产 DNS。证书 SNI 始终使用 knowhub.ruijie.com.cn。

## 6. P2 审批包与回滚

P1 通过后生产变更单必须附：终止点拓扑和负责人、源/目的/NAT 地址、当前和候选镜像 digest、准确配置 diff、证书路径/指纹/链验证、挂载权限、端口及防火墙规则、DNS/IPv6结论、P1验收证据、操作窗口/影响、回滚时限和执行人。未经人工批准不复制 Key、不修改规则、不发布端口、不部署 APP。

批准后的操作顺序：保留受控的旧配置/镜像和选择字段的基线 → 部署并验证 Go 固定只读路由（外部仍不可达）→ 在未放行状态装载证书/代理配置、nginx -t、维护窗口验证重建 → 管理员最后放行隔离源 → 从隔离区执行批准的验收 → 观察拒绝计数和 HTTP/内网 MCP 回归。具体自动化需 P1 才编写。

任何 Key 跨权限、写入成功、非 MCP 路径可达、敏感日志、证书链失败或内网回归失败均立即停止放量。回滚先由网络/代理负责人撤销隔离源 8443 可达性并终止隔离流连接，再移除 8443 server/映射/挂载并恢复旧前端配置；如需恢复 APP 镜像，确认不涉及破坏性迁移及其他生产变更。用旧配置 nginx -t、80健康/内网 MCP检查确认恢复。证书副本按批准留存/销毁策略处理，不删除原始证书归档，不改 Key，不回滚业务数据库。

本方案不需要数据库迁移；如果后续实现提出迁移，必须另行审计并更新回滚方案。已有内网 MCP 请求不得因隔离并发耗尽共享 APP 资源，P1 需测量资源预算，P2 配置相应并发隔离。
