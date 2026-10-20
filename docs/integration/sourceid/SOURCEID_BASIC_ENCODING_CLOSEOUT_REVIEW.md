# SID Basic 编码收口核查（2026-10-15）

结论：受控进程对本地当前用户 DPAPI 存储中的凭据返回 **EQUIVALENT=true**。建议经用户批准后，单独恢复 `SOURCEID_BASIC_ENCODING=oauth_form`；本轮没有更改打包机配置、连接任何服务器或发起真实 SID 请求。

## 证据与结论边界

- 用户提供打包机版本为 `2fcbcaaed`，当前配置为 raw。本轮未 SSH 核验运行实例，不把用户提供的状态写作现场检查结果。
- 静态比较：该版本与核查源码 `1b4e7dc797fb6dc0fb96ffae9c0a5c787b854cbb` 的完整 `exchangeOIDCCode` 函数相同；Basic 配置解析及既有 Mock 测试也没有变化。
- **EQUIVALENT=true 仅针对本机受保护存储当前包含的凭据及 Client ID=knowhub。** 没有检查其 SID 服务端有效性，也没有比较它与打包机当前进程实际加载的密钥版本。该结论不能外推至未来轮换后的密钥。
- 已确认此前 401 的关键原因是协议切换后密钥版本变化。raw 与 oauth_form 尚无证据证明是当时的根因或修复。

## 实现核查

| 项目 | 结果 |
| --- | --- |
| `oauth_form` | `SetBasicAuth(QueryEscape(clientID), QueryEscape(clientSecret))` |
| `raw` | `SetBasicAuth(clientID, clientSecret)` |
| 默认值 | oauth_form；raw 兼容分支保留 |
| 其他 OIDC Provider | `SourceIDEnable=false` 时强制选择 oauth_form，即使配置 raw 也不生效 |
| Header 字节结构 | 两种模式均一个 Authorization、Basic 后一个 ASCII 空格、凭据分隔符为 ASCII 冒号、Base64 一次 |
| 编码次数 | oauth_form 每个凭据组件 QueryEscape 一次，再由 SetBasicAuth 做 Base64 一次；不是两次 Base64 |
| 请求正文 | 两种模式共用原表单构造逻辑；不增加 client_secret、state 或 code_verifier，不改变请求次数 |

位置：`internal/config/sourceid_basic_encoding.go`、`internal/application/service/user.go:exchangeOIDCCode`、`internal/application/service/sourceid_raw_basic_test.go`。

[RFC 6749 §2.3.1](https://www.rfc-editor.org/rfc/rfc6749#section-2.3.1) 要求 Basic 的用户名和密码组件先按表单规则编码；[Go SetBasicAuth](https://pkg.go.dev/net/http#Request.SetBasicAuth) 本身只负责 Basic 构造，OAuth 组件编码由调用方完成。

## 合成验证

重新执行既有 `business-test/basic-header-format-audit.go`：11 组合成凭据、22 次 loopback 请求全部断言通过，正式 SID 请求为 0。

| 合成字符条件 | raw 与 oauth_form |
| --- | --- |
| 纯字母数字、未保留字符 `-._~` | 相同 |
| `+`、`/`、`=`、`%`、冒号、空格、非 ASCII | 不同 |
| 字面量 `%2B` | 不同；其中 `%` 被正常编码，不应把字符串内容当成已经完成编码的凭据 |
| Client ID 自身含需转义字符 | 也可能不同；实际 Client ID 固定为 knowhub |

`TestSourceIDBasicEncodingConfig` 本机重新运行 PASS。实际 Token 交换的 default/oauth_form/raw/other_provider_raw_ignored 四分支、200/401、正文不变、单次请求及脱敏断言，已有 [431226c3b 的完整 CI](https://github.com/suoak/WeKnora/actions/runs/38717758880) PASS 证据；本轮认证源码未变，未宣称新运行了 Windows service 测试或新的 SID 联调。

## 受控凭据检查

新增离线工具 `business-test/check-basic-equivalence.ps1`。独立、短生命周期、无 Profile 的 Windows PowerShell 进程读取既有二进制 DPAPI 文件，使用 CurrentUser、空 entropy 和 UTF-8，与既有保存程序一致。明文及两个完整 Header 只在 C# 运行内存中参与比较，不返回 PowerShell 变量或 Agent；不进行 Base64 解码，也不输出长度、哈希、密钥字符类别或任何认证值。

工具先完成合成编码自检，成功路径只输出 `EQUIVALENT=true/false`；失败只输出 `EQUIVALENT=UNKNOWN`，不输出错误对象。本轮最终输出为 `EQUIVALENT=true`。最初按文本格式读取时安全失败，随后仅修正工具以匹配原有二进制 DPAPI 格式；没有重写或轮换凭据文件。

DPAPI 限制为当前 Windows 用户，不能视为同用户进程之间的访问隔离。工具不生成明文文件、网络请求、Header dump 或凭据指纹。数组在 finally 清理；.NET 托管字符串不能保证立即擦除，因此使用短生命周期进程，不启用 transcript、内存转储或调试跟踪。报告只保存布尔结果，不保存凭据。

## 经批准后的单变量切换方案（本轮不执行）

1. 核对 172.30.232.42 当前 app 镜像、Compose 项目和配置注入来源，只读取必要的非秘密字段；不执行完整 env、compose config 或 docker inspect 转储。
2. 通过受控配置加载进程，对打包机将实际使用的凭据再输出一次编码等价性布尔值，不向 Agent 暴露值或指纹；凭据版本归属由运维/SID 管理员在受保护渠道确认。不得凭本地 DPAPI 结果直接宣称远端已加载同一版本。
3. 若仍为 true，受保护备份配置，仅将 `SOURCEID_BASIC_ENCODING=raw` 改为 `oauth_form`。不改 Secret、认证方式、Token Body、callback、scope、PKCE、nonce/state、Session、身份门禁、数据库或飞书功能。保留切换前 OIDC/诊断开关状态，不借机启用真实登录。
4. 该配置由进程启动时加载；修改 .env 不会自动改变现有容器。固定现有镜像 ID/digest，只重建当前项目的 app 容器以加载环境，不构建、不拉取新版，不重启 frontend、数据库、Redis、DocReader 等配套服务。安排可接受的短暂 app 中断。
5. 无凭据验收：80/8080 健康、前端 API 代理、`/api/v1/auth/oidc/config` 的 `sourceid_basic_encoding=oauth_form`、原 callback 和各门禁布尔值均符合预期。本轮不以点击 OA 进行验收。真实协议复验需单独授权，且只允许一笔新 code 事务；在身份写入前继续执行既有门禁。
6. 若等价性 false/UNKNOWN，停止切换，保持 raw，先确认 SID 的组件表单解码及凭据版本契约。若配置重载或无凭据验收失败，只恢复 raw 配置并重建同一镜像 app；不回滚阶段 B/飞书代码或数据库。

**true 的意义**：相同输入下两种模式产生相同 Header 字节，编码选择不会改变这部分请求。**不代表**已经验证 SID 能正确处理所有特殊字符凭据的 oauth_form；今后每次轮换需重新核查，不能为适配服务端自行预解码 Secret。

机器证据：[合成字节核查](evidence/basic-encoding-synthetic-closeout-20261015.json)、[等价性与范围](evidence/basic-encoding-equivalence-closeout-20261015.json)。
