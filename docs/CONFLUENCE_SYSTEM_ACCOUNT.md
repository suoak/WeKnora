# Confluence 系统内置账号

Confluence 支持由部署方统一管理的系统账号。普通用户可使用该连接选择同步范围，无需填写或获知系统账号的用户名、密码。

## 用户行为

- 后端配置完成后，新建 Confluence 默认使用系统账号，只展示连接说明与地址。
- 已有个人连接不会被自动更换。可点击“使用系统内置账号”，测试并保存后切换；保存时移除该数据源原有的个人凭证及公开用户名字段。
- 系统账号的数据源配置只保存 `settings.credential_source=system` 标记，不复制系统账号凭证到每个数据源。
- 普通用户不能通过数据源凭证接口替换或删除系统账号。修改系统凭证由部署管理员完成。
- 同步使用系统账号在 Confluence 中的可读权限；WeKnora 的知识库管理与租户鉴权继续生效。

## 后端配置

仅给后端配置以下环境变量，勿放入前端运行时配置或 `VITE_*` 变量：

```dotenv
CONFLUENCE_BUILTIN_ENABLED=true
CONFLUENCE_BUILTIN_BASE_URL=http://conf.ruijie.work
CONFLUENCE_BUILTIN_USERNAME=<由部署管理员设置>
CONFLUENCE_BUILTIN_PASSWORD=<由部署管理员设置>
```

也可以用 `CONFLUENCE_BUILTIN_PASSWORD_FILE` 指向服务进程可读的 secret 文件，该项优先于密码环境变量。容器使用文件模式时，需额外挂载 secret 到容器，并使用容器内路径。

内网域名仍需加入 `SSRF_WHITELIST_EXTRA`，保留已有条目，只追加 `conf.ruijie.work`。不会关闭全局 SSRF 防护。

根目录 `.env` 已由 Git 与 Docker 构建上下文排除；`docker-compose.yml` 只向 app 注入配置引用，不包含凭证字面量。轮换环境变量后重新创建 app 容器。首次上线此功能需同时构建、部署后端与前端。

## 接口边界

- `/api/v1/datasource/types` 的 `system_account` 只含 `enabled` 与 `base_url`，没有用户名、密码或可还原凭证的内容。
- 测试连接只需提交 `credentials: {"credential_source":"system"}`。
- 用户请求中的地址、版本、用户名和密码不会覆盖内置连接。内置连接固定使用后端配置的 Server/DC 地址与身份。
- 系统账号被禁用或配置不完整时拒绝连接，不回退到请求携带的其他凭证。
- 系统账号的认证错误不返回 Confluence 响应正文；HTTP 重定向不能离开配置的 origin。
- 普通数据源凭证 PUT / DELETE 对系统账号模式返回 403。

## 本次配置与验证

提供的凭证仅写入被忽略的本地 `.env`，不写入源码、示例配置、文档、前端产物或接口返回。

2026-11-18 从当前环境向指定 Confluence 发起一次只读 `/rest/api/space?limit=1` 请求，返回 HTTP 200。验证未跟随重定向，也未记录响应内容或凭证。

当前工作区已配置好本地后端环境；这不代表线上 KnowHub 已更新，线上仍需应用环境配置并发布对应前后端构建。

已通过的本地验证：

- Confluence 连接器完整回归，包括固定目标地址、禁用时拒绝回退、secret 文件读取、错误正文隐藏和跨 origin 重定向拒绝。
- 数据源 DTO 脱敏与既有 RSS 兼容回归；系统账号凭证 PUT / DELETE 返回 403。
- 41 项前端与多语言回归，前端类型检查。
- 前端生产构建通过（本机使用单线程 Less 编译验证）。
- 4 项浏览器回归，包括新建内置连接、编辑时隐藏替换/删除按钮，以及原有分页、定位、虚拟滚动行为。
- 业务服务包编译通过。当前 Windows 环境构建完整容器依赖包时缺少 `sqlite3.h`，不能据此认定完整后端镜像构建已通过。

提供的账号密码未出现在 Git 变更或前端构建产物中。浏览器验收使用合成数据，不将真实凭证交给浏览器测试。
