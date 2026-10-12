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

本报告初次检查后，用户明确说明已有部署均为测试用途并允许覆盖。因此现有测试服务可作为替换目标；仍不能由此推定允许删除数据卷、读取秘密、调整正式域名路由、绑定真实身份或启用SID。80/8080已有服务，应按选定测试栈逐项替换，不能同时停止全部容器。

建议后续独立测试方案：用户ai目录下新建专用SourceID测试目录、独立Compose project/network/volumes/测试数据库；固定8ae91bc44运行代码和匹配前端。端口先只读核验，再选未占用的loopback绑定端口。目录/端口名称只是待审阅方案，目前未创建或分配。

接受覆盖授权后的优先候选为knowhub24-rc1-smoke测试栈：先核对服务名/资源/数据卷和SID出站，再准备精确版本的app及配套frontend。复用数据库前必须有受控备份与schema兼容性证据，默认不执行自动迁移；不直接借用其他项目的数据库或覆盖其.env。若条件不足，采用新的独立测试库，保留历史库不动。下一步补充检查仍为只读，本轮尚未重建任何容器。

Client Secret仍由受控服务身份安全注入，不放入Codex工作目录/进程环境；不开SID登录开关。配置/日志门禁和正式契约继续适用。正式knowhub域名进入隔离实例的路径尚未证明，需要修改正式域名路由时必须先提交方案并等待用户批准。

未完成：主机及未来应用容器到SID的DNS/TLS/正式元数据、实际回调与通知代理、专用实例版本/密钥注入/四开关、隔离库与审核用户、本地管理员入口、敏感日志验收。当前只证明主机可操作Docker，不能把此前Windows本机网络PASS移作Linux应用结果。

## 补充检查：23:37:43+08:00

本次从Linux主机直连sid.ruijie.com.cn:443：DNS PASS（1个地址）、TCP PASS、系统证书链/主机名TLS PASS，TLSv1.3，证书到期2027-02-05 01:53:26 UTC。首个解析地址、5秒socket超时、整个探测18秒上限，无HTTP请求/重试/发现路径枚举。**这是主机结果，不是未来应用容器的出站验证**。

磁盘489GiB，剩余约265GiB（43%已用）；内存约32GiB、available约28GiB；swap约8GiB、已用约4.6GiB。git/python3命令存在，Buildx v0.17.1可用；没有安装/升级工具。

knowhub24-rc1-smoke的五个Docker命名卷已确认：data-files、docreader-tmp、neo4j-data、postgres-data、redis-data。未读取卷内容、数据库或秘密。指定的两个源码候选目录没有返回Git基线，不能据此认定主机没有其他源码仓库。

服务名Label查询因Windows→SSH命令参数中的引号丢失报template解析错误，**该项未通过**；其他命令继续完成。SSH退出0只代表会话完成，不代表每个检查PASS。已将下一次只读命令改为编码传输，修复引号保真；尚未在服务器重跑该项。不得在未确定service名时执行Compose替换。

后续可先打包固定验证SHA的app/frontend，再核对service/数据与迁移兼容性。镜像构建不需要SID Client Secret，构建上下文只能来自固定Git提交；SID四开关继续关闭。还没有构建/部署动作。

真实联调仍为NOT_TESTED/BLOCKED_EXTERNAL，READY_FOR_RELEASE=false。详见 [真实预检查](SOURCEID_REAL_INTEGRATION_PRECHECK.md) 和 [剩余阻塞](SOURCEID_REMAINING_BLOCKERS.md)。
