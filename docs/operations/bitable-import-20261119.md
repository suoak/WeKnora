# 飞书多维表格个人导入预检拒绝

2026-11-19（北京时间）。目标知识库 `99a09c4c-6de8-4b68-97a4-14d6246619a2`，目标文档 `c230eac6-cc24-4942-8066-24cfffbd60b1`。只读取生产记录及登记的原文件，在独立构建环境验证修复。未修改生产业务记录或重试预算。

## 定位

生产 APP 与前端 revision 均为 `6082f0bd26e09d3b1f05f50955ae7015da3236eb`。目标个人导入处于 `index_pending`，generation=3，最新错误类别为 `embedded_dependency_unsupported`，尚未进入本次文档解析执行。`FEISHU_PERSONAL_EXACT_STANDALONE_XLSX` 与 `FEISHU_PERSONAL_EXACT_EMBEDDED_XLSX` 均已开启。

原文件为 1,659,903 字节的 XLSX，MD5 与 Knowledge 登记值一致，共 21 个工作表。文件结构、生产解析代码及原文件重放确认存在以下拒绝条件：

1. 条件格式规则中的 `formula` 元素被通用依赖预检无条件拒绝；独立 XLSX 的事实读取器也未接受 `conditionalFormatting/cfRule/formula`。
2. 两张表分别包含 152,240 与 16,359 个单元格，超过沿用内嵌表格的 10,000 单元格限制；它们仍在独立 XLSX 原有 1,024 行、256 列范围内。
3. 15 个 `HYPERLINK` 单元格公式的引号内文字含方括号或竖线，被整串字符黑名单误判为外部工作簿或 DDE 引用。结构扫描确认这些符号均在双引号字符串内。
4. 两个单元格使用标准 XML 命名空间的 `xml:space` 属性，单元格属性检查一律拒绝带命名空间属性。原文件重放在修复上述项目后暴露此处拒绝；修复仅接受 `preserve/default` 两个标准值，保留原文字空白。

页面将上述情况归为同一“不支持的内嵌表格或内容结构”提示，不能据此判断是飞书权限、网络、文件丢失或嵌套对象下载失败。

## 修复

独立 XLSX 接受标准命名空间下 `worksheet/conditionalFormatting/cfRule/formula` 路径，把规则属性和公式作为原文事实保留；不计算公式。单元格计数上限与已有 1,024×256 行列边界一致，保留原有 ZIP、XML、输出字节和分段限制。

公式依赖检查识别 Excel 双引号字符串及双引号转义。引号内的方括号、竖线可作为普通文字保留；引号外的外部引用、DDE 字符、NUL 及未闭合字符串继续拒绝。内嵌 XLSX 保持原有解析范围。

## 上游对照

- [Issue #2087](https://github.com/Tencent/WeKnora/issues/2087)、[PR #2334](https://github.com/Tencent/WeKnora/pull/2334)：DOCX 导出丢失内嵌表格内容，改进 blocks 读取。
- [Issue #3599](https://github.com/Tencent/WeKnora/issues/3599)、[PR #3608](https://github.com/Tencent/WeKnora/pull/3608)：Python/openpyxl 无法读取部分数据验证、方案区域，导致 XLSX 解析失败。
- [Issue #4022](https://github.com/Tencent/WeKnora/issues/4022)：blocks 模式内嵌表格响应过大导致内容丢失。

这些问题现象相似，但本次拒绝来自本分支个人导入的 Go 解析预检，不能直接用上述上游修复替代。

原文件仅用于受控私有测试，不提交到仓库。取证脚本和回执位于 `E:/AIData/Codex/bitable-incident-20261119` 与既有受控运维目录。

## 验证与发布状态

Linux Go 1.26.7、链接既有 anydoc 静态库：

```sh
go test -tags anydoc ./internal/application/service \
  -run 'TestStandalone|TestExactStandaloneWorkbook|TestExactWorkbook' -count=1 -v
```

测试通过，包内执行耗时 12.999 秒。包含条件格式事实保留、外部公式拒绝、字符串转义、超过 10,000 单元格、标准空白属性与既有内嵌表格回归。私有原文件重放成功，21 个工作表生成 667 个分段，Markdown 总计 5,018,253 字节，未放宽原有输出总预算。

`git diff --check` 通过。修复分支为 `fix/bitable-import-20261119`，基于生产提交建立；本轮未部署修复，未把生产记录改为成功，未声称完成 embedding 或最终入库。发布后仍需由正常队列执行源校验、解析、向量生成和发布，并验证该文档最终状态。
