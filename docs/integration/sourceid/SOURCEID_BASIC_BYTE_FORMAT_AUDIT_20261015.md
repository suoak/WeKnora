# SourceID Basic 字节格式专项核查

2026-10-15。仅运行版本只读核对、源码审计及纯合成 loopback Mock。没有真实 SID 请求，没有读取/解码/输出真实认证值，没有修改运行参数或生产服务器。

## 六项结论

| 问题 | 结论 |
| --- | --- |
| 英文冒号是否正确 | **正确**。SetBasicAuth 内部拼接 username + ASCII `:` + password，分隔字节 0x3A；不是中文冒号 |
| Basic 后空格是否正确 | **正确**。固定字面串 `Basic `，空格为单个 0x20；合成发送端和 Mock 接收端检查通过 |
| 是否双重编码 | **没有重复 Base64，也没有对 Header 组件重复 QueryEscape**。组件各一次 QueryEscape，再一次 Base64；正文 Encode 是另一输入，不构成 Header 重复编码 |
| 何时与 SID 原样 Basic 不同 | 组件中包含需要 QueryEscape 改写的字符时，例如 +、/、=、%、冒号、空格、非ASCII。两组件完全不变时等价，如字母数字和 `-_.~`。无需转义字符条件是充分且可直接检查的等价条件 |
| 是否足以修改模式 | SID最新确认了其成功请求 raw 构造；但真实有效凭据算法等价性仍 UNKNOWN，**不足以直接切换运行模式或判定401根因** |
| 等价时下一步 | 不应以切换Basic算法重复验证；转向凭据版本匹配、SID入口/网关认证和成功Postman请求上下文 |

Secret 本身若含冒号或内部空格，raw 解码后的对应字符是凭据组成，不是错误分隔或多余空格。无控制字符的合成输入没有CR/LF。Header 的Base64部分无空格、制表符或换行；已逐项验证，未输出具体Header或编码值。

## 运行代码证据

打包机 172.30.232.42 当前 app revision：`3be5d3f9268fe8819d66485a2b990c6237827e25`。

实际运行 `/app/WeKnora` SHA-256：`39f582b85040e408b8d00f73528aa763a4a7d21abbf524cc928edb17d564635e`，与部署验收二进制一致。这是程序文件摘要，不是任何凭据指纹。

构建源码及当前源码的 exchangeOIDCCode 均使用：

```go
req.SetBasicAuth(url.QueryEscape(cfg.ClientID), url.QueryEscape(cfg.ClientSecret))
```

Go 标准库本地源码可核对：net/http/request.go 的 SetBasicAuth 设置 `Basic ` + basicAuth(username,password)；client.go 的 basicAuth 拼接 `username + ":" + password` 后调用 base64.StdEncoding.EncodeToString。当前业务只有一次 SetBasicAuth，无后续显式 Authorization Set/Add；原诊断Transport只读，httptrace观察不改Header。现有真实诊断构造值数和实际写出值数均为1。

无观察到重定向或重试，诊断模式既有策略不跟随3xx。若未来出现其他Transport或代理改写，不能由本次代码审计保证；本次实际连接和Header计数未提供此类证据。格式通过仍不证明SID接受凭据。

## 合成测试与证据

新增可重复运行工具：`business-test/basic-header-format-audit.go`。11类输入：字母数字、URL-safe组合、+、/、=、%、Secret中的冒号、内部空格、非ASCII、字面百分号序列、Client ID中的加号。

每类分别构造 oauth_form 和 raw，两次独立 loopback 请求，共22次，Mock返回204。要求：单Header、准确Basic Scheme/ASCII空格、Base64一次解码与预期组件逐字一致、ASCII分隔符、无中文冒号、无Header额外空白/CRLF、发送后Header不变。失败会直接非零退出，输出仅类别和布尔结果。

测试只模拟 SID 最新明确的 raw 构造和现有 RFC 组件编码；不声称实际运行了管理员电脑上的 Postman。不将合成两次请求当作真实 code 的双消费实验；本工具不发送授权码或调用SID。

本地执行方式（使用现有本地Go，不安装或改变项目编译器）：

```powershell
$env:GOTOOLCHAIN='local'
$env:GO111MODULE='off'
go run docs/integration/sourceid/business-test/basic-header-format-audit.go
```

结果：全部断言PASS，22次Mock，真实SID请求0。普通安全字符两策略等价，其他列出的特殊字符不同；没有双重Base64或Header覆盖。

## 真实凭据等价性与条件性兼容设计

真实凭据等价性：**UNKNOWN**。当前已部署服务无内置的受控算法等价性检查。没有为本轮创建读取Secret的exec脚本，也没有读取DPAPI或private/sourceid.env，不能根据以前聊天内容代替当前有效配置计算。用户此前只确认有短横线、无反斜杠/首尾空白，不代表完整字符集无需转义。

如将来需要判断，可单独审查在服务持有凭据的内存环境内比较两种构造，输出仅等价性布尔值；不能输出值、Base64、长度、哈希或跨系统凭据指纹。本轮未实现或部署该检查。

仅当 SID 明确要求 raw，且等价性=false 已由受控流程确认，才考虑 SourceID 专用配置，例如 `SOURCEID_BASIC_ENCODING=oauth_form|raw`：

- 默认 oauth_form，保留现有RFC6749策略。
- raw 分支调用 SetBasicAuth(cfg.ClientID,cfg.ClientSecret)，不自己拼Base64、不改变ASCII冒号/空格。
- 仅 SourceIDEnable 时允许 raw；其他 OIDC Provider 忽略此SourceID专用参数，未知值应启动验证失败。
- Token正文、PKCE、nonce/state、签名、TLS、重定向和请求次数全部不变；先验证合成特殊字符及其他Provider回归，再另行授权部署/真实单次验证。

这是条件性实现提案，**本轮没有新增配置、修改业务代码、切换认证模式或部署**。标准实现不因SID原样Basic示例而被判为错误。规范依据：[RFC6749 §2.3.1](https://datatracker.ietf.org/doc/html/rfc6749#section-2.3.1)。

附件：[合成字节格式结果](evidence/basic-header-format-audit-20261015.json)、[运行版本只读核对](evidence/basic-runtime-identity-20261015.json)。
