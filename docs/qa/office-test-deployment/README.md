# Office 独立测试站验收（2026-11-25，北京时间）

实际运行源码为 **668d067ec75a2a351ace4f7f0797be77af2b24ae**。上传专项的 43 个 HTTP/MCP/权限/资源断言、4 个真实浏览器断言及非空旧版 DOC 对照通过。12 项总清单仍有 PARTIAL / NOT_RUN，**不声明全面验收完成，不创建合并 PR、不合入 main、不部署生产**。飞书真实同步按用户本轮明确指示免测。

## 部署身份和隔离

- 通过既有 CurrentUser DPAPI 与固定主机密钥连接 `aqyfzx@172.30.232.42:22`，实际主机 `aqyfzx10`、UID 1000。未重试 root、修改 SSH、使用特权容器或宿主根目录/Docker socket 挂载。
- 新 Compose 项目 `knowhub-office-guard-test`，7 个服务：APP（含既有 Asynq worker）、Frontend、DocReader、PostgreSQL、Redis、ClamAV、受控模型 fixture。未修改现有 KnowHub Compose 项目及 80/8080 入口。
- 目录 `/mnt/sata0/knowhub-feishu-v2-builds/office-guard-4ffd7442d-20261125/site`。目录名记录最初候选，不代表当前运行 SHA；当前镜像/来源以 [部署证明](deployment-proof.json) 和 [容器隔离及健康证据](isolation-health.json) 为准。
- PostgreSQL 为新实例/卷 `knowhub-office-guard-test-postgres`，库 `office_guard_test`；Redis 为新实例/卷 `knowhub-office-guard-test-redis`，新密码与队列命名空间；文件、飞书暂存和 DocReader 临时目录均在本任务私有目录。没有复制旧 `.env`、OAuth、生产数据库、模型凭据或队列。
- `knowhub-office-guard-test-private` 是 internal bridge（`10.254.222.0/28`）；仅 Frontend 另接 preview bridge（`10.254.222.16/28`），发布 `127.0.0.1:39781`。宿主和 73 个既有 Docker 网络/路由核查无冲突后才分配。数据库、Redis 和 APP 保持内部网络。APP 的 internal-only 网络不提供可用宿主发布端口；初始 `39780` 配置不能当作可访问入口。
- 浏览器通过普通 SSH 的 localhost 转发访问 SPA；HTTP 用例经上述 Frontend 的真实 `/api` 代理；严格只读 MCP 通过新 APP 的内部地址测试，未给 Nginx 增加内部路由代理。
- 使用正常注册的 `office-owner`、`office-other`、专用知识库、正常组织共享及 Key 签发/撤销 API；系统管理员标记为 false，没有 SQL 提权、伪造企业身份或修改安全锁。OAuth/来源表为空，详见隔离证据。
- 专用账号凭据仅保存在新 site 的 `owner-account.json` / `other-account.json`（0600），不在报告或 Git 中提供密码/会话。人工访问可通过既有授权 SSH 转发 `-L 39781:127.0.0.1:39781`；不开放新的公网入口。
- 模型为新网络内的确定性 embedding/chat fixture，仅验证解析、分块、向量/BM25 与检索管道；不代表真实大模型语义质量。Personal MCP 启用真实 ClamAV，正常文本放行、EICAR 拒绝。未关闭 CGO、扫描器、SSRF、身份、权限、租约或来源安全锁。

| 组件 | 实际镜像 ID |
| --- | --- |
| APP | `sha256:7af8f3d50631fad66a9947cd349817ed840897986c60f6b0933df39d1931bf0a` |
| Frontend | `sha256:0497f445be12edc79fbde45cacd487e458878b87ff12c197694ea79075466ba5` |
| PostgreSQL / Redis / DocReader / ClamAV | 精确 ID 见部署及隔离 JSON；复用镜像字节，创建新实例/独立数据，不连接旧实例 |

## 12 项实际结果

每条原始站点断言包含 UTC 执行时间、完整运行 SHA、HTTP/结构化响应、耗时。北京时间为 UTC+8。主要验收在 2026-11-25 10:14–10:22 执行。不能将下表 PARTIAL 合并统计为 PASS。

| # | 场景 | 状态及证据 |
| --- | --- | --- |
| 1 | 普通 DOCX/XLSX/PPTX 上传、解析、检索 | **PASS**。3 份文件 completed、有 Chunk/索引且 canary 检索逐个命中；[鉴权原文下载](original-downloads.json) 的 SHA 与原文件完全一致。使用 Python Office 库生成有效文档，不称作 WPS 产品生成。见 [HTTP 验收](acceptance-20261125T021452Z.json)、[数据对账](final-data-reconciliation.json) |
| 2 | 浏览器选择后预检、无上传请求 | **PASS**。打开密码 DOCX、合成 IRM 及用户真实 DRM XLSX 均出现对应中文提示，上传请求数 0。见 [浏览器结果](browser/results.json) 与截图 |
| 3 | 直接 HTTP 绕过前端 | **PASS**。真实加密 DOCX/XLSX/DOC/XLS/PPT、TXT 扩展名伪装及 channel 伪装均为 HTTP 400，`error.code=2400`。DRM 同码、不同受控中文解释。拒绝前后 Knowledge、Chunk、索引及文件摘要一致 |
| 4 | Personal MCP 上传 | **PASS**。加密文件返回 `isError=true` 与 `structuredContent.error.code=2400`；真实 DRM 使用 DRM 说明。正常文本放行、EICAR 被实际扫描器拒绝、Key 撤销后 401 |
| 5 | 混合批量局部跳过 | **PASS**。选择普通 DOCX、编辑保护 XLSX、打开密码 XLSX、DRM 共 4 份，仅产生 2 个上传请求。两份允许文件均 completed 且有 Chunk/索引。见 [选择截图](browser/batch-selected.png)、[上传后截图](browser/batch-after.png) 与数据对账 |
| 6 | 文件替换拒绝后原文/索引不变 | **PARTIAL**。真实 HTTP 同名加密重传拒绝且旧文件/行/索引摘要不变。`ReplaceKnowledgeFile` 在候选中没有生产 HTTP/MCP 调用方，不能声称站点实际调用该服务；独立替换服务由 CI SQL 用例覆盖 |
| 7 | 旧版 Office 与真实 WPS | **PARTIAL**。DOC/XLS/PPT 正常/加密格式控制及公开普通 WPS XLSX 的站点上传解析通过；另补充非空 DOC，2 Chunk、2 索引并检索命中，见 [对照](legacy-doc-control.json)。22 份实际 WPS 元数据样本只执行本地双端格式检测。真实 WPS 打开密码与专有格式仍 **NOT_RUN** |
| 8 | 修改/工作表保护不误判 | **PASS**。修改保护 DOCX、工作表保护 XLSX 真实上传并完成解析/索引。浏览器批量中的受保护 XLSX 也通过。真实 WPS 本地探测 13 份 protected，没有归为打开密码 |
| 9 | 身份、共享授权与撤权 | **PASS**。普通账号自有库成功；无关账号 403；共享 Editor 正常上传、加密拒绝；Viewer 403；撤销共享后 403；撤销 MCP Key 后 401。均通过正常 API 完成 |
| 10 | 严格只读 MCP | **PASS**。同一写入 Key 对内部只读入口上传正常/加密文件均被严格协议边界 HTTP 400 拒绝，未解除入口限制；非用更高权限绕过 |
| 11 | 飞书/其他同步更新旧版本保留 | 飞书站点专项 **WAIVED**（用户明确无需本轮验证）；其他来源实际站点同步 **NOT_RUN**。独立来源/旧版本保留的 SQL、原生解析、真实索引及 Redis 安全回归 CI 通过，不能替代真实同步源验收 |
| 12 | 超限/超时/unknown/异常/并发 | **PARTIAL**。CI 有界读、畸形 CFB/ZIP、取消/耗时及 race 通过；站点 18 请求/6 并发正常及加密混合通过。畸形/unknown 记录真实结果；未知 `.wps` 为既有 HTTP 500 / 1007 unsupported file type。没有把 unknown 误记为打开密码；未宣称专有 WPS 支持。真实 HTTP 慢源/断连取消的端到端验收未完成 |

`plain.doc` 控制样本的 FIB `ccpText=1, fcMin=2048, fcMac=2049`，仅段落结束符；completed 且零 Chunk/索引不能当作非空 DOC 解析失败。对账中的扩展集合布尔值仍保留原结果，非空 Apache POI DOC 对照独立证明有效解析和检索。

## DRM 与真实样本

前后端新增独立 `drm` 分类：必须同时找到 live IRM 数据空间路径、匹配的变换定义与 DRM transform 信息；仅 OLE 头、孤立 EncryptedPackage、失效/伪造目录项均不足以认定 DRM。保持原 4 MiB / 8192 步 / 750 ms 检测预算。

浏览器、HTTP、MCP 对确认的 DRM 使用：

> 该文件受 DRM 权限管理保护，知识库无法解析。请在有权限的原系统中导出未受保护的副本后再上传。

结构依据：[Microsoft MS-OFFCRYPTO 数据空间定义](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-offcrypto/c04677c7-59dd-46aa-a892-edb88bbfa763) 与 [IRM transform 存储](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-offcrypto/dc6708bb-e852-44b1-acba-f74614155191)。没有建议绕过 DRM 授权或要求上传业务密码。

- 用户指定 XLSX：203,776 字节，SHA-256 `5f6b9ff1ba9ca08d6e99055f660078080f7098dc5751a0f55736059dfdbc6f4b`。已有 IRM 结构，前后端均为 drm；站点 HTTP/MCP/浏览器均拒绝。没有将其称为 WPS 打开密码样本。
- 授权 Downloads 中 33 份实际文件：22 份含 WPS producer 元数据（12 DOCX、8 PPTX、2 XLSX），检测为 9 readable、13 protected；另外 11 DRM，双端结果一致。见 [逐样本矩阵](real-sample-probes.json)。只提交哈希/格式/生产者元数据，不提交私人文件、正文或许可证。
- WPS producer 元数据可见 11.1.0.x、12.1.0.x、3.9.3.6359 和 WWO 构建信息；不是本机安装软件版本或新建样本的证明。真实 WPS 打开密码 DOCX/XLSX/PPTX、WPS 编辑密码产品生成流程、专有 WPS/ET/DPS 格式未取得有效样本，保持未验证。
- 公开普通 WPS XLSX 按既有来源和 SHA 验证，站点 completed、有索引；MIT msoffcrypto 公共加密/旧格式控制沿用已固定来源。新增 [Apache POI 非空 DOC](https://github.com/apache/poi/blob/trunk/test-data/document/simple.doc) 由 Git blob 固定并记录 SHA，Apache-2.0，仅保存在忽略的样本目录。

## CI、性能与失败记录

- [最终 Linux/前端 CI 45530472812](https://github.com/suoak/WeKnora/actions/runs/45530472812)：**PASS**。Go 检测 51、业务 225、权限/MCP 523、race 97 个 PASS 事件，0 FAIL/0 SKIP；前端 1,719 PASS、既有 1 SKIP、0 FAIL，类型检查/构建/Chromium 检查通过。
- [原生联合 CI 45530472495](https://github.com/suoak/WeKnora/actions/runs/45530472495)：**PASS**，真实 ParadeDB/Redis 与原生解析联合。精确事件统计见 [CI 摘要](final-ci-summary.json)。
- [最终镜像 CI 45530538647](https://github.com/suoak/WeKnora/actions/runs/45530538647)：**PASS**。精确源 SHA、CGO=1、原生库、运行时链接校验；预置词典和离线 DuckDB 扩展，未关闭解析器或降低安全检查。
- 站点并发：18 个请求、6 workers，9 份正常文本被接受、9 份加密文件返回 400/2400，合计 0.114 s，p50 26.31 ms、p95/max 62.24 ms。此为小文件冒烟负载，不能外推容量或大文件 SLA。
- APP cgroup 生命周期内存峰值 **245,473,280 B（约 234 MiB）**，限额 2 GiB；采样时 214.3 MiB、CPU 6.06%（采样值不是 CPU 峰值）。全部 7 容器无 OOM、重启次数 0。每服务内存/CPU/PID、日志 10m×3 均受限。
- 根分区实测约 150 GiB 可用（68% 使用），数据盘约 139 GiB 可用；与历史 98% 不同，不将外部容量变化归功于本任务。本轮没有继续删除镜像、缓存、备份或旧目录。Docker 数据、制品、持久化和日志均在数据盘，完整前后值见证明 JSON。
- 保留失败/复测：默认网络池耗尽→核查所有现有网络及路由后指定新 /28；首版缺 Jieba→使用模块校验后的只读词典及新镜像内置；隔离网络不能发布入口→只给 Frontend 增加专用 preview 网；DuckDB 离线下载失败→预置扩展；只读词典副本导致 CI 清理失败→修正临时副本清理，重新执行最终完整打包；制品资格检查错误使用 root+cap-drop→改为实际 UID 1000 验证；MCP DRM 提示错误→业务修复并完整 CI/部署复测；脚本响应层级/202/引导弹窗造成的断言失败均保存原日志，未当成产品 PASS。
- 全部 CI/操作/失败/复测原始日志保留在本工作树 `private/`（Git 忽略）；[完整性清单](log-integrity-manifest.json) 记录路径、大小、SHA。原始凭据、账号密码、Key、会话及样本文件不进入 Git。

## 数据恢复、PR 与回滚

测试库已真实 `pg_dump -Fc` 并恢复至同一独立 PostgreSQL 内的新 template0 数据库。首次使用默认模板因预装 paradedb schema 冲突失败，保留该失败及备份；复测成功，未删除或重建任何已有数据库。最终恢复点及对账见 `restore-proof-*.json`（Knowledge、Chunk、embeddings 行数相等）；恢复证明不代表生产灾备演练。

当前未创建 PR、未合入 main。真实 WPS 打开密码/专有格式、独立替换入口、其他来源真实同步和异常链路尚有边界，不把 43+4 个通过断言称作 12 项全面完成。本轮没有执行 main 合并或生产发布，也没有把生产部署作为后续自动步骤。

回滚仅需停止本项目：在上述私有 site 目录执行 `docker compose -p knowhub-office-guard-test -f compose.json stop`，保留所有新卷/文件/备份，不影响现有项目。需回退测试候选时使用保存的 `compose.json.ca1c058c0` / `compose.json.4ffd7442d` 及对应镜像 ID；保留当前镜像、两份前版镜像和恢复点，不执行 volume prune/down -v。功能代码回退到 4ffd7442d 会移除新增 DRM 识别/提示，不需要逆向数据库迁移；尚无生产部署需要回滚。
