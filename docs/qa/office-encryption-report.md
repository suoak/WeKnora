# KnowHub Office 加密上传拦截测试报告

更新时间：2026-11-25（北京时间）。工作分支 feature/office-encryption-guard。原工作树 E:/code/WeKnora-office-guard 保留运维证据；新增业务修复在独立 E:/code/WeKnora-office-drm 工作树完成，没有覆盖原工作区的其他改动。

**站点验收基线：668d067ec 已部署至新建隔离测试项目。上传专项 43 个 HTTP/MCP/权限/资源断言、4 个真实浏览器断言及非空旧版 DOC 对照通过；该版 Linux/原生 CI 和镜像构建通过，前端有既有 1 项 SKIP。12 项仍有 PARTIAL / NOT_RUN，飞书真实同步按用户指示免测。本轮已获授权修复 unsupported .wps 的 4xx 映射并准备 Draft PR，不合入、不重新部署或改动测试站数据，不部署生产。最终新提交 CI 与风险接受要求见 [PR 审查记录](office-pr-review.md) 及 Draft PR 检查。**

最新部署身份、镜像 ID、12 项逐项状态、真实 Office/WPS 样本、失败和复测、资源、隔离、恢复及回滚证据统一见 [独立测试站验收报告](office-test-deployment/README.md)。下文历史 CI 和基线不代表本轮已重新合并最新 main；运行源码 SHA 为 668d067ec75a2a351ace4f7f0797be77af2b24ae。

## 基线与上游对比

- suoak/WeKnora main：50f82ae496d78238c0af7332739e7a9d3a49dbb1。
- Tencent/WeKnora main：71c32af9273482e0dad013d0e3bea2f4d719516c。
- 已获取两端最新 main；origin/main 为功能分支祖先，无待吸收的 origin 提交。上游 98fb3f91c 的事务式替换和 79298a358 的解析错误返回已纳入对照；保留本仓库既有文件替换补偿、Personal MCP admission、飞书来源身份和隔离安全协议，没有整包覆盖为上游实现。
- 初版最后业务变更为 a22d7efce；本轮追加 ca1c058c0（DRM 独立识别与提示）和 668d067ec（MCP 仅放行安全常量说明），两次均重跑精确源码 CI。部署 668d067ec 的 APP/Frontend 镜像经 SHA/归档哈希/镜像 ID 核对。
- 为执行 Linux 验证，仅推送 CI 专用引用 ci/office-encryption-validation 和 ci/office-encryption-native-validation；功能分支尚未推送，未创建 PR。

## 调用链与修改清单

| 范围/文件 | 原因与结果 |
| --- | --- |
| internal/officeguard；frontend/src/utils/officeEncryption.ts | 有界 CFB/ZIP 检测，新增 DRM 分类；明确打开密码或 IRM 返回 2400、使用不同受控说明；unknown/损坏不冒充加密 |
| 知识上传选择器、确认框、聊天附件；knowledge-base 与临时附件 API 封装 | 选后预检、批量局部跳过；API 请求前再次断言 |
| knowledge_create.go、knowledge_replace.go、personal_upload.go、temporary_document.go、旧聊天附件处理器 | 在现有授权之后、业务文件存储/建行/入队或替换旧任务之前独立强制检测 |
| datasource_service.go、office_sync.go | 已有原文字节在查旧、DeleteKnowledge、HardDeleteKnowledge、创建新版本前检查；单条拒绝继续同批正常项 |
| feishu_document_failure.go；core/shared.go、personal_resource.go | 内容拒绝作为单文档失败；不降级成格式错误，不触发 blocks fallback，不当作删除/撤权 |
| feishu_legacy_staging.go、feishu_legacy_assets.go | 暂存主体及资产独立写入门禁 |
| feishu_personal_recovery.go、manifest.go、first_create.go | 授权下载后、manifest/SaveFile/claim 前检测；历史 manifest 读回再检查 |
| feishu_personal_exact_document.go、exact_consumer.go、exact_publish.go、exact_resume_images.go | 证明、解析消费、发布及图片恢复持久化/复用分别检查 |
| office_sync_postgres_test.go、feishu_source_pipeline_native_v2_test.go、office_native_safety_test.go | 真实 SQL、原生索引、Redis 安全控制器联合验证旧版本保留 |
| core/office_encryption_test.go、handler/office_encryption_test.go、MCP matrix | 游标重启重试、HTTP 绕过 400/2400、工具结构化错误码与 Viewer 拒绝 |
| .github/workflows/office-encryption*.yml；scripts/ci/office_* | 隔离 Linux 回归、真实服务依赖、必过项目断言、日志归档及只读环境探测 |

完整生命周期分析、实际检测顺序及各飞书写入点见 [同步专项评审](office-encryption-sync-review.md)。仅 URL 而未取得原始字节的来源，不承诺首次建行/入队之前识别；其专用下载任务仍走原受控下载/解析，本轮未改该生命周期，不能据此声称所有 URL 来源已全面接入。未为检测额外抓取或绕过 SSRF。FAQ、图片、模型及技能包继续使用各自格式和安全约束。

## 格式、分类与样本

DOCX/XLSX/XLSM/PPTX 检查有效 CFB 根目录中的 EncryptionInfo、EncryptedPackage 及版本标志；不凭 OLE 文件头拒绝。DOC 检查 WordDocument FIB 打开加密位；XLS 检查 BIFF FILEPASS 并区分 PROTECT；PPT 检查 Current User/headerToken。ZIP 检查目录及加密标记，有限读取 OOXML 保护 XML。

结果为 encrypted、protected、readable、invalid、unknown、not_office。修改保护、工作表/工作簿保护不按打开密码拒绝。readable 表示未发现打开加密，不保证全文完整。伪装 TXT/PNG 不能绕过内容检测。HTTP 明确加密返回 400、业务码 2400；Personal MCP 返回 OFFICE_ENCRYPTED (2400) 及 error.code=2400。中文提示要求在 Office/WPS 移除打开密码，不收集密码或尝试解密。

- 27 份结构样本：正常/加密 OOXML、旧 DOC/XLS/PPT、修改保护、工作表保护、畸形和未知格式。
- 11 份真实格式样本：MIT 许可 msoffcrypto-tool，固定 bdb71ea5dfbdee2f3cd86f8852e74be7a6f15667，覆盖标准/Agile、RC4 CryptoAPI、XOR 及正常旧格式。来源、许可证、摘要见 internal/officeguard/testdata/vendor。
- 一份公开问题单声明由 WPS 生成的真实 XLSX，内部 Application 属性含 WPS Office：Go 与前端均判为 readable。来源及哈希见 [WPS 探测证据](office-encryption-evidence/wps-public-probe.json)。仅从公开附件临时读取，未将无明确再分发许可的字节纳入仓库。
- 合成 wps-ooxml.docx 仅为兼容容器测试。真实 WPS 加密文件、WPS DOC/PPT 等矩阵尚未完成，不能声明 WPS 专项验收通过。

## Linux 执行及失败复测

Linux 使用 GitHub Actions Ubuntu 24.04，CGO_ENABLED=1，真实隔离 PostgreSQL 17/ParadeDB、Redis；未关闭扫描器、权限或安全控制器。原生任务从固定源码构建解析器，安全控制器通过实际 worker inventory、TTL 健康协议及 SQL fencing 工作；受控 HTTP 源和模型不等于真实飞书/模型服务验收。

| 运行 | 结果与证据 |
| --- | --- |
| 45442268879 | FAIL：首次飞书导入先触发 PDF 格式校验，未保留加密业务码；已前移授权下载后的检测 |
| 45443794219 | FAIL：HTTP 测试缺少生产编辑授权中间件；仅修复测试装配，未放宽权限 |
| [45445106918](https://github.com/suoak/WeKnora/actions/runs/45445106918)，0df0bbb77 | PASS：Linux 业务、权限、race、前端、Chromium、类型检查、构建。业务 182 个 PASS 事件、1 SKIP；权限 522 PASS 无跳过；race 91 PASS 无跳过；检测器 47 PASS 无跳过。PASS 事件含子测试，不当作独立用例数累加 |
| 同轮前端完整测试 | 1715 项：1714 PASS、1 SKIP、0 FAIL。跳过既有 human handoff 窄屏交互用例；Office 浏览器专项未跳过 |
| 45445116014 | FAIL：原生测试更新源版本后复用旧 revision-bound context，被一致性检查正确拒绝；改为正常授权重新绑定新修订 |
| [45447320601](https://github.com/suoak/WeKnora/actions/runs/45447320601)，a22d7efce | FAIL：历史 Redis 恢复用例未达到 needs_attention。先统一注册经过身份校验的 op.TaskType 并继续诊断；最终日志证明已消费，根因为缺表。新增 exact 恢复/拒绝测试全部通过 |
| 45448760638、45450125299 | FAIL：正确注册消费者仍未收到任务；后者实际检查确认 Redis 原任务为 pending。不能将前次消费者注册修正当作根因已解决；继续收集队列/消费者诊断 |
| [45451436177](https://github.com/suoak/WeKnora/actions/runs/45451436177)，913481a0d | FAIL：业务 223 PASS、1 FAIL，无跳过。诊断证明原任务已送达正确消费者；后续安全检查因 fixture 缺 embeddings 表失败并进入 retry，未达到预期 needs_attention。此前“未送达”的判断由最终日志纠正。完整权限 522 PASS、race 92 PASS，无跳过；前端全量通过但保留既有 1 SKIP |
| [45453083132](https://github.com/suoak/WeKnora/actions/runs/45453083132)，e39bbb055 | PASS：业务 224、检测器 47、完整权限/MCP 隔离 522、race 92 个 PASS 事件，均无 FAIL/SKIP。Redis 恢复 1.23 秒通过，保留实际队列诊断。前端 1714 PASS、1 既有 SKIP、0 FAIL；Chromium、类型检查与构建通过 |
| 45447320362 | FAIL：加密拒绝/旧索引快照断言通过后，流式执行代际检查发现测试缺少真实 syncLogRepo；已装配 SQL 仓储 |
| 45449901233 | FAIL：流式正常项发布后，既有 reparse drain 检查发现测试缺少配置；补齐真实 connector registry、KB service 和 handler config |
| [45451127083](https://github.com/suoak/WeKnora/actions/runs/45451127083)，6603ecd69 | PASS：原生构建及旧版本保留联合测试，14.11 秒、无跳过；实际 Knowledge/Chunk、向量/BM25、本地文件不变，正常后续版本继续，撤权仍拒绝 |

旧业务 SKIP 为历史测试强制专用 Redis 主机名，最新 CI 将该主机名解析到本次临时 Redis，并将真实队列恢复测试列为必须 PASS；没有放宽其隔离断言或 FlushDB 保护。新一轮增加 exact 恢复图片正常/拒绝/旧输出/过期 owner 回归。

原始 JSONL/文本下载见 office-encryption-evidence/linux-* 与 native-*；完整失败与通过轮次日志及运行元数据见 [CI 日志目录](office-encryption-evidence/ci-logs)，实际事件计数与归档 SHA-256 见 [机器可读证据索引](office-encryption-evidence/ci-summary.json)。45442178038 早期工作流缺少 pipefail 与必过断言，界面成功不能作为通过证据，未纳入通过结论。被后续提交取消的运行也不计 PASS。

原生与最终整轮 CI 的 SHA 不同，差异为后续测试 fixture、诊断和 CI 配置，业务代码均包含 a22d7efce 的相同修复。最后的报告/归档提交只增加文档、日志和已在本机执行的手动样本探测工具，不代表部署版本，也不据此重新宣称测试站验收通过。

Windows 既有 MinGW/DuckDB 链接错误 -lrstrtmgr、__stack_chk_fail 保留于 [阻塞日志](office-encryption-evidence/native-blocker.txt)。不修改全局工具链、不禁用 CGO 来取得假通过。此前本地前端专项、类型检查和构建证据保留，最终以 Linux 实际执行项为准。

## 旧版本及安全验证

SQL 测试逐项比较 Knowledge、Chunk、本地旧文件及索引快照；混合批次正常文件继续，拒绝不创建新知识或排队，不把内容拒绝当删除。原生联合用例另外比较实际向量/BM25 行，正常后续版本继续发布，撤权仍拒绝。游标测试验证失败项 seen ID、旧 edit time、失败列表和重启后重试。

Personal MCP 保留格式白名单、主动内容限制、扫描器、幂等锁、入锁后二次授权和 Key 能力边界；完整权限套件覆盖只读隔离入口。HTTP 测试使用真实 handler/service 与知识库授权中间件，但注入测试调用者，不等于测试站 JWT/MCP Key 验收。个人更新禁用策略不解锁，既有拒绝保持生效。没有修改生产部署、迁移、凭证或来源队列。

## 资源与性能

单次检测限制 4 MiB 读取、8192 次读取操作、750 ms；CFB 链与扇区缓存均有界。保护 XML 单项最多 256 KiB、累计 1 MiB；不展开全文。前端批量顺序检测并缓存同一 File，CFB 缓存最多 128 页。Go 同步执行，不为超时遗留检测 goroutine；请求取消返回错误，不继续存储。

超时、超限、未知变体为 unknown，普通上传继续既有解析流程，不能保证任意专有 WPS/DRM 或恶意超限容器都被识别。明确 encrypted 才返回 2400；invalid 不冒充密码错误。Linux fuzz 与资源单测通过仅证明已测边界，不等于高并发压力验收。

原本地内存 ReaderAt 微基准：加密 DOCX 约 2.56 μs/3816 B，正常 XLSX 约 3.30 μs/6640 B，正常 DOC 约 1.92 μs/3176 B，见 go-detector.txt；不是网络或生产吞吐指标。HTTP multipart 仍可能进入既有请求临时缓冲；“持久化前拒绝”指业务源文件、知识行和队列，不承诺 HTTP 服务从未接收字节。

## 测试环境专项验收（历史准备表）

以下是站点部署前保留的准备表，不是最新执行状态。实际 668d067ec 的 PASS/PARTIAL/NOT_RUN/WAIVED 见 [12 项实际结果](office-test-deployment/README.md#12-项实际结果)；本轮最终候选对账及新增 4xx 修复范围见 [PR 审查记录](office-pr-review.md)。历史条目不改写为新源码已做站点验收。

可执行步骤与证据要求见 [测试站执行清单](office-encryption-acceptance.md)，全部站点条目当前为 NOT_RUN。

仓库历史配置指向 172.30.232.42，但未据此假定环境安全。实际只读探测：首页和 /health HTTP 200，/api/v1/health HTTP 401；SSH aqyfzx 认证拒绝，不能核实容器、数据库、对象存储及来源任务是否隔离。没有执行上传、部署、来源任务或生产数据操作。见 [实际探测](office-encryption-evidence/environment-probe.json)。已请求提供获准的测试环境及现有认证配置位置，尚待回复。

| 用户要求 | CI/本地证据 | 指定测试站结果 |
| --- | --- | --- |
| 普通 DOCX/XLSX/PPTX 上传、解析、检索 | 检测样本通过；原生联合用例使用受控 Markdown 来源，不能替代三种 Office 完整解析 | NOT_RUN |
| 浏览器选择加密文件无上传请求 | Chromium 真实组件及 API 封装通过 | NOT_RUN |
| HTTP 绕过返回 400/2400 | 真实 handler 集成通过，包含改扩展名/伪装 channel | NOT_RUN |
| Personal MCP 拒绝 | admission/工具结构化错误码通过 | NOT_RUN |
| 批量正常文件继续 | 前端及同步混合批次通过 | NOT_RUN |
| 替换拒绝保留旧原文与索引 | 服务回归与 SQL 快照通过；原生联合结果以上表为准 | NOT_RUN |
| 旧 DOC/XLS/PPT、真实 WPS | 真实旧格式样本和一份普通 WPS XLSX；真实加密 WPS 矩阵缺失 | NOT_RUN |
| 修改/工作表保护不误报 | 双端样本通过 | NOT_RUN |
| 普通用户、共享知识库、撤权 | 完整权限回归通过 | NOT_RUN |
| 隔离只读 MCP 禁上传 | 完整 MCP 隔离回归通过 | NOT_RUN |
| 同步加密更新保留旧检索 | SQL 通过；原生联合结果以上表为准 | NOT_RUN |
| 超限、超时、unknown | 单元/浏览器超时与 fuzz；unknown 放行政策已明确 | NOT_RUN |

## PR、风险与回滚

本轮用户明确授权创建 Draft PR，不自动合入。最终提交重新执行 Linux/前端/原生/镜像 CI；实际结果和完整 SHA 在 Draft PR 描述及检查中保留。main 未合入，生产发布等待单独确认。

测试站认证、隔离、核心上传/权限/资源及恢复验证已完成，原始证据保留；真实 WPS 打开密码/专有格式、独立替换入口、其他来源现场、极端并发和真实慢读盘/超时仍有覆盖边界。unknown 放行、URL 专用下载任务尚未统一接入本模块，需产品及技术负责人明确接受或补齐后再决定是否合入；不得用 CI 通过替代范围审查。

无数据库迁移和数据修复。回滚须审核后按依赖逆序 revert 本功能提交，并重新构建配套前后端或恢复此前产物；不清除飞书安全锁、不重置游标、不删除保留旧版本。回滚将恢复加密文件可进入旧解析流程，以及普通同步旧版先删后建的风险。拒绝未创建业务记录，无需生产清理。

格式依据：[MS-OFFCRYPTO](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-offcrypto/2895eba1-acb1-4624-9bde-2cdad3fea015)、[MS-DOC FibBase](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-doc/26fb6c06-4e5c-4778-ab4e-edbf26a545bb)、[MS-PPT CurrentUserAtom](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-ppt/940d5700-e4d7-4fc0-ab48-fed5dbc48bc1)。
