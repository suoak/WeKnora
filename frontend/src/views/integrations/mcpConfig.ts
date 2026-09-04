export interface MCPConfigSpace {
  tenantId: number
  tenantName: string
}

const slug = (value: string) => value
  .normalize('NFKD')
  .replace(/[^\p{L}\p{N}]+/gu, '-')
  .replace(/^-|-$/g, '')
  .toLowerCase() || 'workspace'

export function serverName(space: MCPConfigSpace) {
  return `knowhub-${slug(space.tenantName)}-${space.tenantId}`
}

export function buildMCPServers(url: string, token: string, spaces: MCPConfigSpace[]) {
  return {
    mcpServers: Object.fromEntries(spaces.map(space => [serverName(space), {
      type: 'http',
      url,
      headers: {
        Authorization: `Bearer ${token}`,
        'X-Tenant-ID': String(space.tenantId),
      },
    }])),
  }
}

export const stringifyMCPConfig = (url: string, token: string, spaces: MCPConfigSpace[]) =>
  JSON.stringify(buildMCPServers(url, token, spaces), null, 2)

export function claudeCommands(url: string, token: string, spaces: MCPConfigSpace[]) {
  return spaces.map(space =>
    `claude mcp add --transport http ${serverName(space)} ${JSON.stringify(url)} --header ${JSON.stringify(`Authorization: Bearer ${token}`)} --header ${JSON.stringify(`X-Tenant-ID: ${space.tenantId}`)}`,
  ).join('\n')
}
