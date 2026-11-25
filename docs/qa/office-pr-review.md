# Office 上传拦截：Draft PR 审查记录

2026-11-25，北京时间。本轮授权仅为代码审查、风险收敛、重新验证及 **Draft PR**；不自动合入，不修改测试站数据、安全锁或生产环境，也不部署新候选。

## main 与代码漂移

本轮 `git fetch origin main` 后，远端 `suoak/WeKnora main` 为 `50f82ae496d78238c0af7332739e7a9d3a49dbb1`，`git ls-remote` 核对一致。它是本功能分支祖先；审查起点 960d157c1 相比 main 落后 0、领先 21 个提交，没有新增 main 代码要合并，也没有合并冲突需要解决。创建 PR 前再次核对远端；若 main 变化，应重新检查，不将本结论外推到未来 main。

相对已验收业务 SHA `668d067ec75a2a351ace4f7f0797be77af2b24ae`，本轮业务修改包括不支持文件类型的 4xx 映射、读取返回后的预算/取消回检，以及双端加密版本组合的明确识别；另有回归、lint 格式/注释/显式忽略只读 Close 返回值及审查文档。上传、飞书、MCP、权限、替换的既有门禁没有放宽。

## 本轮修复与审查结论

- `.wps` 等不在既有文件白名单中的类型仍由服务返回 `ErrInvalidFileType`，白名单未扩大。文件上传 handler 用 `errors.Is` 将该 sentinel 映射为 **HTTP 400 / code 1000**，中文说明为“不支持的文件类型，请上传系统支持的文件格式。”不再包装成 500 / 1007，也不使用加密码 2400。
- handler 回归走真实 multipart、服务及错误中间件，保留生产路由的所有权/写授权 guards；覆盖 `.wps`/`.WPS` 的 400/1000，未授权仍为 403。服务回归验证拒绝时 SaveFile、数据库创建及任务入队计数均为零。
- Office 分类保持 `encrypted / drm / protected / readable / invalid / unknown / not_office`。只有明确 encrypted 或 drm 产生 2400；protected、unknown、invalid 不冒充“成功识别加密”。readable 仅表示未发现打开加密，不能证明可解析或容器完整。结构探测不进行解密或密码验证。
- 规范对照修复版本组合的误判：Standard 2/3/4.2、Extensible 3/4.3、Agile 4.4 才作为已识别的加密版本；2.3/2.4/3.4/5.4 等组合回落 unknown。新增双端版本组合样本/回归，不对各自版本号独立范围取笛卡尔积。unknown 仍须按既有策略处理，不能将此收紧误称为所有未来/WPS 加密格式已支持。

版本组合依据为 Microsoft [Standard](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-offcrypto/2895eba1-acb1-4624-9bde-2cdad3fea015)、[Extensible](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-offcrypto/a922e41e-63f2-4701-8521-7f5d221a7ce0) 与 [Agile 2.3.4.10](https://officeprotocoldoc.z19.web.core.windows.net/files/MS-OFFCRYPTO/%5BMS-OFFCRYPTO%5D.pdf)；并不声称完成密码/加密载荷完整性验证。
- CFB 不按 OLE 文件头直接认定加密；检测 live 根流及相应格式标记。DRM 要求特定 live IRM 路径、变换定义和 DRM transform 信息。ZIP/CFB 畸形、伪造/失效标记、旧版格式、修改/工作表保护、超限/取消均沿用双端样本与资源预算回归；预算耗尽回落 unknown。
- 本轮补充 ReaderAt 返回后的预算/取消回检，防止末次读取跨过预算或取消边界后仍报告明确加密。确定性测试覆盖“读取期间到期”及“最后一次读取取消”，取消返回 context.Canceled。750 ms 是协作预算，不是强行中断已阻塞磁盘系统调用的承诺；未引入超时后遗留的读取 goroutine，真实慢盘仍是未覆盖边界。
- 上传与替换：身份/知识库/写授权仍先于服务持久化；明确拒绝发生在保存文件、建行、入队和替换取消旧任务之前。替换服务没有站点调用方的限制继续明确，不新增未审查的替换 API。
- Personal MCP：Key 当前状态、账号/成员资格、来源范围、格式、扫描及锁内重授权均保留。MCP 错误只输出已知安全常量，不转发任意内部错误或路径。严格只读入口没有获得写权限。
- 同步：取得字节后的检测仍在旧版本删除、暂存及发布前；内容拒绝不作为源端删除，混合批次继续，保留来源身份、幂等、租约/代际/签收、撤权、安全锁和发布条件。飞书免测仅豁免真实站点同步，本 PR 保留全部预检及 SQL/Redis/原生回归。
- URL 专用下载任务尚未统一接入该门禁；unknown 继续既有流程，可能进入后续解析失败。不得宣传为“所有 URL/未知格式均可识别或完全禁止加密内容落盘”。

## 12 项验收对账

证据中的运行 SHA 保持 **668d067ec**，不改写为本轮新提交。测试站未部署本轮 4xx 修复；历史 unknown `.wps` 的 500 是真实记录，本轮新行为由最终候选 Linux handler 集成回归证明。站点证据及原文下载、数据对账、网络监听、资源与恢复日志见 [独立测试站验收](office-test-deployment/README.md)。

| 项目 | 已有站点状态 | 新候选/影响边界 |
| --- | --- | --- |
| 1 普通 DOCX/XLSX/PPTX | PASS | 解析、索引、逐文档检索及原文下载哈希一致；受控模型，不声称大模型质量 |
| 2 浏览器预检 | PASS | 打开密码/DRM 中文说明，上传请求 0 |
| 3 HTTP 绕过 | PASS | 原文上传/伪装扩展/channel 400/2400，无业务持久化；URL 专用下载不在该全覆盖声明内 |
| 4 Personal MCP | PASS | 结构化 2400、DRM 说明、扫描、Key 撤销 |
| 5 批量 | PASS | 四选二请求，两份允许文件完成解析/索引 |
| 6 替换 | PARTIAL | HTTP 同名加密重传快照不变；专门替换服务仅服务/SQL 回归，无站点入口可测 |
| 7 旧格式/WPS | PARTIAL | 非空 DOC/XLS/PPT 控制、普通 WPS XLSX 站点通过；22 份 WPS 普通/保护仅本地分类；真实 WPS 打开密码 DOCX/XLSX/PPTX 及专有格式 NOT_RUN |
| 8 修改/工作表保护 | PASS | 双端识别与站点正常解析/索引，不作为打开密码拒绝 |
| 9 权限/共享/撤权 | PASS | 普通账号、自有/Editor、Viewer/无关账号、共享撤销、MCP Key 撤销 |
| 10 只读 MCP | PASS | 正常及加密写入均被既有边界拒绝 |
| 11 同步 | 飞书 WAIVED；其他真实来源 NOT_RUN | 用户明确豁免飞书真实站点，SQL、Redis、原生联合证据继续保留；不是全来源现场验收 |
| 12 资源/未知/超时 | PARTIAL | 有界读、fuzz、race、18 请求/6 并发、两入口 multipart 取消通过；真实慢读盘/服务端超时和极端并发未执行。新候选 `.wps` 改为 400/1000，不称作专有格式支持 |

本轮公开来源检索未取得可核实 WPS 版本、打开密码生成方式且可合法复用的 DOCX/XLSX/PPTX 三类样本。保留既有真实样本哈希/分类矩阵，不拿其他软件生成或合成加密样本冒充 WPS 产品验收，不绕过 DRM。

## 最终验证与 PR

审查发现 12 份历史 CI 展示文本带有尾部空格/额外空行，导致相对 main 的 diff-check 失败。本轮仅规范化展示副本的空白，原始字节仍可从不可变历史提交取得，原始 CI ZIP 未改；[双哈希清单](office-encryption-evidence/display-normalization.json) 记录变换。没有放宽 diff-check 规则或改写测试结果。GitHub 对原始日志标记 generated 以便折叠，审查 Markdown 和测试源码仍正常展示。

本轮最终提交必须重新运行 `.github/workflows/office-encryption.yml`（Linux 业务、SQL、权限/MCP、race、完整前端、类型检查、构建和 Chromium），以及 `office-encryption-native.yml`（真实 ParadeDB/Redis/原生联合）。镜像构建由专用 CI 引用精确 checkout 同一最终提交，保留 CGO/原生库，校验源码 SHA、运行时链接、制品哈希及镜像 ID。

最终 CI 链接、实际验证完整 SHA 和镜像 ID 填入 Draft PR 描述与 CI 制品；审查文档不会用先前 668d067ec 的通过结果替代最终提交验证。不会在 CI 完成后再改业务代码却沿用旧 CI。未执行的全仓库检查、lint 或基线失败须单列，不勾选假通过。

## 合入风险接受与回滚

| 风险/未覆盖 | 需要接受或补齐的责任 | 合入前建议 |
| --- | --- | --- |
| 真实 WPS 打开密码及专有格式不足 | 产品负责人、技术负责人 | 接受有限兼容范围并明确用户说明，或补样本后再决定 |
| unknown 放行、URL 专用下载尚未统一门禁 | 技术负责人/安全审查者；产品负责人确认范围 | 明确是否允许该边界；若产品要求所有 URL/未知格式严格拦截，应作为合入阻塞并另行实现/验收 |
| 替换无站点入口、其他来源未现场验收、飞书豁免 | 技术负责人；产品负责人确认本轮上线范围 | 认可现有服务/SQL/原生证据和豁免，或补专项；不得移除既有安全门禁 |
| 极端并发、慢读盘/服务端超时未覆盖 | 技术负责人/运维 | 当前仅小文件冒烟负载，不作容量或大文件 SLA 承诺 |

建议先保持 **Draft / 不合入**。CI 通过只满足技术验证前提，不代替上述负责人对范围和风险的审查；本轮不自动转 Ready、不合并，生产部署仍须单独确认。

无数据库迁移、历史数据清理或安全控制变更。回滚需恢复配套前后端镜像，或按依赖逆序 revert 该功能；本轮不支持格式映射可独立回退，但会恢复旧 500。回退整项拦截会重新暴露加密上传和旧同步先删后建风险，须审查。测试数据、已保留旧版本、安全锁、游标和队列均不随代码回退删除或重置。
