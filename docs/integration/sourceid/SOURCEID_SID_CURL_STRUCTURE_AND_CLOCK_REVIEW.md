# SID 成功 curl 结构与服务器时间核对

2026-10-14，北京时间约 20:22。只读核查，无新增登录/Token 请求，无配置或时钟修改。

## 请求结构

SID 管理员最新提供的 curl 为 POST /oidc/accessToken，Content-Type 为 application/x-www-form-urlencoded，Basic Header，正文仅有 grant_type、client_id、redirect_uri、code。**它也没有正文 client_secret 和 code_verifier**，与实际 KnowHub 四字段结构一致。

尚不能据此认定完全等价：示例 Basic 所属凭据版本、组件编码方式、Postman 实际 Accept/User-Agent/HTTP 版本、来源 IP 和 DNS/网关节点仍未确认；curl --location 允许跟随重定向，但 KnowHub 最新记录没有观察到重定向。管理员提供的是成功结构，不复用其凭据、code 或 Token。

提供无凭据 [Postman 导入模板](business-test/SID_TOKEN_REQUEST_TEMPLATE.postman_collection.json)。Authorization 保持显式 Basic Header 占位符，避免由另一个认证助手重复生成 Header；四个表单字段与 curl 一致。SID_BASIC_CREDENTIALS 和 SID_CODE 均为空，不包含聊天中的真实值。URL 使用普通字符串，不包含聊天 Markdown 的方括号/链接语法。

导入不会发请求；由用户在本地受控方式配置认证值，后续仅替换 SID_CODE。**不能使用已被 KnowHub callback 自动兑换过的 code，也不能使用管理员示例中的旧 code。** 新 code 的获取与单次消费流程需先安排好，不能将“只换 code”理解为复制已处理 callback 后重复请求。本轮不启动 Postman 或真实兑换。

完整 Basic Header 可恢复 Client Secret。聊天中的该凭据不写入任何模板、报告或 Git，不解码比较；请由管理员按既有安全要求处理暴露凭据并安全同步版本，不再发送值。

## 时钟实测

- 打包机时区：Etc/UTC。
- 系统：NTP=yes，NTPSynchronized=yes。
- 采样：服务器 2026-10-14T12:22:19.731244+00:00；app 容器 12:22:20Z；同一时刻 Windows 为北京时间约 20:22。
- 服务器采样落在 Windows 发起与收到只读检查的观测区间内，粗略偏移界限 -0.327 到 +1.287 秒（包含 SSH/进程往返开销）。没有发现明显时钟偏移；这不是精确毫秒级 NTP 测量。
- 最新 Token 写出日志 `2026-10-14T12:04:14.419494239Z` 对应北京时间 **2026-10-14 20:04:14.419494239+08:00**。所以“晚上 8:04”是正确的北京时间；不是早上 8:04，也不是服务器慢了八小时。

给 SID 查日志时同时提供 UTC 和北京时间，不能把时区差异当成时钟漂移。响应 Date 12:04:14Z 与该请求处于同一 UTC 秒，但 Date 本身不能证明所有服务节点时钟绝对一致。

时钟证据：[clock-readonly-20261014-2022.json](evidence/clock-readonly-20261014-2022.json)。
