# M3 资源链接、缓存和历史引用专项威胁模型

第二批完成设计；尚未实现生产资源 broker，未改变任何签名或缓存配置。以下门禁不是“已经修复旧 REST”的声明。实际 M2 text-only 组件遇到 HTTP(S)、resource/storage/backend/local/COS/S3 引用、文件代理路径、短链、Markdown 图片即拒绝输出整次正文；此保守拦截只适用于隔离试点，不代替 M3 的资源绑定解析。

## 资产、攻击面与撤销缺口

| 资产/入口 | 现有边界和威胁 | 必须实施的处理 |
| --- | --- | --- |
| `/api/v1/files/presigned` | HMAC/expiry 后直接 GetFile；一天 `public,max-age=86400`；无员工主体，签名仍有效时拿到链接者可复用 | 敏感资源禁止匿名签发；现有处理器在读对象前解析当前资源/文档策略并阻断敏感或 unknown 资源；去除敏感响应公共缓存 |
| `/r/:token` | 短期 bearer 能力不是员工 ACL；被复制、日志/浏览器历史泄漏后可重放 | 改为主体绑定且可撤销的 resource request，或拒绝受限资源；每次核验当前 policy/resource version，不能只验 token 未过期 |
| 对象存储直链/历史 presigned S3/COS | 请求绕过应用，应用 ACL 变化不会自动使已签 URL 失效 | 存储侧停止公开/匿名策略、撤销有界签发能力或迁移受控对象；不能即时撤销且无法证明失效时 HOLD，不开放受影响 KB |
| `/files` raw、KB files、preview、download、batch-download | tenant/KB access 和 storage path 校验不等于文档 ACL；preview 也可能返回原始字节 | 将 resource binding 反解到所有来源文档，统一 operation 与本文策略；仅 parsed-read 不能下载原始文件 |
| 图片、附件、图库及多绑定资源 | 公开副本或另一份绑定可成为读敏感副本的旁路；EXIF、caption/OCR/缩略图同样敏感 | 精确 hash/handle 与可信当前 binding 认证；混合来源按最严策略，处理所有派生尺寸与 metadata |
| 历史 message/Agent/artifact/Wiki 引用 | 读者拥有会话不代表仍拥有源文件；引用中可能嵌已签名 URL 或缓存正文 | 保留 session ownership + 当前来源授权双检查；未知来源闭包拒绝；不凭持久化引用重新授予来源访问 |
| 浏览器/CDN/反向代理/检索/LLM缓存 | 公共缓存可能脱离鉴权，文档 ACL 新版本不能撤回已缓存字节 | 从 sensitive/unknown 响应禁用 public/shared caching；每次 cache hit 在取正文前查当前权限和 revision；隔离 user/key/operation，紧急失效与 purge 要留证据 |

链接在正文中、图片 src、Wiki Markdown、FAQ answer、历史 session tool result、metadata、redirect Location 中都可能泄露能力。只从 search response 删除 URL 不会解决 download/proxy 或旧引用。

## 可即时撤销的受控路径设计

拟提供认证资源读取入口（接口形状仅设计，不是新增路由）：`GET /api/v1/resources/:handle/content`。句柄是定位符，不是授权。服务端从可信身份、当前 Key/账号/员工与成员状态确定 caller；从 resource catalog + live bindings 查 ownerTenant/KB/Knowledge，调用同一 document ACL，校验精确操作 `parsed_attachment_preview` 或 `original_download`。public parsed grant 不含这两个操作。

在 fetch/storage、redirect、响应 headers/body 前读取当前 policy/source/credential revisions；长流采用有界时间/分段重验并在收权后终止后续输出。不得为规避应用鉴权返回可复用的匿名存储 URL。下载采用受控流式代理、`Cache-Control: private,no-store`、`Vary: Authorization`；不在 query 放 Key，HEAD 与 GET 使用完全相同授权。Range、ETag/304、跨 origin 请求均须先鉴权，避免 304 或 metadata 泄露。

复用 access/files.go、message_files.go 和 resource catalog 既有所有权/当前绑定检查，在其上追加文档 ACL，不能删除原 KB/会话/存储路径守卫。授权变化同步拒绝读取，再异步清理缓存/衍生索引。resource/version/credential cache hit 不能依赖过期 TTL；当前版本不可验证时 fail closed。原文件许可、公共正文许可和空间管理权分别判定。

## 已签历史匿名链接的处置流程

1. 先冻结为候选受限资源新签匿名链接，盘点签发者、签名体系、对象公开策略、代理/CDN/浏览器缓存和最大存活窗口；仅记必要 resource ID/聚合，不输出实际 token/URL。
2. 应用代理链接：所有仍可命中的 handler 必须在存储读取前根据可信资源绑定识别受限资源并拒绝；短链状态可撤销，历史签名即使校验通过也不能放行。
3. 存储/CDN 直接链接：验证供应商的撤销/对象策略/缓存清除能力。若需轮换签发 Key 或移动对象，须另行批准并评估其他消费者；本批不操作任何生产密钥或数据。清理回执和负例测试是验收证据。
4. 无法即时使存量 URL/共享缓存失效：受影响资源及衍生闭包继续 HOLD，不宣布敏感收权完成。可选等待已知最大旧签名与缓存窗口全部结束并验证所有旧 URL/缓存拒绝，但等待期间仍保持关闭；不能仅缩短新 URL 的过期时间。
5. 已向合法用户传输或用户另存的内容无法由服务端追回；撤销保证的范围为后续受控请求、后续缓存读取和后续流片段，不作“追回已下载字节”的承诺。

## 必须执行的合成负例

旧签名签发→文档收权→重放 GET/HEAD/Range/304；short token 拷贝给 C；A 会话引用 D 的源附件；相同物理对象多绑定；public→restricted→public 版本回退；Key 轮换/吊销；CDN hit/body cache hit；对象存储直链绕过代理；连接中途失权。C 的任何路径不得得到标题、缩略图、OCR/正文或对象长度等受限信息，D 只能读其精确 grant，匿名永不获得 restricted 内容。

当前已验证仅 M2 组件正文含匿名/资源引用时整体拒绝，以及候选空集/未知衍生来源阻断的接口契约。真实 proxy/CDN/存储/Wiki/历史引用的上述测试均 NOT_TESTED，且是混合敏感 KB 激活前的强制门禁。
