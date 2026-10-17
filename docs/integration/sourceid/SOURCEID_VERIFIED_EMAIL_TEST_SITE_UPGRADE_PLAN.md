# 测试站点升级准备：APP-only，等待授权

仅172.30.232.42、knowhub-company-test；不操作生产。当前APP f40b1602f、Frontend832b6652a、DB3029。目标为经过CI的verified_email_policy候选APP与DB3030，Frontend不变。本文件不是实际升级执行记录。

候选源码：`5f988be51779f1a22bd9d027b7503fe6a8749f62`；镜像：`sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b`。四项CI全部通过。候选目录`/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51`，APP pin为`app-pin.json`，恢复pin为`app-rollback-pin.json`。当前基础Compose是`/home/aqyfzx/ai/knowhub-company-entry-20261017/green/compose.json`；批准升级时按基础Compose→候选APP pin顺序，仅对app执行`up -d --no-deps --no-build --pull never --force-recreate app`。不得混用历史pilot或旧默认项目pin。

网关当前环境标记为`company-test-f40-db3029`；批准部署后应随实际新版本验收更新为`company-test-verified-email-db3030`并验证，不能只为满足采集器而提前改标记。真实测试临时覆盖必须位于候选pin之后，结束时撤除，不改基础配置。

## 授权前交付物

候选完整commit、四项CI结果、离线构建来源/编译器/源码归档校验、不可变镜像ID和OCI revision；实际3029快照的3030隔离迁移收据；用户/权限/KB/文档/向量引用一致性；明确APP-only恢复pin。未经部署批准，不创建运行覆盖、不替换APP、不迁移当前库。

本次无需新Frontend构建：前端认证接口、Access-only结果、无Refresh及回调协议不变，前端源码与832基线兼容；Mock浏览器使用相同Vue登录/Portal逻辑验证。APP与Frontend运行revision不同须如实记录。

## 获得单独批准后的顺序

1. 再确认80/8080网关只指向company-test，实际APP/Frontend镜像及schema3029 clean；检查无使用中的认证事务和维护窗口，记录其他服务容器ID/卷/路由。
2. 冻结本测试站点新业务写入，创建最新受保护Compose/app环境/容器元数据备份；pg_dump一致性DB3029及同代文件只读打包，记录配对恢复位置。保留f40不可变镜像及当前Frontend，不依赖本轮预演快照替代发布前备份。
3. 审核3030只新增政策/会话元数据及异常索引，无用户/权限DML。使用已验证标准migrate仅up 1，检查3030 dirty=false；任何dirty或数据差异停止，不force跳版本、不删库/卷。
4. 在现有Compose顺序最后叠加仅app image的candidate pin，固定不可变镜像；保留原入口、DB/文件/Redis/DocReader连接，不拉取重建其他版本。只recreate APP；前端若缓存旧APP地址，只做必要的nginx解析刷新，不替换Frontend镜像。
5. 默认仍trusted_ownership；VERIFIED_EMAIL_POLICY_CONFIRMED=false、OIDC=false、protocol_only=true、contract/email/auto/JIT关闭、Session external、prompt=false、Refresh=false、oauth_form及AUTO_MIGRATE=false。其他门禁保留现场原值。不加载Synthetic政策，不把产品方案批准当真实业务窗口批准。
6. 只做无凭据80/8080、静态资源、config、无参数callback、app版本/健康及隔离合成回归；核对其他容器/卷不变、真实身份/企业Session无新增。验证新模式配置默认关闭及原密码/MCP/飞书兼容，不请求真实SID认证接口。

## 回滚

- 始终先关闭真实业务门禁，保存脱敏故障证据；按明确关联定向撤销verified-email Session与派生Access，不能批量清理其他人的会话。
- 仅用旧APP f40不可变image pin恢复APP及原受保护配置，保留Frontend832、DB3030、新审计表、oauth_form及全部卷。旧代码不支持新策略的业务继续默认拒绝；不恢复raw、不整体服务栈回滚。
- **应用回滚不执行3030 down**：已提供保留型down用于迁移兼容测试，会禁用新策略并撤销对应Session，保留绑定、审计和异常记录；本次发布回退仍应保留3030，只回退APP。不得删除真实账号、绑定或审计掩盖失败。
- 若确需DB恢复，必须另行批准并成套恢复同代DB+文件；先隔离/核对升级后业务写入差异，不能直接用旧dump覆盖新数据。

当前测试库和原知识库均保持现场运行状态。升级成功也不自动开启verified_email_policy或真实SID登录；真实公司政策与一次业务测试另行批准。
