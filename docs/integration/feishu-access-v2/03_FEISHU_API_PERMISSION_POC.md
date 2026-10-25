# 官方接口权限矩阵与只读 PoC

2026-10-25 使用飞书开放平台官方开发文档检索工具读取下述原文。网页 SPA 无法提取正文时，以官方文档检索结果补足；没有依据第三方教程作权限结论。**接口文档支持 Token 不等于企业应用、人员和文档已实际获权。应用身份部分只读验证已通过；员工 A/B/C 真实账号验证仍 BLOCKED。**

## 文档矩阵

路径省略 https://open.feishu.cn/open-apis；T=tenant_access_token，U=user_access_token；Scopes 列列出只读方向或可选权限，最终仍须按发布版本和实际授权 scope 验证。

| 操作 / 路径 | Token | Scope / 权限与限制 | 官方证据 |
| --- | --- | --- | --- |
| OAuth 授权码/刷新 | 换发 U | offline_access 必须配置、发布并在授权时请求；refresh token 单次轮换，过期或撤权要求重新授权 | [获取](https://go.feishu.cn/s/6D-Xh15yUPm)、[刷新](https://go.feishu.cn/s/6D-Xh15yQPm) |
| GET /drive/explorer/v2/root_folder/meta | U/T | drive:drive.metadata:readonly 等；T 不代表某员工的个人根目录 | [根目录](https://go.feishu.cn/s/63sbd2GBo03) |
| GET /drive/v1/files | U/T | space:document:retrieve 或 drive:drive:readonly / drive:drive；只当前层；省略 folder_token 的根列表不支持分页/快捷方式；应先取 root token 再显式分页，最大 page_size 200 | [文件清单](https://go.feishu.cn/s/6aCQJ_55A0s) |
| GET /wiki/v2/spaces、/:space_id/nodes、/spaces/get_node | U/T | wiki:wiki:readonly 等；仅当前身份可见空间/节点；不能据此假定底层 obj 内容可读 | [空间](https://go.feishu.cn/s/65W4PEw0U04)、[节点](https://go.feishu.cn/s/65W4PEw0I04)、[解包](https://go.feishu.cn/s/65W4PEw1g04) |
| POST /drive/v1/metas/batch_query | U/T | drive:drive.metadata:readonly 或 drive:drive，逐项处理失败；本地 resolveResource 已使用；此 POST 不改变文档；敏感字段另需相应 scope | [元数据](https://go.feishu.cn/s/6atKEsFAI0s) |
| GET /docx/v1/documents/:id、/:id/blocks | U/T | docx:document:readonly 等；调用身份须有对应文档访问权限；blocks 分页，元数据成功不代表正文/媒体读取成功 | [Docx 概述](https://go.feishu.cn/s/68niwzFhM0s)、[所有块](https://go.feishu.cn/s/68niwzFig0s) |
| POST /drive/v1/export_tasks，GET /:ticket，GET /export_tasks/file/:file_token/download | U/T | docs:document:export 或 drive:export:readonly；ACL/下载控制需单独验；Doc/Docx 支持 docx/pdf，Sheet/Bitable 支持 xlsx/csv；格式支持不保证内容完整 | [创建导出](https://go.feishu.cn/s/69xjRpRhA02)、[查询](https://go.feishu.cn/s/69xjRpRhE02) |
| GET /sheets/v3/spreadsheets/:token；/sheets/v2/spreadsheets/:token/values/:range | U/T | sheets:spreadsheet:readonly 等；范围读取上限 10MB，不支持部分跨表引用/数组计算结果；显示值、原值、公式语义须区分 | [单范围读取](https://go.feishu.cn/s/5_sDxkQ4g02) |
| GET /bitable/v1/apps/:app/tables；/:table/records；POST /records/search | U/T | bitable:app:readonly 或相应 base scopes；高级权限可造成成功但数据为空，需要核对字段/行范围和足够高级权限 | [查询记录](https://go.feishu.cn/s/6lY28723A04) |
| GET /drive/v1/permissions/:token/members | U/T | docs:permission.member:retrieve 或官方列出的管理类 scope；还受资源权限及协作者可见性设置约束；不能用查询结果替代实际内容读取 | [协作者列表](https://go.feishu.cn/s/6bld8Prm402) |
| POST /permissions/:token/members/batch_create 等管理操作 | U/T，受接口限制 | docs:permission.member:create 等；只有具备管理权的身份才可授权；不能直接给应用添加文件夹协作者后假定有效；官方建议包含机器人的群组及相应管理权 | [批量增加协作者](https://go.feishu.cn/s/6kccfWW5803)、[文档权限 FAQ](https://go.feishu.cn/s/5_sDxlNRQ02) |

官方当前检索结果把 OAuth 换发/刷新指向 **https://accounts.feishu.cn/oauth/v3/token**（form-urlencoded 推荐、兼容 JSON）；现有代码调用 open.feishu.cn 的 authen/v2/oauth/token。此为版本差异证据，**尚未证明 v2 停服**；需在测试应用上验证兼容性、返回字段/错误码后单独适配，不在本轮盲改生产路径。

现有 feishuScopes 含 drive:file:readonly、drive:drive.metadata:readonly，却未请求 folder-list 文档列出的 space:document:retrieve 等权限。新增浏览必须重新核对最小 scope 并要求用户重新授权，不能靠 OAuth 登录成功开放文件树。应用 API scope、资源 ACL、导出/下载权限、高级行列权限是不同门禁。

## A/B/C 验收方法及真实状态

准备独立测试资源，由有权负责人建立 A 所有者、B 仅阅读且不能管理、C 无访问权三类员工，并分别绑定自己的 KnowHub 测试用户。U 和 T 必须分开运行；T 没有“员工身份”，角色标签只描述该测试资源的员工关系，需单独记录应用 ACL。另准备禁止下载文档、撤权文档、开启高级权限的 Bitable、Wiki/Docx 同一对象的两个入口。

| 验证项 | A | B | C | 本轮真实结果 |
| --- | --- | --- | --- | --- |
| user_info 与预期 open_id 一致 | 必须 | 必须 | 必须 | BLOCKED：无批准测试凭据 |
| Docx metadata / blocks | 分别验证 | 分别验证，不能推导可导出 | 应拒绝；不能把限流/失效 Token 当 ACL 拒绝 | BLOCKED |
| 根目录、文件夹、Wiki 单页 | 仅本人可见范围 | 仅本人可见范围 | 自己根目录可读，不代表目标文档可读 | BLOCKED |
| Sheet/Bitable 行列内容 | 对照样本 | 高级权限下对照可见字段/记录 | 明确拒绝或符合官方策略的不可见结果 | BLOCKED |
| 导出/下载和禁止下载 | 单独验证 | 阅读权不保证下载权 | 拒绝 | BLOCKED，GET-only PoC 不创建导出任务 |
| 增加协作者/管理 | 仅有权者人工操作 | 不允许替其执行 | 不允许 | 本轮不调用管理 API |
| OAuth 刷新/撤权竞争 | 隔离环境测试 | 隔离环境测试 | 不适用权限提升 | BLOCKED，PoC 不消耗真实 refresh token |

## 已实现的最小探针

`poc/readonly_probe.py` 是离线默认、显式 --execute 才读取凭据的 GET-only 探针：固定官方域名、拒绝重定向、15 秒超时、1MiB 响应、最多 20 用例、顺序 2RPS、每项只读一页、不重试。先验证用户身份再读资源；同时检查 HTTP、业务 code 和操作对应响应结构。预期拒绝须显式列出经官方核对的权限错误码，不把 401、429、网络错误当预期拒绝。报告不输出 Token、open_id、URL、标题或正文，仅保存操作、fixture hash、HTTP/code 和结果。

探针 PASS 仅表示对应固定样本的一页 API 验证通过，**不是导入、导出、全量枚举、高级权限完整性或 OAuth 验收通过**。它不访问 KnowHub API、数据库、Redis、Asynq，也不创建导出任务或协作者。

执行前将示例复制到 Git 仓库外的私有 manifest，替换测试资源 ID；通过既有安全凭据工具设置 FEISHU_POC_USER_TOKEN、FEISHU_POC_EXPECTED_OPEN_ID，应用身份使用 FEISHU_POC_APP_TOKEN。不要把凭据写入命令、manifest 或提交文件。

```powershell
python docs/integration/feishu-access-v2/poc/readonly_probe.py <private-manifest.json> --execute --output <private-result.json>
```

离线 preflight 结果 [preflight-result.json](poc/preflight-result.json) 为 BLOCKED / execution_not_requested，它只代表该次未执行联网。

用户随后提供应用凭据后，使用 `poc/application_credentials_probe.py` 完成了真实验证。凭据经标准输入仅供进程内获取应用 Token；未写入仓库或报告。脱敏结果见 [live-application-probe.json](evidence/live-application-probe.json)：

| 真实调用 | 结果 | 证明范围 |
| --- | --- | --- |
| POST /auth/v3/tenant_access_token/internal | PASS，取得非空应用 Token | 凭据可用；不证明员工 OAuth 或具体文档 ACL |
| GET /drive/explorer/v2/root_folder/meta | PASS，HTTP200/code0，返回 token 字段 | 当前应用身份根目录元数据可调用；不是员工的私人根目录证明 |
| GET /wiki/v2/spaces?page_size=20 | PASS，HTTP200/code0，has_more=true | 当前应用身份首批空间列表 API 可调用；没有自动翻页，不代表全量发现或节点/正文可读 |

仍缺 A/B/C 用户独立授权、预期 open_id、Docx/Sheet/Bitable 受控样本清单、源侧 ACL/高级权限真值、禁止下载和撤权样本。应用其余 scopes 和导出可用性尚未验证；不能把取得 Token 当应用全部权限已通过。未读取具体业务文档正文、未创建导出任务、未变更协作者、未导入或修改 KnowHub 数据。
