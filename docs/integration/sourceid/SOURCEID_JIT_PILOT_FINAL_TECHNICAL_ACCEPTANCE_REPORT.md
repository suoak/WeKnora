# JIT 全员候选：独立环境最终技术验收

范围更新（2026-10-17）：以下是已完成的独立环境技术验收历史记录，实测数值不变。正式产品现为公司全员一次性开放，不设部门/组织/角色准入或线上人数白名单；公司级SID企业账号类型及完整获批精确邮箱域策略取代旧范围讨论，不新增组织字段校验。300–500人只代表旧容量规划，不代表全公司总规模。剩余真实阻塞以[最新READY报告](KNOWHUB_REAL_OA_BUSINESS_ACCEPTANCE_READY_REPORT.md)及[正式GO/NO-GO](SOURCEID_ENTERPRISE_RELEASE_GO_NO_GO.md)为准；本次不操作环境或重新执行已通过验收。

**结论：独立试点升级、迁移、Mock、容量复测及APP-only回滚通过；真实业务与生产开放仍为NO-GO。**

本报告记录本轮授权的独立项目构建、3029迁移、Mock、容量及回滚，不代表批准真实身份绑定或正式发布。日期按证据中的UTC和北京时间换算；历史09e6/3028报告保留为上一阶段记录。

## 部署来源和边界

目标仅172.30.232.42的 `knowhub-oa-sso-pilot`。APP源码 `f40b1602f69ce5d88e1e87dc3cefae36e9bde356`，镜像 `sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413`，OCI revision与同源码四项成功CI一致。[CI收据](evidence/enterprise-jit-final-ci-20261016.json)；[CI运行](https://github.com/suoak/WeKnora/actions/runs/38900142644)。

使用已有离线固定builder和Go1.26.7，没有更换构建依赖；CI使用Go1.26.8，二者不能描述为同一编译器版本。镜像包含WeKnora、sourceid-ownership及完整迁移资源。源包使用git archive导出指定提交，收据记录代码源包校验值，未加入真实身份或配置。

保留Frontend832b6652adf541892545a7eac8b68733f846ab95，镜像 `sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1`。`git diff 832b6652a f40b1602f -- frontend/src`为空。APP/Frontend revision不同，已按本轮明确要求验收保留，不伪称同版本。

默认80/8080项目、原schema79数据库和原文件卷不在替换范围。本轮没有真实SID authorize、Token、UserInfo调用、真实开户、归属apply或身份绑定。

## 数据库、备份与回滚

独立库 `knowhub_oa_sso_pilot` 3028→3029，标准migrate执行up 1，无force、down或跳版本。003029 up仅新增JIT来源表，user_id外键和issuer+employee/user/email唯一约束；不修改历史用户/角色/权限。未执行down迁移。

APP停止写入后重新备份：`/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f/private/before-upgrade`。数据库dump21684360字节，文件tar2570240字节，配置和元数据一并保留；目录0700，受保护文件0600。数据库与文件属于同一冻结窗口。

当前pin与回滚pin分别为候选目录中的 `jit-app-pin.json`、`jit-app-rollback-pin.json`。Compose顺序为原试点compose.json后接目标pin；不叠加旧09镜像pin覆盖目标。仅APP执行 `up -d --no-deps --no-build --pull never --force-recreate app`。Frontend只reload Nginx解析，不重建。

实际APP-only回滚、最终恢复及最终数据核验结果见本报告末尾证据索引。回滚保留DB3029、JIT表、归属/审核/审计，不恢复旧dump抹除业务数据。未来有真实JIT数据后应先关闭JIT与业务门禁并定向撤销这些用户的企业Session/Token，再回退APP；旧APP不会执行新开户逻辑，不意味着可以继续放行缺少审批的JIT来源账号。

## 账号和知识库

前后私有基线比较只输出布尔结果，不输出员工标识或其指纹。基线：users3、tenants4、tenant_members5、总KB2/有效KB1、文档3、全部chunks19786/有效非空chunks2717、embeddings2717、资源3及绑定3。

原user_id、tenant_id、管理员/跨空间标记、成员角色、KB ACL、文档/分块/向量引用和模型配置身份元数据均应保持。文件3个、2554991字节，逐文件在受控进程内比较，与原卷及备份一致；无文件原文或校验指纹输出。

候选与09旧APP在可丢弃3029副本中：原管理员本地登录/auth-me/KB读取通过，3份文档下载均200且非空；已有向量查询3命中、原文档引用保持、外部模型调用0。真实存在的另一空间KB由管理员可读，测试viewer无成员或共享，KB读取与空间切换均403。

BM25、外部Embedding、重排和完整RAG不在这次获准的原文外传/模型调用范围，不声明已验证。不存在的真实角色组合标为NOT_REAL_DATA_VERIFIED，八类合成权限矩阵由同源码CI和离线Handler回归覆盖。

## 业务Mock与容量

仅可丢弃数据库允许加载synthetic政策和Mock身份。正式独立库保持OIDC关闭、protocol_only=true、contract/email/auto/enterprise/JIT放行关闭、session=external、prompt=false、oauth_form、Refresh=false和AUTO_MIGRATE=false。

验收检查原账号首次/再次关联、JIT首次/再次开户、零默认空间/管理员权限、Access-only、无Refresh响应/Cookie/新增数据库记录、SwitchTenant合法性和绝对期限不延长、退出、多设备隔离与撤销。副本继承的24条已撤销历史Refresh不算本轮新增；首次容量验收错误使用全表计数而FAIL，原始记录保留，修正为定向新账号统计后重跑，见容量报告。Mock容器禁用Docker日志，不保存原始请求/响应。结束删除可丢弃数据库和容器，干净试点不得出现Mock归属/绑定/Session/JIT记录。

容量用完整APP+保留Frontend代理+TLS Mock SID的HTTP API链路，区分已有账号首次关联、JIT、混合、到期重登及SID模拟故障；不是浏览器渲染负载或真实SID容量验收。详细结果另见300/500容量报告。PostgreSQL连接和锁等待采样不能替代未导出的database/sql池等待计数。

## 真实业务准备与结论

SID DNS/TLS/Discovery/JWKS通过，非必要出站及未批准bridge直连拒绝。无凭据临时Chrome经18190→SSH→18090验证页面、静态资源、API和空参数callback；该证据不能代替日常有SSO的Chrome实际路由。现有hosts指向服务器80会进入默认服务，禁止据此开始独立试点真实认证。

没有权威历史归属批次或HR/SID目录接口，不能制造“真实批准数据”进行dry-run；当前只能验证合成批次流程。真实数据dry-run待负责人交付，apply、已有账号真实登录与新员工真实JIT分别需要独立授权。

正式一次性全员开放仍为NO-GO：缺正式归属与开户资格/同步契约、会话与普通SSO风险审批、生产容量/SID限流契约、实际SSO浏览器路由及两类真实业务验收。RJGH稳定唯一不回收、RJEMAIL可信属于当前员工均已确认，不重新列为阻塞。

## 最终执行证据

最终结论：**独立技术验收PASS；生产及真实业务放行NO-GO，等待独立授权与必需输入。**

- 构建/003029/健康/原数据完整性/旧09 APP兼容及实际回滚恢复：PASS。
- pilot与enterprise_all原账号Mock、JIT首次/再次开户及定向停用撤销：PASS。JIT用户没有合法成员时SwitchTenant为403；合法切换和不延长绝对期限由原账号用例验证，不能把JIT的拒绝当作成功切换。
- 300/500容量复测10场景：4300成功、100预期拒绝，无Code重复兑换，新增Refresh0；第一次全表Refresh断言失败已保留并解释。生产容量不作PASS声明。
- 最终DB3029 clean，原用户/角色/ACL/文件/2717条有效分块和向量引用保持；真实身份/企业Session/JIT记录0，业务门禁关闭。默认80/8080容器及DB3027、原库79保持。

[完整执行收据与首次失败记录](evidence/jit-pilot-final-technical-acceptance-20261016.json) · [300/500容量明细](SOURCEID_JIT_CAPACITY_300_500_REPORT.md) · [真实业务操作卡](SOURCEID_JIT_REAL_BUSINESS_ACCEPTANCE_OPERATION_CARD.md) · [GO/NO-GO清单](SOURCEID_ENTERPRISE_RELEASE_GO_NO_GO.md)

无凭据桌面/移动页面证据：[桌面](evidence/jit-release-login-desktop-20261016.png)、[移动](evidence/jit-release-login-mobile-20261016.png)、[四类请求路由](evidence/jit-release-browser-route-20261016.json)。实际有SSO的Chrome尚未验证，不要求用户提供Cookie、Token或HAR。

临时Mock/容量容器与数据库已移除，受保护的合成夹具和运行时测试env已定向清理；备份、构建收据和脱敏失败/成功证据保留。日常Chrome及其Cookie未改动。
