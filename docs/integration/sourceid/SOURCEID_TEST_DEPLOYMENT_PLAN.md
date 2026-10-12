# 测试环境替换与打包计划

用户已确认现有主机部署为测试用途并允许覆盖。此授权适用于测试服务替换，不代替真实员工登录/身份绑定/SID登出/正式域名路由/生产操作的独立确认。

## 固定范围

- 代码：8ae91bc4439dcc93be11844ada5ec1479f2730bc；app/frontend必须同一提交。
- 候选替换项目：knowhub24-rc1-smoke。实际service名需从精确Docker Label查询复核，不能根据容器后缀猜测。
- 保留旧app镜像、原Compose及所有现有数据卷。不得执行down -v、删除数据库、全机stop/prune或覆盖旧.env。
- 四开关OIDC_AUTH_ENABLE/SOURCEID_ENABLE/SOURCEID_LOGOUT_ENABLE/SOURCEID_CONTRACT_CONFIRMED保持false；打包不需要Client Secret。
- 正式callback域名路由不改；如果后续必须分流到测试实例，先提交具体方案等待用户批准。

## 当前阶段：只打包

远程构建脚本与交互式SSH启动器位于开发机受控任务目录，语法检查已通过，尚未执行构建。脚本先复核候选service和磁盘空间，再在用户ai目录独立build工作区获取公开仓库，无Git凭据helper；checkout精确SHA并用git archive生成新的构建上下文，不使用现有项目目录/env或数据卷。

镜像标签：knowhub-sourceid-test/app:8ae91bc44、knowhub-sourceid-test/frontend:8ae91bc44。沿用已存在KnowHub release配方，包括AnyDoc/BrowserSkill；只生成一份构建专用Dockerfile副本，将Go1.26.7改为与CI一致的Go1.26.8，匹配其版本断言/元数据，不改认证源码或仓库Dockerfile。Go基础镜像使用明确1.26.8-bookworm标签，构建结束记录镜像ID/digest；不能把标签等同immutable digest。其余配方原有digest/checksum校验保留，不关闭TLS。

构建有后端2小时、前端1小时的上限。构建后在无网络、无env/volume的临时容器中检查二进制、003024 up/down和frontend产物存在；不启动应用entrypoint或nginx、不连接数据库/真实SID。这些只说明包装完整，不能代替运行/协议测试。不会自动push镜像或替换运行中的容器。

## 镜像就绪后的替换条件

1. 镜像来源/工具链/配方摘要确认；精确service与原镜像回退记录；现有测试Compose由受控部署工具读取，不由Agent读取/输出其Secret或展开配置。
2. 对测试数据库取得schema/migration版本与受控备份证明，不能仅看health就认为新schema兼容。新AuthToken有session_id，若003024未应用，本地认证也可能失败；不能直接AUTO_MIGRATE=false替换后宣称就绪。其他未完成迁移亦须确认。
3. 如果复用历史测试库无法满足备份/一致性条件，使用新的独立测试库和volumes，旧数据保留不动。真实员工关联仍需审核dry-run差异和用户确认。
4. 在镜像、备份和兼容性均就绪后，只重建选定app及匹配frontend；其他项目不动。配套frontend可使用先核验的loopback端口，不自动抢占80/8080。
5. 验证应用启动/迁移状态、健康、四开关关闭、公开OIDC配置无Secret、本地应急入口及原业务；检查实际应用容器SID DNS/TLS（主机PASS不可替代）。不打印完整env、Docker inspect、应用敏感日志或Compose展开配置。
6. 敏感日志门禁未修复、正式协议未确认、测试绑定/域名未授权时，SID保持关闭。失败仅回退选定服务及配套frontend，保留数据库和撤销证据；不盲目down migration。

当前结果与剩余门禁见 [测试机环境报告](SOURCEID_TEST_HOST_ENVIRONMENT_REPORT.md)、[真实预检查](SOURCEID_REAL_INTEGRATION_PRECHECK.md) 和 [剩余阻塞](SOURCEID_REMAINING_BLOCKERS.md)。镜像构建、服务替换和真实联调当前均NOT_TESTED，READY_FOR_RELEASE=false。
