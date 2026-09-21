export interface MCPConfigSpace {
  tenantId: number
  tenantName: string
}

export type MCPConfigClient = 'workbuddy' | 'workmate' | 'cursor' | 'codebuddy' | 'generic'

// Branded presets are only promoted after a real client import test. Generic is
// the portable baseline and does not claim client-specific compatibility.
export const verifiedConfigPreset = (client: MCPConfigClient) => client === 'generic'

const slug = (value: string) => value
  .normalize('NFKD')
  .replace(/[^\p{L}\p{N}]+/gu, '-')
  .replace(/^-|-$/g, '')
  .toLowerCase() || 'workspace'

export function serverName(space: MCPConfigSpace) {
  return `knowhub-${slug(space.tenantName)}-${space.tenantId}`
}

// MCP HTTP ingress and the KnowHub REST API intentionally use different
// authentication contracts. Keep the builders separate so a runtime check of
// one endpoint cannot accidentally change presets for the other endpoint.
export function buildMcpHttpHeaders(token: string, tenantId: number) {
  return {
    Authorization: `Bearer ${token}`,
    'X-Tenant-ID': String(tenantId),
  }
}

export function buildRestApiHeaders(token: string, tenantId: number) {
  return {
    'X-API-Key': token,
    'X-Tenant-ID': String(tenantId),
  }
}

export function buildMCPServers(url: string, token: string, spaces: MCPConfigSpace[], client: MCPConfigClient = 'generic') {
  const transport = client === 'workbuddy' ? 'streamableHttp' : 'http'
  return {
    mcpServers: Object.fromEntries(spaces.map(space => [serverName(space), {
      type: transport,
      url,
      headers: buildMcpHttpHeaders(token, space.tenantId),
    }])),
  }
}

export const stringifyMCPConfig = (url: string, token: string, spaces: MCPConfigSpace[], client: MCPConfigClient = 'generic') =>
  JSON.stringify(buildMCPServers(url, token, spaces, client), null, 2)

export function claudeCommands(url: string, token: string, spaces: MCPConfigSpace[]) {
  return spaces.map(space =>
    `claude mcp add --transport http ${serverName(space)} ${JSON.stringify(url)} --header ${JSON.stringify(`Authorization: Bearer ${token}`)} --header ${JSON.stringify(`X-Tenant-ID: ${space.tenantId}`)}`,
  ).join('\n')
}
