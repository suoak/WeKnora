import { personalMcpURL } from './personalMcpConfig'

// These are UI choices, independent of the persisted historical client_type.
// References were checked on 2026-10-29. No preset claims a live client session.
export const mcpClients = [
  { value: 'claude', label: 'Claude Code', icon: 'terminal', description: '连接项目知识', location: '用户级 ~/.claude.json', steps: ['点击“复制完整配置（含密钥）”，将 mcpServers 合并到用户级 ~/.claude.json，保留原有设置。已自动填入当前 Key，无需手动替换。', '配置含真实密钥，仅保存到自己的用户级配置；禁止写入项目 .mcp.json 或提交代码仓库。', '启动 Claude Code，在 /mcp 中启用 KnowHub 并验证知识检索。'] },
  { value: 'codex', label: 'Codex', icon: 'code', description: '连接编程助手', location: '用户级 ~/.codex/config.toml', steps: ['点击“复制完整配置（含密钥）”，将 TOML 合并到自己的 ~/.codex/config.toml，保留已有服务器。http_headers 已包含当前 Key，无需再设置环境变量或替换密钥。', '配置含真实密钥，仅保存在用户级配置，禁止写入项目配置或提交代码仓库。运行 codex mcp list 检查配置，在 /mcp 中验证连接。'] },
  { value: 'copilot', label: 'GitHub Copilot', icon: 'code', description: '通过 VS Code 接入', location: 'VS Code 用户级 MCP 配置', steps: ['本入口适用于 VS Code 中的 GitHub Copilot。点击“复制完整配置（含密钥）”，内容已自动填入当前 Key，无需再替换密钥。', '在 VS Code 命令面板运行 MCP: Open User Configuration，将配置合并到打开的用户级 MCP 配置，保留已有服务器。配置含真实密钥，禁止保存到项目 .vscode/mcp.json 或提交代码仓库。', '运行 MCP: List Servers，启动 KnowHub，在 Copilot Agent 中启用工具并验证检索。'] },
  { value: 'vscode', label: 'VS Code', icon: 'code', description: '连接工作区', location: 'VS Code 用户级 MCP 配置', steps: ['点击“复制完整配置（含密钥）”，配置已包含当前 Key 的真实 Secret，无需在客户端另贴密钥。', '运行 MCP: Open User Configuration，将配置合并到用户级 MCP 配置，保留已有服务器；禁止将含密钥配置写入项目 .vscode/mcp.json 或提交代码仓库。', '运行 MCP: List Servers，启动 KnowHub，再在聊天工具选择器中启用工具。'] },
  { value: 'workbuddy', label: 'WorkBuddy', icon: 'service', description: '连接工作助手', location: 'WorkBuddy MCP 配置编辑器', steps: ['打开 WorkBuddy 的 MCP 设置，将配置合并到 MCP 配置编辑器。', '保存并启用 KnowHub 服务器，检查工具是否加载；不同版本的菜单位置可能不同。'] },
  { value: 'pi', label: 'Pi', icon: 'terminal', description: 'Pi Coding Agent', location: '~/.pi/agent/mcp.json · Pi v1.4.0', steps: ['安装固定版本：npm install -g --ignore-scripts @earendil-works/pi-coding-agent@1.4.0。官方仓库：github.com/earendil-works/pi。此版本原生支持 MCP，无需扩展。', '将配置合并到用户级 ~/.pi/agent/mcp.json，保留已有服务器。运行 pi mcp list 检查连接。', '启动 pi，用 /mcp 检查 KnowHub；配置变更后用 /reload 重新加载。旧版 Pi 请先升级到目标版本。'] },
  { value: 'generic', label: '其他 MCP 客户端', icon: 'link', description: '查看通用接入方式', location: '支持 Streamable HTTP 与 Bearer 认证的客户端', steps: ['确认客户端支持 Streamable HTTP 和自定义 Authorization 请求头。', '此处为通用参考模板，请按客户端官方文档调整配置字段后启用。'] },
] as const

export type MCPClientPreset = typeof mcpClients[number]['value']

export function personalClientConfiguration(client: MCPClientPreset, publicURL: string, token: string): string {
  const url = personalMcpURL(publicURL)
  if (client === 'codex') return `[mcp_servers.knowhub]\nurl = ${JSON.stringify(url)}\nhttp_headers = { Authorization = ${JSON.stringify(`Bearer ${token}`)} }\n`
  const headers = { Authorization: `Bearer ${token}` }
  if (client === 'vscode' || client === 'copilot') {
    // This complete configuration belongs in MCP: Open User Configuration.
    return JSON.stringify({ servers: { knowhub: { type: 'http', url, headers } } }, null, 2)
  }
  if (client === 'pi') {
    // Pi v1.4.0 schema: remote URL and headers, not VS Code servers or Codex TOML.
    return JSON.stringify({ mcpServers: { knowhub: { url, headers, description: '检索本人授权的 KnowHub 知识' } } }, null, 2)
  }
  return JSON.stringify({ mcpServers: { knowhub: { type: client === 'workbuddy' ? 'streamableHttp' : 'http', url, headers } } }, null, 2)
}
