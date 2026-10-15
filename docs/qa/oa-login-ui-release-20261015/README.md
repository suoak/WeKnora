# OA 登录 UI 生产构建视觉证据

本目录为纯合成 Mock，本地 127.0.0.1 浏览器，没有真实 SID 登录或凭据。

根目录 before/after 对照使用当前部署版 431226c3b 和 UI 候选应用源码；后续提交只修正测试工具/夹具，应用源码与最终 1ba24ef5f 一致。`ci-1ba24ef5f` 为最终精确 SHA 的 GitHub Actions 原样截图及结果，优先用于发布审核。

桌面 1440×1000、手机 390×844；中英文、暗色。完整页面与登录卡片裁切均保留。结果包含几何布局、主题前后相同、密码及 OA 事件、Loading、Focus、错误恢复、其他 Provider 和关闭 OIDC 的断言。

详见 [独立发布报告](../../integration/sourceid/KNOWHUB_OA_LOGIN_UI_RELEASE_REPORT.md)。当前只是候选，未替换打包机 frontend。
