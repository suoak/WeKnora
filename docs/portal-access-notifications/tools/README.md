# 独立测试实例工具

这些是本次验收的操作脚本，不是生产发布脚本。连接复用本机已有 DPAPI 加密凭据及固定 SSH 主机公钥；需要 Python、Paramiko 和已有 `E:/AIData/Codex/sourceid-ssh.py`。密码只在进程内解密，不放入命令行或仓库。

- `remote_readonly.py --mode production --output <新的私有文件>`：有超时的生产只读元数据核查。
- `isolated_site.py stage --source-root <独立 worktree> --sha <完整 SHA> --manifest <新的私有文件>`：上传准确的 Git archive 和已构建前端，校验摘要。归档导出显式关闭 `core.autocrlf`，避免 Windows 配置改变 Rust/TOML 等文本字节。前端必须提前从相同 SHA 构建。
- `isolated_site.py build|poll|deploy|seed|audit --manifest <文件>`：有限资源编译、新建隔离实例、创建真实测试账号和测试文档、只读复核。不会操作已有应用数据库。新数据库使用 TCP 就绪检查，避免命中初始化临时进程；恢复验证库从 `template0` 创建，避免继承扩展造成冲突。
- `forward_site.py --manifest <文件> --runtime <新的私有文件>`：将测试主机 loopback 前端转发到本机随机 loopback 端口，最长一小时。创建同名 `.stop` 文件停止转发，远程测试实例保留。

`runtime` 含测试账号密码，只能保存在仓库外的私有目录。浏览器测试不启用 trace/video，避免网络凭据进入跟踪包。设置 `PORTAL_REAL_RUNTIME`、`PORTAL_REAL_EVIDENCE` 后，在 frontend 运行 `npx playwright test --config tests/portal-notifications/playwright.real.config.ts`。每次完整流程使用尚无目标成员关系的申请账号；已通过的账号不能再次申请，不删除授权来伪造首次流程。

构建和部署会拒绝已有服务名称。遇到部分部署失败时保留数据、日志和备份，先检查自己项目的标签、镜像和数据库状态，再恢复本项目操作；不要使用 prune、force 或删除已有服务。测试主机默认网络池耗尽，脚本只在确认不与现有 Docker 网络及主机非默认路由重叠后，为新实例分配两个 `/28` 网络。

测试文档是明确标记的合成手工数据，仅验收内容授权与文档名称搜索；不调用外部模型，不代表企业 SSO、解析和语义检索验收。源程序、二进制、镜像、备份摘要和 schema 版本均记录在部署清单中。
