# 知汇 KnowHub 品牌资产

“知汇”图形由两侧知识页/数据流向中心节点汇聚构成，同时形成抽象的 `H` 轮廓。它与上游 WeKnora 的帆船和波浪标志保持明确区分。

## 标准名称

- 中文：知汇
- 英文：KnowHub
- 组合：知汇 KnowHub
- 来源说明：知汇 KnowHub，基于开源 WeKnora 构建

## 标准色

- 汇聚绿：`#07C05F`
- 深墨色：`#101F38`
- 节点金：`#D9A94F`，仅作小面积强调
- 深色背景文字：`#F4F7FB`

## 使用规则

- 小于 32 px 时只使用图形标志，不附加文字。
- 图形四周至少保留中心金色节点直径一倍的安全区。
- 浅色背景使用 `knowhub-mark.svg`，深色背景使用 `knowhub-mark-dark.svg`。
- 彩色不可用时允许使用纯黑或纯白单色版本，不使用阴影、描边或渐变。
- `WEKNORA_*` 环境变量、Go module、API audience、数据目录等兼容标识不得随视觉品牌改名。

## 文件

- `knowhub-mark.svg`：浅色背景图形母版
- `knowhub-mark-dark.svg`：深色背景图形母版
- `knowhub-mark-inverse.svg`：品牌色背景反白版
- `knowhub-mark-mono.svg` / `knowhub-mark-white.svg`：单色版本
- `knowhub-logo.svg`：中英文横版组合标志
- `knowhub-logo-dark.svg`：深色背景横版组合标志
- `knowhub-mark-512.png`：通用透明位图
- `../images/knowhub-logo.png`：横版 PNG

端侧资产：

- Web：`frontend/public/favicon.ico`、`apple-touch-icon.png`、192/512 应用图标和 Open Graph 分享图
- 文档站：`website-docs/public/logo-mark*.svg`、`favicon.svg`、`knowhub-og.png`
- 小程序：`miniprogram/assets/brand/knowhub-app-icon.png`，用于微信公众平台上传
- 桌面端：`cmd/desktop/build/appicon.png`，技术产物名继续保留 `WeKnora Lite` 以兼容升级

各端 PNG/ICO 由上述几何参数导出，不作为二次编辑母版。
