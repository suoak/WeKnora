# SID 测试机环境检查（2026-10-12）

状态：SSH及主机Docker/目录/部署只读检查完成；SID部署、应用网络、真实登录和登出未执行。

目标：用户指定的Linux打包/测试机，SSH用户aqyfzx、端口22。主机地址保留在受控操作记录，不将完整主机目录/容器清单复制进仓库。认证密码由用户在交互式终端输入，Agent未读取、保存或输出密码。

执行时间：2026-10-12 23:28:22 Asia/Shanghai（15:28:22 UTC）。检查输出含AUDIT_END，SSH退出码0。此前认证拒绝没有执行远程检查；此次认证成功。

## 已验证结果

| 项目 | 结果 |
|---|---|
| SSH与工作目录 | PASS，进入用户主目录 |
| Docker客户端/服务端 | PASS，23.0.1，服务端API 1.42，Linux amd64 |
| Docker Compose | PASS，命令可用，报告版本v5.4.0 |
| Docker context | default；用户可读取容器列表 |
| 容器/项目状态 | 存在多套历史KnowHub烟测、分段测试及WeKnora运行实例；部分旧容器已退出，未读取其日志或尝试重启 |
| 最新可辨识烟测镜像标签 | KnowHub24-rc1，标签含854f3b95；这是标签证据，不证明实际代码提交或包含SID实现 |
| 正式SID基线专用实例 | 本次列表未见明确标识为SourceID/OIDC且对应8ae91bc44的实例；不推断未知标签的容器内部版本 |
| 端口 | 0.0.0.0及IPv6的80/8080已监听，并映射到现有WeKnora容器；另有测试端口映射。未扫描空闲端口 |
| 目录 | 用户ai目录已有多个构建/测试项目与Compose配置路径；没有新建目录或读取配置内容 |

执行命令仅为pwd/date、Docker版本/context、格式化docker ps、docker compose ls（项目名/状态/配置路径）、限定深度的目录名查找、ss监听列表。不读取env/Secret、Docker inspect、Compose展开配置、数据库、容器日志或浏览器资料。没有sudo、服务重启、镜像构建/拉取、容器创建、迁移、路由修改、身份绑定或SID开关变化。

## 环境边界与下一步

不能将该主机全部已有实例当作可替换的SID测试部署。80/8080已有服务，禁止覆盖、停机或借用其数据库；现有项目的生产/测试归属尚未逐一确认。本轮只读列举不代表取得修改授权。

建议后续独立测试方案：用户ai目录下新建专用SourceID测试目录、独立Compose project/network/volumes/测试数据库；固定8ae91bc44运行代码和匹配前端。端口先只读核验，再选未占用的loopback绑定端口。目录/端口名称只是待审阅方案，目前未创建或分配。

Client Secret仍由受控服务身份安全注入，不放入Codex工作目录/进程环境；不开SID登录开关。配置/日志门禁和正式契约继续适用。正式knowhub域名进入隔离实例的路径尚未证明，需要修改正式域名路由时必须先提交方案并等待用户批准。

未完成：主机及未来应用容器到SID的DNS/TLS/正式元数据、实际回调与通知代理、专用实例版本/密钥注入/四开关、隔离库与审核用户、本地管理员入口、敏感日志验收。当前只证明主机可操作Docker，不能把此前Windows本机网络PASS移作Linux应用结果。

真实联调仍为NOT_TESTED/BLOCKED_EXTERNAL，READY_FOR_RELEASE=false。详见 [真实预检查](SOURCEID_REAL_INTEGRATION_PRECHECK.md) 和 [剩余阻塞](SOURCEID_REMAINING_BLOCKERS.md)。
