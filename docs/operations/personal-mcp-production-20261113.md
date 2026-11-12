# KnowHub Personal MCP 生产发布验收报告

**生产发布完成，验收通过。** 验收完成时间：2026-11-13 07:12:01 +08:00。
功能 PR [#138](https://github.com/suoak/WeKnora/pull/138) 与协议鉴权修复
[#141](https://github.com/suoak/WeKnora/pull/141) 已合并。生产仅使用合并后的 main 构建。
本报告的文档分支不作为生产构建来源。

| 对象 | 实际生产结果 |
| --- | --- |
| 生产 main SHA | `474feec9b6e10f5b9f42a9ecc94e667c6e7493f5` |
| APP / 内置六个 Asynq worker pool | `sha256:453b855d2a97052a7ac593fb05991e570c6fd505e927d3e80df6f6f9c0d6c97b` |
| 前端镜像 | `sha256:e20ddd2487d42e5f28d4829e2e42aeea54af797e7c21d6e1d96ebbe59e69c370` |
| APP 二进制 SHA-256 | `f0888966924a76605ebf321409d0779ebea5336e121e05bd22fbaaf52fc39244` |
| PostgreSQL schema | **3047，dirty=false** |
| 本次生产迁移 | **未执行**；`AUTO_MIGRATE=false` 保持 |
| 当前轮次 / 历史回滚 | 第三轮成功；前两轮已回滚，本轮未回滚 |

镜像摘要为实际 Docker/containerd manifest digest；OCI config 和新 layer 字节也独立
校验，父镜像运行环境保持，未向外部 Registry 推送。详见
[镜像字节](../integration/personal-mcp-production-20261113/image-bytes.json)、
[main 来源](../integration/personal-mcp-production-20261113/paired-source.json)、
[最终运行状态](../integration/personal-mcp-production-20261113/runtime.json)。

## 真实生产调用与权限

同一真实个人 Key（ID 34，不包含 Secret）通过实际公司域名调用：

| 入口 | HTTP / 工具 | 验证 |
| --- | --- | --- |
| `http://knowhub.ruijie.com.cn/api/v1/mcp/personal` | 200 / 12 个工具 | 检索 + 可写发现、文本、文件、处理状态 |
| `https://knowhub.ruijie.com.cn:8443/api/v1/mcp/personal` | 200 / 固定 8 个只读工具 | TLS 证书及域名验证通过；写入、上传、状态扩展、更新、删除均拒绝 |

[双入口实际调用](../integration/personal-mcp-production-20261113/actual-dual-entry.json)、
[真实 Codex 客户端](../integration/personal-mcp-production-20261113/actual-codex-client.json)。
实际安装的 Codex app-server 经两个生产地址完成工具发现、门户统一知识库列表、
可写列表，以及 Markdown 和 TXT 上传；没有模型生成，也没有用 Mock 替代 MCP 传输。

**64 项生产检查全部通过**：两个新建且真实权限隔离的合成用户、多个知识库、仅
Retrieve、固定范围、本人普通/私人知识库、有效跨空间 Editor、同空间他人库拒绝、
他人私人库拒绝、Viewer/Portal 只读拒绝、伪造 Tenant/Header/参数拒绝、URL 导入
拒绝、编辑授权/Key 撤销和本地账号停用立即拒绝、跨 Key 状态访问拒绝、并发幂等
和不同 payload 冲突、审计、两入口隔离及正常检索。
[完整检查名称与逐文档数据库结果](../integration/personal-mcp-production-20261113/acceptance.json)。

Markdown 文本与 PDF、DOCX、XLSX、TXT、Markdown 文件等共 **13 个新文档**，包括
Codex 创建的两份，均实际完成解析并生成非零 Chunk / embedding。关键词、语义、
混合检索通过；五类上传文件均可被 MCP 检索命中；用户 A 可读取 B 合法发布的门户
内容，同时不能向 B 的只读目标写入。
[13 个处理状态](../integration/personal-mcp-production-20261113/processing.json)。

按照用户将试点改为全面开通的明确授权，已为 **80 个当前有效 Personal Key**
添加 `add_content`。未扩大知识库 ACL 或固定范围，未启用 FullAccess，机器 Key
保持；新建 Key 默认仍关闭。运行时每次实际写入重新核验当前权限，公共检索不转化
为新增权限。能力变更前值已在生产 root 私有 journal 落盘并 fsync，支持条件回退。
[能力变更结果](../integration/personal-mcp-production-20261113/key-capabilities.json)。

本轮三个短期验收 Key 已撤销、两个验收账号已停用，保留验收文档和审计；未清理员工
内容。[真实隔离资源 ID](../integration/personal-mcp-production-20261113/synthetic-resources.json)、
[凭证回收结果](../integration/personal-mcp-production-20261113/synthetic-credentials-retired.json)。

## 数据库、配置与任务

旧快照 3046 已过时；本次发布前真实生产已是 3047。12,678,864,560 字节的真实升级前
完整备份已在隔离副本恢复，正式链 3046→3047、重复执行、业务计数与飞书 fence
负向验证通过。本轮 main 的 migrations Git tree 与已验证版本完全相同，新 main
编译的正式 schema-upgrade 又在完整副本的 3047 上验证 `unchanged=true`，前后
6839 个知识、6189640 个 Chunk、1928142 个 embedding 与 fence 保持。
[完整恢复链](../integration/personal-mcp-production-20261113/full-backup-restore.json)、
[新 main 的 3047 幂等验证](../integration/personal-mcp-production-20261113/migration-compatibility.json)。
没有生产迁移、版本强改、down/force 或队列重置。

生产现有 Compose 叠加顺序、环境变量、挂载、端口、安全属性、无 Docker Socket
挂载、8443 TLS 配置及只读代理均保留；其他服务容器保持。没有独立 Worker 镜像，
Worker 与 APP 同一提交。`sync` 和 `approved_sync` 继续暂停；既有 Wiki 活跃/重试
任务正常保留，未清队列、解除飞书安全锁。
[队列前后快照](../integration/personal-mcp-production-20261113/queues.json)。

可见主机和 DNS 指向私网 `10.51.134.114`，用户确认公司内网边界。HTTP 80 的 Key
只在可信公司网络使用；主机核验不等于完整证明不可见的上游 NAT 规则。

## 回滚及修复记录

| 轮次 | 触发与结果 |
| --- | --- |
| 1，旧 main `66976ce6f` | 静态资源目录 0700 导致 Nginx worker JS 403；2026-11-13 00:15:36 +08 回滚 APP/前端，3047 保持 |
| 2，旧 main `66976ce6f` | 前端修复通过后，实际 Codex 因协议请求全库权限解析超时 403 关闭 transport；00:29:40 +08 回滚并停用合成凭证 |
| 3，当前 main `474feec9b` | 协议元数据改为实时身份/Key/空间资格核验，实际内容操作完整鉴权保留；生产 64 项及真实 Codex通过，07:12 验收完成 |

[第一次实际回滚](../integration/personal-mcp-production-20261113/rollback-attempt1.json)、
[第二次实际回滚](../integration/personal-mcp-production-20261113/rollback-attempt2.json)。
静态目录/文件规范为 0755/0644；发布前使用实际 Nginx worker 网络隔离探测，发布后
核对实际 HTTP 首页/JS 字节。
[worker HTTP](../integration/personal-mcp-production-20261113/frontend-worker.json)、
[生产实际静态文件](../integration/personal-mcp-production-20261113/frontend-served-http.json)。

当前保留的受控回滚入口：

```sh
sudo sh /root/knowhub-personal-main-474feec9b-20261113/rollback.sh
```

此入口仅在需要回滚时由获授权运维执行：恢复原 APP/前端，按私有 journal 条件回退
本轮新增能力并回收本轮验收凭证。保留 3047、队列和安全锁；不得重置数据库版本。
本次成功后未执行该命令。

## 验证范围与延期

PR #141 与合并后 main 的适用 CI 均成功，包括格式/vet/构建、Linux、RBAC、
PostgreSQL、MCP 并发/SSE、审计、原生解析恢复等；自动镜像发布工作流为 skipped，
实际生产镜像由上述受控 main 构建产出。[main CI](../integration/personal-mcp-production-20261113/main-ci.json)。
生产执行正常新增、并发重放、冲突与撤权等实际场景；后台任务故障恢复另由 Linux
隔离恢复验证和相应 CI 覆盖，没有对生产模型服务或队列注入故障。

按用户决定延期外部员工实时状态源和 ClamAV：仍实时核验本地账号、Key、成员和
编辑权限，保留文件大小/格式/包结构校验；本期不声称具备病毒特征扫描或外部停用
即时同步保证。后续补齐这两项并单独验收。
