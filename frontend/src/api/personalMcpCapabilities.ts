import type { MCPCapability } from './mcpAccessKeys'

export function withPersonalMCPBuiltinCapabilities(capabilities: readonly MCPCapability[] = []): MCPCapability[] {
  return [...new Set<MCPCapability>([...capabilities, 'retrieve', 'add_content'])]
}
