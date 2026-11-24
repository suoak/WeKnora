# KnowHub Office 加密上传拦截测试报告

本报告交付可审查的开发候选。当前不满足发布或合入条件：同步来源专项评审、Linux 业务回归、真实 WPS 样本和指定测试环境验收尚未完成。已通过的本地检测与浏览器测试不能代替完整验收，更不能视为生产验收。

## 基线与上游对比

- 工作分支：`feature/office-encryption-guard`，独立工作树 `E:/code/WeKnora-office-guard`。原工作区已有改动未被纳入或覆盖。
- 开发基线：最新拉取的 `suoak/WeKnora main`，`50f82ae496d78238c0af7332739e7a9d3a49dbb1`。
- 上游对照：已成功拉取 `Tencent/WeKnora main`，`71c32af9273482e0dad013d0e3bea2f4d719516c`。首次拉取出现 TLS 连接失败，重试成功；未关闭 TLS 校验。
- 两条 main 已分叉。上游 `98fb3f91c` 的事务式文件替换、`79298a358` 的同步解析错误返回，以及当前创建/数据源实现已纳入对照。保留本分支已经具备的替换补偿、个人飞书隔离、Personal MCP admission 与来源身份逻辑，没有整体覆盖为上游实现。
- 检测器为本次新增；上游差异没有可直接替代本次前后端预检的统一模块。

## 调用链与接入范围

| 入口 | 现有链路与本次处理 | 边界 |
| --- | --- | --- |
| 知识库文件选择、目录选择 | `KbUploadSourceDropdown` → 确认框 → 上传任务 → `uploadKnowledgeFile` | 选择后检测并局部跳过；确认框覆盖拖放/追加；API 封装再次校验 |
| 批量上传 | 上传队列逐文件请求同一 HTTP 接口 | 不新增整批失败；明确加密文件不发请求，其他文件继续 |
| HTTP 文件上传、CLI、Python MCP | `/knowledge-bases/:id/knowledge/file` → `CreateKnowledgeFromFile` | 服务在已有知识库权限检查后、哈希去重更新/保存/建行/入队前强制检测 |
| 文件替换 | `ReplaceKnowledgeFile` | 检测位于新文件保存、旧任务取消、旧行更新与重解析之前；不更改补偿和删除顺序。当前前端没有独立替换上传 API |
| Personal MCP | `upload_document` → `PersonalContent.Upload` → 创建服务 | base64 解码和授权后检测；保留原格式白名单、主动内容限制、扫描器、幂等锁、入锁后二次授权。工具返回稳定结构化错误码 |
| 聊天附件 | `AttachmentUpload` → 临时附件 API → `temporaryDocumentService.Create` | 选择后预检和 API 预检；服务在 SaveBytes、建行和入队前检测 |
| 旧版内联聊天附件 | QA → `AttachmentProcessor.ProcessAttachment` | 在 SaveBytes 前检测；原有会话授权和附件大小校验保持原位 |
| 个人飞书普通导入 | 下载、来源覆盖和身份复核 → 创建服务 | 通用创建入口受保护；更新禁用、来源签收规则保持现状 |
| 飞书个人原文首次发布、legacy staging | `feishu_personal_first_create.go`、`feishu_legacy_staging.go` 可直接 SaveFile | 绕过通用创建；本次只做影响分析，未改暂存、代际、签收和发布流程，尚不构成全覆盖 |
| Confluence、Notion、GitLab 等同步 | `DataSourceService.ingestItem` → 删除旧版本 → 创建服务 | 创建会拒绝，但旧版本先删的风险必须解决后才能发布；见专项评审 |
| 只有 URL 的导入 | 建立知识行/任务后受控下载和解析 | 入队前无原始字节，不能承诺入队前检测；未新增下载，不绕过 SSRF 或来源授权 |
| FAQ、图片、模型、技能上传 | FAQ 数据导入与文档源文件不同；图片专用入口、模型文件和技能包有各自限制 | 不扩展这些入口的格式或执行权限；Office 检测不替代各自安全校验 |

同步影响与具体前置校验方案见 [同步来源专项评审](office-encryption-sync-review.md)。当前统一服务校验对同步的影响尚未获评审结论，因此本候选不可直接部署。

## 修改清单与检测策略

`internal/officeguard` 提供无网络、只读的检测和校验 API；`frontend/src/utils/officeEncryption.ts` 提供浏览器检测、同一 File 对象的结果缓存、上传前断言和批量筛选。两端共用测试清单及固定真实格式样本验证结果一致性。

- DOCX、XLSX、XLSM、PPTX：解析 CFB 的活动根目录、`EncryptionInfo`、`EncryptedPackage` 及版本信息识别打开加密；不会仅依据 OLE 文件头拒绝。ZIP 格式检查目录及加密标记。
- DOC：读取 WordDocument FIB 中的 `fEncrypted`，覆盖 XOR/RC4 类型标志；修改密码标志单独归类。
- XLS：读取 Workbook/Book 的 BIFF 记录，区分 FILEPASS 与 PROTECT/WINDOWPROTECT；只扫描有上限的前缀。
- PPT：检查 Current User 记录与加密 headerToken；未识别的旧格式返回 unknown，不声称覆盖全部历史 PPT 变种。
- OOXML 的文档修改保护、工作表/工作簿保护单独归类，不按打开密码拦截。保护标志分类不等于验证密码有效性或验证文件可完整解析。
- 结果分为 encrypted、protected、readable、invalid、unknown、not_office。结构错误不冒充加密；readable 仅表示未发现打开加密，不保证文件未损坏。检测不是全文件完整性验证。
- 内容嗅探独立于扩展名和自定义展示名称，改名为 TXT 不能避开已识别的加密容器。
- HTTP 明确加密返回 400，业务码 `2400`（`ErrOfficeEncrypted`）。Personal MCP 的错误文本为 `OFFICE_ENCRYPTED (2400)`，structuredContent 同时返回 `error.code=2400`；不泄露底层路径或原始业务错误。
- 中文提示说明设置了打开密码，并提示在 Office/WPS 移除密码后重试。不会向用户索取密码或尝试解密。

接入文件为 `knowledge_create.go`、`knowledge_replace.go`、`personal_upload.go`、`temporary_document.go`、旧聊天附件处理器与临时附件错误透传，以及前端两处 API、选择器、上传确认框和聊天附件组件。错误码定义在 `internal/errors/errors.go`。新测试位于检测器、知识服务、Personal MCP 与浏览器测试目录。另增加只允许手动触发的 `.github/workflows/office-encryption.yml`，不构建发布镜像、不部署。

## 测试证据

本地执行跨越 2026 年 11 月 24 日至 25 日。Go 使用本机已安装的 1.26.11，Node 为 22.22.1。前端复用本机已有 node_modules；独立 Linux CI 将通过 npm ci 复核锁文件安装结果。

| 检查 | 结果 | 证据或限制 |
| --- | --- | --- |
| 前端检测与既有队列/确认框回归 | PASS，58/58 | [原始输出](office-encryption-evidence/frontend-regression.txt)；其中检测、批量筛选、超时和 API 断言共 41 项 |
| Go 检测器与种子测试 | PASS | [原始输出及微基准](office-encryption-evidence/go-detector.txt)，含 27 份合成结构样本、11 份真实格式样本、取消/读取预算及 fuzz 种子 |
| Chromium 真实组件和 API 封装 | PASS | [执行输出](office-encryption-evidence/browser.txt)；混合选择跳过 1 个加密文件、2 个其他文件继续；直接调用 API 封装伪装扩展名仍为 2400 且请求计数不增加 |
| 前端完整类型检查 | PASS，exit 0 | [执行记录](office-encryption-evidence/validation.json)；成功时无输出 |
| 前端生产构建 | PASS，exit 0，2 分 58 秒 | [构建尾部输出](office-encryption-evidence/frontend-build-tail.txt)；只构建本地静态产物，不发布，有大资源包提示 |
| Go 创建/替换无副作用、Personal MCP 真 SQL 测试、MCP 结构化错误 | 尚未取得可接受的执行通过证据 | 新增断言已落地；Windows 原生链接首次失败，不能将编译到某阶段算作通过 |
| 本地短时 fuzz | PASS，132173 次执行 | [执行输出](office-encryption-evidence/fuzz.txt)，2 个 worker，10 秒目标时长；不等于长期压力验收 |
| Linux race 与 Linux 业务回归 | 未执行 | 手动 CI 已准备；本机无可用 Docker/WSL 命令 |
| 真实 WPS 生成的文件、真实客户端 HTTP/MCP、测试环境完整验收 | 未执行 | 必须补充真实样本和指定测试环境 |

公开格式样本来自 MIT 许可的 [msoffcrypto-tool](https://github.com/nolze/msoffcrypto-tool)，固定提交 `bdb71ea5dfbdee2f3cd86f8852e74be7a6f15667`，来源、SHA-256 与许可证保存在 `internal/officeguard/testdata/vendor`。真实样本覆盖标准加密、Agile 加密、RC4 CryptoAPI、XOR 和正常旧版 DOC/XLS/PPT。合成 `wps-ooxml.docx` 只代表兼容容器测试，不是 WPS 产品验收证据。

首次 Windows 业务包测试失败于现有 MinGW/DuckDB 依赖 `cannot find -lrstrtmgr`；使用本机 Windows SDK 路径和局部链接参数的重试未完成，已停止，仍不计通过，见 [阻塞记录](office-encryption-evidence/native-blocker.txt)。未修改全局工具链，未禁用 CGO、扫描器或安全检查来获取假通过。浏览器测试初轮曾因测试拦截器误拦 `/src/api/` 源模块而失败；已修正为只拦 API 路由，最终 Chromium 执行通过。

自动化业务测试明确断言：创建拒绝时保存次数、数据库创建次数和入队次数均为零；替换拒绝时旧行与事件序列不变；Personal MCP 授权后拒绝加密上传，隔离 SQL 数据库记录数和实际文件目录不变。这些断言需在受支持 Linux 环境执行通过，尚不能据测试代码本身认定验收完成。

## 性能与安全影响

每次检测最多读取 4 MiB、8192 次底层读取，预算 750 ms；CFB 目录链、FAT、DIFAT、mini-FAT、BIFF 前缀和 ZIP 目录均有限制。只检查有限保护 XML，单项解压上限 256 KiB、累计 1 MiB，不展开整份文档或枚举暴力密码。前端批量选择顺序检测以限制内存并缓存结果；CFB 扇区缓存最多 128 项。Go 检测同步执行，不为超时遗留后台检测 goroutine；已取消的请求不继续存储。

本机小型合成样本 Go 微基准：加密 DOCX 约 2.56 μs/3816 B，正常 XLSX 约 3.30 μs/6640 B，正常 DOC 约 1.92 μs/3176 B。它们是内存 ReaderAt 的检测开销，不是实际磁盘、网络上传延时或生产吞吐结论。病态输入与大量并发上传仍需测试环境压测。

检测超时、超限或不支持格式返回 unknown，不向普通上传用户错误宣称文件加密，仍沿用原解析流程。此策略意味着无法对超限容器、专有 WPS/DRM 格式及全部历史变体保证识别；Personal MCP 原有更严格的格式限制仍生效。加密输入的资源限制与未知状态策略必须纳入验收。

未改动身份认证、知识库授权、共享来源授权、MCP Key 能力与隔离环境中间件；未扩大 URL 获取能力、沙箱读写路径、允许扩展名或主动内容权限。HTTP multipart 仍可能按既有上限临时缓冲到请求临时目录；“保存前拒绝”指业务源文件存储与知识行/任务，不是承诺服务端从未接收请求字节。未记录文件正文、密码或私有来源链接。

## 完整验收与回滚

验收需在指定隔离测试环境对普通账户、共享知识库账户及 Personal MCP Key 分别验证：正常文件完成上传/解析/检索；加密文件在浏览器无上传请求；直接 HTTP multipart、伪装扩展名、Personal MCP base64 均被拒绝且无业务文件/知识行/任务；批量中正常文件继续；替换后旧文件、索引、任务保持不变；无权限、撤权和隔离端点继续拒绝。同步需额外确认旧版本不丢失、失败条目不被 stale sweep 当成删除、飞书代际和签收未变化。真实 Office/WPS 各类型样本与并发资源测试需留存证据。

待同步专项评审与以上验收通过后，再创建 PR、复核 main 漂移、合入 main。生产部署单独确认；本轮没有推送分支、创建 PR、合并或部署。

本次无数据库迁移、无配置/凭证改动、无数据修复。候选回滚为撤销本功能提交并重新构建前后端；已部署后的回滚应按既有发布机制恢复前一版本前后端产物，保持原认证与安全网关。撤销会恢复加密 Office 文件可进入原解析流程的旧行为。被拒绝的文件不产生知识记录，无需删除或清理业务数据；同步旧版本保留策略仍须先评审，不能用回滚承诺补偿未经评审的删除。

格式依据：[MS-OFFCRYPTO EncryptionInfo](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-offcrypto/2895eba1-acb1-4624-9bde-2cdad3fea015)、[MS-DOC FibBase](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-doc/26fb6c06-4e5c-4778-ab4e-edbf26a545bb)、[MS-PPT CurrentUserAtom](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-ppt/940d5700-e4d7-4fc0-ab48-fed5dbc48bc1)。
