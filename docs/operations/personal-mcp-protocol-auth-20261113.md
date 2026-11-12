# Personal MCP 协议鉴权修复与发布阻断记录

PR [#138](https://github.com/suoak/WeKnora/pull/138) 已合并到 main
`66976ce6fec4907044377659cbca86c4cb82c0ff`。本记录对应随后真实生产客户端
验收发现的问题；**该候选尚未通过生产验收，生产已回滚**。

## 问题与修复

原处理器在 initialize、tools/list、resources/list 和 SSE GET 时，与实际读取知识
一样枚举并解析整个可检索语料的权限。完整备份恢复副本上，该步骤达到 30 秒
请求上限并返回 HTTP 403；同副本的可写列表约 200 毫秒完成。生产第二次发布时，
实际 Codex 能发现云外 12 个、云内 8 个工具，但随后因 HTTP 403 关闭 MCP transport。
隔离复现揭示了相符的失败路径；仍需真实客户端验证修复，不能据此认定生产验收通过。

协议发现改为实时核实 Key、本人、本地账号、当前空间成员及 Key 空间范围，不加载
文档、Chunk 或向量，也不生成任何知识授权。所有 tools/call 保留原来的内容权限
判定；新增和上传继续复用原有实时写权限。未知替代 Store 保留原完整鉴权作为
失败关闭的兼容路径。权限解析超时返回 504，真实权限拒绝继续返回 403。

云内注册表仍固定八个只读工具，代理路径、TLS、大小限制和边界校验不变。
此修复不增加数据库迁移，不接入延期的外部员工状态源或 ClamAV。

## 已发生的发布与回滚（UTC）

1. 2026-11-12 16:13 左右切换 main 候选。前端 assets 目录被打包为 0700，
   Nginx worker 读取 JS 返回 403。16:15:36 回滚 APP 和前端。
   包装流程随后显式规范目录 0755、文件 0644，246 个静态文件字节保持 main 原样；
   独立真实 Nginx worker 测试复现旧包 403、新包 200。
2. 16:27 左右使用同一 main APP 与修复包装的前端再次发布。实际 HTTP 首页、JS
   字节及提交身份检查通过；真实 Codex 调用随后失败。16:29:40 回滚 APP 和前端。
   合成验收 Key 已撤销、合成账号已停用。未批量开通员工 Key；未创建验收文档。

两次回滚均保持数据库 **3047 / dirty=false**、飞书 fence、Compose 顺序、
挂载/环境/端口/安全属性、8443 TLS 和只读代理不变；未清队列、解除安全锁或
修改 migration 版本。回滚后同一真实个人 Key 实测两个入口均 HTTP 200、8 个工具。

脱敏证据：[第一次回滚](../integration/personal-mcp-protocol-auth-20261113/rollback-attempt1.json)、
[第二次回滚](../integration/personal-mcp-protocol-auth-20261113/rollback-attempt2.json)、
[前端包装与 worker 探测](../integration/personal-mcp-protocol-auth-20261113/frontend-packaging-r2.json)、
[第二次发布实际前端 HTTP](../integration/personal-mcp-protocol-auth-20261113/frontend-http-attempt2.json)。

当前恢复的 APP 源码为 `f71b5160872c982c6d3c747f3668abe71fc95ec9`，镜像为
`sha256:9e2dc174543a188006610ffde7f18b043f02806b6f1a0c3356ba7c4baed5b8c7`。
前端源码为 `bb6196fcd91a6ace333043ee2552431ccc9e8b81`，镜像为
`sha256:d87b2ddfd12d1df35ecd4d508c7c183e63b8948293fd6ae53a3582091a17be39`。

## 数据库恢复链

真实升级前备份 12,678,864,560 字节，SHA-256
`07f6787a9619ad3d30d988e459770fbfd2b5f87d2a154075ee34110db609c4b9`。
隔离完整恢复的实际 Docker exec 退出 0，正式工具完成 3046→3047、二次执行保持
不变、业务计数和历史用量摘要保持、无效飞书 nonce 写入被拒绝。外层监督等待
曾超时，已用持久化实际 exec 身份及退出记录核实，未将超时伪装成成功。
生产只读核实已是 3047，本任务没有运行生产迁移。
[完整恢复与迁移结果](../integration/personal-mcp-protocol-auth-20261113/restore-migration.json)。

## 下次发布闸门

修复须先完成 Linux 回归、完整副本复现及真实 Codex 双入口调用，再经 PR/CI 合并
main，并从重新确认的 main SHA 构建。不得从修复分支直接发布；前端包装权限检查
必须保留。发布保留两次历史回滚记录，验收失败仍回滚应用并停用临时验收凭证。

最终仍需生产验证文本和五类文件、解析/Chunk/向量/检索、并发幂等及失败恢复、
跨用户/同空间拒绝、有效 Editor、撤销权限及 8443 固定八个只读工具。通过这些闸门
后，按用户授权为当前有效 Personal Key 开通 add_content；不增加 FullAccess、
不改变固定范围或知识库 ACL，新 Key 默认关闭。

外部员工状态即时同步及 ClamAV 按用户决定延期。本地账号、Key 和权限仍实时校验；
现有文件大小/格式/包结构限制保留，但不能声称已有病毒扫描保障。HTTP 80 仅用于
用户确认的可信公司内网；可见 DNS/主机信息指向私网，不能据此断言不可见上游
NAT 不存在。发现不可信网络暴露时停止开放写入。
