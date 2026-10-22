# Go MCP V1 正式测试部署与main交付

## 管理摘要

2026-10-22，已完成获准打包服务器现有80/8080测试环境升级、3033→3037迁移、集中合成业务与安全验收。生产未修改，生产飞书仍保持停更，个人Go MCP生产入口未启用。真实模型与飞书云端凭据未获批，分别保留BLOCKED；没有把合成结果描述为生产验收。

最终部署源码SHA256：`bf199466752a3a1d75540355dc1fee9fb7f6bdef338c4ad0b62a49485b151661`；业务源码清单SHA256：`0a1b5cebe229246b4c646cccc4749f51aa97eb6aec9f5a5a2066f4153406463e`。Git基线为RC3 `e62b17e2d305358467821aeff1cce63ce1d61254`，当前开发工作树 `E:/code/WeKnora-worktrees/knowhub-v1-wiki-dynamic-20261022`。上述文件指纹不等于Git commit。

实际入口：`http://172.30.232.42/platform/mcp-access`，个人MCP：`http://172.30.232.42/api/v1/mcp/personal`。仍复用APP:8080和现有Nginx，无新增个人MCP服务或端口。HTTP入口仅用于获准测试，不声称生产HTTPS代理已验收。

当前必需部署/安全/业务门禁通过，可进入PR和CI审查。提交、PR、CI和main的实际结果见后续证据更新；未完成时不能称已合入。生产灰度和恢复飞书同步仍不可直接执行，需真实模型、获批飞书应用及来源撤权/积压评审。

## 最终材料

| 材料 | 实际值 |
|---|---|
| 同源源码包SHA256 | `67ee35ed5a80c8431ccdb14192929eabb52063d68db666797e99221b05af49cc` |
| APP Image ID | `sha256:954ca9badbf0470375419ab60d9a4cbbb78a2239d1d0b39a9856ef5b790eda47` |
| 前端 Image ID | `sha256:d1e031898dc48ae2a67a1b5207b957d20289c2bb41cf8fb00360dd834c557769` |
| 实际APP二进制SHA256 | `fdc4d9cfc3fad3fd678a58d5aba53328aff185461f789c1092358ae6f4d4aa50` |
| 数据库 | 打包服务器WeKnora-postgres内获准测试库WeKnora，3037:false |
| 硬化回滚镜像 | `sha256:d5c17d6b2d7c84ff463453a3f6809b4b921c97c621d83d3ce7f73d9fcd1fb1b8` |
| 原Python容器 | `590102ce51aab2d5950e42965202c44dbb407ef3e1e2f6675a10397bd5e224e0`，保持运行且镜像不变 |
| 原3033备份SHA256 | `51b1ef074c05570fcf49272881f3bfa452005064c246b93757497bcc9b78a4fc` |
| 数据盘受控备份 | `/mnt/sata0/knowhub-formal-c2-20261022t072133z-a3/private`，不入Git，不导出密钥 |

本地源码包 `E:/AIData/Codex/knowhub-v1-wiki-dynamic-bf199466752a3a1d75540355dc1fee9fb7f6bdef338c4ad0b62a49485b151661.tar.gz`。远端冻结目录 `/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/bf199466752a3a1d`。镜像本地构建，RepoDigest为空时只报告实际Image ID，不编造registry digest。

与原c2相比：先修复3037多页Wiki迁移去重（两份SQL+回归），形成0a7374候选；后发现完整前端样式守卫违规，仅将MCP页已有字号/圆角换成同值主题令牌，形成bf199466同源镜像。Go、依赖、迁移与已通过完整race的090216源码逐文件一致。原c2、0a、V1 5468、飞书补丁、b470及全部失败证据保留。

合入准备另外增加两个CI发布安全条件：KnowHub fork仅在明确设置`KNOWHUB_RELEASE_PUBLISH_ENABLED=true`时才允许发布Docker/PyPI；当前该变量不存在，发布默认关闭。APP/前端/Python测试与不发布镜像构建CI不受影响。此两文件不改变已部署APP输入，Git树与部署冻结材料的差异必须在提交Manifest中列明，不能伪装成完全同字节的源码包。

## 验收矩阵与证据

详见 [集中验收记录](FORMAL_TEST_CONCENTRATED_ACCEPTANCE.md) 与 `evidence/formal-shared-upgrade/` 各目录原始命令、日志、exit code、候选SHA。

| 项目 | 状态 | 主要证据目录/范围 |
|---|---|---|
| 正式迁移/备份恢复/原Key AES | PASS | backup-a3、deploy-a4；先恢复旧库再正式runner迁移；原数据不变，6条原Key解密及hash校验 |
| 同源APP+前端+入口/版本 | PASS | promote；最终二进制及双镜像一致；旧Python/冻结实例未覆盖 |
| 新最终APP八工具/只读/拒绝 | PASS | final-smoke；public/private union、partial、Wiki无写入、REST和未过期HMAC拒绝、撤Key下一请求401 |
| Portal动态发布/新增KB/索引文档 | PASS | business、portal-dynamic，实际管理API及真实PG；无逐KB审批；取消发布不影响私人授权 |
| dynamic_all/历史fixed/两员工 | PASS | business、auth-edges-a2；新增/撤销membership，fixed不扩权；tenant/platform机器及伪造Header拒绝 |
| 最终浏览器及配置 | PASS | browser-bf199466752a3a1d；实际SPA、无限期、加密reveal/rotate、HTTP复制粘贴、桌面与窄屏 |
| 最终Codex CLI八工具 | PASS | codex-bf199466752a3a1d；复制UI TOML，环境变量Secret；八工具实际事件+敏感明确拒绝 |
| Wiki来源/状态/混合/ABA/无索引 | PASS | business、wiki-negative、final-smoke；源安全证明要求未放宽 |
| 飞书Wiki/Drive真实Worker状态机 | PASS（合成云端） | feishu-a2；更新稳定KID、未变化跳过、失败保旧、429/临时403/未知缺失、完成收据、可信本地删除持久拒绝 |
| HOLD单来源隔离 | PASS | feishu-a2、final-smoke；问题来源拒绝，健康search/read/Wiki/REST继续，partial可见 |
| Worker崩溃、重复任务、Redis故障 | PASS（明确注入） | worker-fault-a2；私有恢复库/Redis探针，不停止共享Redis；generation=failed:2:false；终态后真实显式重试恢复；非自然30分钟等待证明 |
| 硬化回滚及返回新最终版本 | PASS | recovery-a3、final-safe-rollback-approval；相同未过期链接、REST、历史引用、功能关闭均拒绝；保留3037/授权指纹；不运行不安全旧APP |
| Go完整/race/vet/build/正式迁移 | PASS | development-090216及extra-090216；ci-preflight另含默认tag完整Go/vet和desktop测试；修订只改CSS和发布CI，Go输入一致 |
| 全前端/类型/构建 | PASS | polish：1585 PASS、0 FAIL、1既有SKIP；bf199构建全部14命令0；未放宽守卫或授权断言 |
| PG/ParadeDB规模/维度 | PASS（合成模型） | compatibility-a2非规模项+a4完整规模；128～1001篇四模式及并发，无256截断；1024请求和错误维度/模型失败安全拒绝 |
| Python完整+新版SDK | PASS | compatibility：55 unittest及实际router HTTP/SSE；feishu-a2实际新版weknora_api_key读取/拒绝 |
| 原Python实际服务代码 | PASS | legacy-python-a5：原镜像和原server SHA、双凭据SSE/HTTP，tenant full_access仍拒绝已撤来源；原容器未变 |
| OA/SID/Portal/个人飞书兼容 | PASS（源码回归）；真实云端NOT_TESTED | 全Go及全前端；OIDC/SID源码未改，沿用RC3真实登录证据，不借会落到生产的回调重验 |
| 正式模型凭据与质量 | BLOCKED | 凭据用途未批准；不调用未知生产凭据；合成维度/spy不代替正式质量 |
| 真实飞书云端/积压/移动撤权 | BLOCKED/UNKNOWN | 测试应用未批准；生产七来源未执行同步/游标写入，云端事件不可推定 |
| WorkMate会话tools/call | 历史FAIL，延期 | 按指令不继续排查，不是V1/Codex交付前置 |

## 生产只读结论保留

17个有效已发布空间、47个有效document KB。历史48与当前47差异为1个KB及其3篇文档软删除；不恢复、不读取无授权正文，不推测删除操作者或动机。7个active旧来源为2 Wiki、5 Drive，真实积压数量与权限事件UNKNOWN。生产Schema3033 clean、飞书停更=true保持原状。

## 上线及回滚建议

可以申请真实测试员工/应用/模型资源下的正式业务评审；不能直接申请“所有来源已安全恢复”。按顺序完成：获批模型的真实1024调用/质量和历史维度兼容；获批飞书云端删除/移动/撤权及积压恢复证据；生产副本迁移与受控HTTPS代理/员工灰度；在独立上线批准后分别启用MCP与恢复旧来源，未知来源继续HOLD。

默认回滚是关闭个人MCP并切换已验证硬化镜像，保留Schema3037、相同AES/签名密钥和持久拒绝事实；不能直接down3034～3037或恢复未撤权的旧DB快照。生产未授权执行任何步骤。

## PR/CI/main结果

等待本轮最终提交树核验、PR及CI。此节由实际结果更新，不以源码自审代替GitHub检查，也不将创建PR写成main已合入。
