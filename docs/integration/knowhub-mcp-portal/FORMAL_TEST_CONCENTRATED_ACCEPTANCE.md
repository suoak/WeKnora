# Go MCP V1 正式测试集中验收记录

## 当前结论

现有打包服务器 `http://172.30.232.42` 的80/8080测试入口已由P3 RC3升级，正式测试数据库已由3033迁移至3037，dirty=false。原Python MCP容器、历史候选和备份均保留。生产未访问或修改，飞书生产停更没有解除。

当前部署候选 `0a7374bbbdbd8c4a802733811ad3ecbeb70293ba33b89eefa9bd05c36ecbd371`；业务源码清单 `090216bc1a7d0c547235df30eef6f425050b9260fadba9791329b6c267a24783`。这不是Git提交。正式测试发现的必要修复是3037多Wiki页面迁移去重，原c2失败和dirty恢复库保留。完整前端回归随后发现MCP页面新增字面量违反已有样式守卫，已仅改成数值完全相同的主题令牌；正在重新冻结统一候选，不能把旧镜像当成最终修订版本。

尚未创建提交、PR或合入main。下表是当前实际证据，不代表生产验收或最终合并批准。

## 已完成门禁

证据根目录 `evidence/formal-shared-upgrade/`；远端执行根目录 `/home/aqyfzx/ai/knowhub-formal-c2-20261022/20261022t072133z`。所有测试使用新建fm-账号、820xxx合成空间及测试知识，没有重置原账号、静默扩大历史Key或读取生产正文。

| 门禁 | 状态 | 实际范围与证据 |
|---|---|---|
| 备份/恢复3033 | PASS | backup-a3：数据盘受控dump、配置/AES、文件/Redis备份；恢复库原表数量及内容指纹相等；6条原加密Key可解密及hash验证 |
| 正式3033→3037/同源部署 | PASS | deploy-a4：维护入口503、旧APP/Worker停止、遗留连接0；先新恢复库后实际测试库；原数据不变；关闭403、启用未认证401；实际二进制校验一致 |
| 八工具与统一知识 | PASS | business：真实APP/80入口；两个公共+两个私人KB；普通员工不在公共空间成员；敏感/未发布/写工具拒绝 |
| 私人dynamic_all | PASS | business：Key创建后新增私人空间授权即时可见、撤权拒绝；另一员工与历史fixed Key不扩权 |
| Portal动态发布/新增KB/文档 | PASS | business、portal-dynamic：真实管理API发布/取消；相同Key自动发现；真实PG三模式检索新索引；不产生逐KB批准记录；私人授权不受取消发布影响 |
| Wiki八工具安全 | PASS | business、wiki-negative：slug消歧、缺索引无写入；draft/archived、未证明正文、ABA恢复、混合及缺来源拒绝；健康Wiki继续可读 |
| Key生命周期与主体 | PASS | business、auth-edges-a2：无限期、加密存储、仅本人reveal、轮换/撤销、停用账号即时拒绝、auth/me撤销拒绝；真实租户/平台机器Key和伪造身份Header不能调用个人MCP；retrieve-only旧Key不取得公共范围 |
| 实际浏览器 | PASS/待最终候选复验 | browser-0a7374bbbdbd8c4a最终成功attempt：实际SPA原入口、创建/复制TOML和JSON、reveal/rotate、1440及390px；HTTP真实execCommand复制/粘贴，不替换剪贴板API；先前脚本错误全部保留 |
| 实际Codex八工具 | PASS/待最终候选复验 | codex-0a7374bbbdbd8c4a：复制页面TOML、环境变量Key、真实八工具及敏感拒绝；另以事件正文证明Wiki/Drive更新canary均可读；不是仅看agent最终回答 |
| 飞书真实Worker/PG状态机 | PASS（合成云端） | feishu-a2：Wiki/Drive稳定KID、首次旧来源重放、未变化不重建、成功更新、500失败保旧、429保旧、临时403及未知缺失HOLD不删除、不推进Cursor；可信本地删除持久拒绝 |
| 单来源隔离 | PASS | feishu-a2：MCP read/list/keyword/vector/hybrid、健康Wiki/REST、Owner KB列表；HOLD内容不读，健康内容可用，明确partial/Warning299 |
| 旧匿名链接撤销 | PASS | feishu-a2：有效未过期链接200，版本切换及HOLD/撤权后403/404；示例签名密钥本来即被拒绝，仅增加独立测试签名密钥，原AES不变 |
| 备份恢复与硬化回滚 | PASS | recovery-a2：撤权库恢复、前进/硬化兼容/返回候选；相同未过期HMAC、REST、历史引用均拒绝；功能关闭仍拒绝；Schema3037、凭据范围/拒绝状态指纹不变；无down、无不安全旧APP |
| Worker与Redis故障 | PASS（明确注入范围） | worker-fault-a2：私有恢复库/无公开端口临时探针；真实SIGKILL、重复准入拒绝、旧generation fenced、稳定KID；私有Redis实际中断，旧索引/Cursor不变，终态后显式重试成功；共享P3/Redis未停止 |
| 原Python镜像/源码 | PASS | legacy-python-a5：原容器不变，从其实际server字节复制至无端口探针；新合成legacy tenant Key和传输Secret，SSE/HTTP均验证有效来源、已撤来源、已撤Key；full_access仍不能绕过来源拒绝 |
| 新Python完整及SDK | PASS | compatibility：55个Python unittest通过；实际router+HTTP/SSE SDK集成通过；feishu-a2还验证新版weknora_api_key模式 |
| PG规模、维度及模型错误 | PASS（合成模型） | compatibility-a2非规模项及a4规模：1024维、128/257/512/864/1001篇最后文档命中，keyword/semantic/hybrid/grep与并发；798/3584不匹配、模型故障拒绝；不能宣称正式模型调用通过 |
| Go基本质量 | PASS | development-090216bc1a7d0c54、extra-090216bc1a7d0c54：完整Go、race、vet、PG/race、SQLite/PG正式迁移、构建；ci-preflight还通过不带sqlite_fts5的Go全套/vet及desktop容器测试 |
| 前端全套 | PASS（补丁工作副本） | polish：完整父目录、未修改1586项测试断言；1585 PASS、1已有SKIP、0 FAIL；类型/构建结果收集中，随后冻结部署复验 |

## 原始FAIL与修复判定

- 原c2真实3037迁移：产品缺陷，PG/SQLite多页夹具均RED；三文件最小修复后完整正式链通过。禁止覆盖原FAIL。
- 初期备份/部署：已删除宿主挂载路径及Compose材料不全，保留原失败；从运行容器受控恢复必要绑定文件，没有恢复不安全旧APP。
- 初期浏览器：误要求新账号存在历史凭据区、HTTP无navigator.clipboard；修正测试前提，实际HTTP复制粘贴通过，页面安全断言不放宽。
- 初期旧HMAC：原示例密钥应被拒绝、夹具错误用resource id替代handle；独立测试签名密钥+真实handle后验证先200再撤销拒绝，没有去掉签名检查。
- PG兼容前几次：DSN/计划输出路径不满足既有夹具显式约束；保留FAIL，使用新独立PG与精确约定重跑全规模通过，没有改断言或清除失败库。
- Worker首次：误以为AUTO_MIGRATE=false仍运行启动清理。该配置实际依靠同步准入的stale heartbeat回收；修正测试调用次序，在另一新恢复库验证generation从1到2且旧执行失效。长时间无人重试时不会凭该测试推定已自动清理。
- 原Python首次：旧server使用传输Secret+X-WeKnora-API-Key双凭据，不转发user_mcp所需租户Header；后续使用新合成legacy tenant Key证明原协议兼容。未把旧server改造成新个人身份。
- 完整前端首次：新增样式字面量两项真实违规，采用既有等值令牌修复；其余失败为测试未挂载父目录文件，完整仓库工作副本重新运行通过。未提高守卫baseline。

## 明确BLOCKED / NOT_TESTED

正式模型及飞书真实云端凭据用途未经批准，均BLOCKED。生产七来源的真实积压、云端删除/移动/撤权事件仍UNKNOWN。本轮可信删除是合成来源的真实Owner本地API事件，不是飞书云端事件。429等使用受控HTTP服务，不是向真实飞书故意制造故障。

SID/OA代码未改，本轮按用户决定复用RC3既有登录验收；测试回调仍落到生产，因此不发起新版真实SID登录。Docx/Sheet/Bitable/OA/Portal的源码回归包含于完整Go及既有专项，真实云端端到端仍未验收。WorkMate会话tools/call历史FAIL继续保留，不是本次交付前置。

当前测试入口HTTP，不把Chromium测试权限或客户端配置描述为HTTPS已通过。正式生产HTTPS已有入口的最终代理、真实员工及模型/飞书授权资源仍需灰度评审。>2048文档KB拒绝，不静默截断；混合KB未标记敏感内容不受完整文件ACL保护，不声称M3全部完成。

## 合并与生产门禁

合成授权/同步安全、迁移恢复、硬化回滚、完整兼容检查是当前PR必需门禁。真实云端/正式模型因未授权保留BLOCKED：在默认关闭、生产仍停更、未知事件拒绝的代码合并范围内不伪装PASS，也不自动阻塞已获准代码评审；它们仍是生产模型/同步启用评审的必需证据。

最终修订候选、镜像和实际二进制尚须更新后复验；PR、CI、代码审查和main合并在门禁全部满足后执行。当前文档为过程记录，最终报告需给出实际提交和CI结果，不能以本记录代替。
