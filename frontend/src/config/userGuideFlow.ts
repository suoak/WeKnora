export interface UserGuideContext {
  role: string
  isSystemAdmin: boolean
  agentsSupported: boolean
  mcpSupported: boolean
}

export interface UserGuideStepDescriptor {
  key: string
  target?: string
  optional?: boolean
}

export function buildUserGuideFlow(context: UserGuideContext): UserGuideStepDescriptor[] {
  const steps: UserGuideStepDescriptor[] = [
    { key: 'welcome' },
    { key: 'portal', target: '[data-guide="portal-home"]' },
    { key: 'quickAsk', target: '[data-guide="portal-quick-ask"]' },
    { key: 'space', target: '[data-guide="space-switcher"]' },
    { key: 'knowledge', target: '[data-guide="nav-knowledge-bases"]' },
  ]
  if (context.agentsSupported) steps.push({ key: 'agents', target: '[data-guide="nav-agents"]', optional: true })
  if (context.mcpSupported) steps.push({ key: 'mcp', target: '[data-guide="portal-task-mcp"]', optional: true })
  if (context.role === 'contributor') steps.push({ key: 'contributor', target: '[data-guide="nav-knowledge-bases"]', optional: true })
  if (context.role === 'admin' || context.role === 'owner') {
    steps.push({ key: 'members', target: '[data-guide="nav-members"]', optional: true })
    steps.push({ key: 'integrations', target: '[data-guide="nav-integrations"]', optional: true })
  }
  if (context.isSystemAdmin) steps.push({ key: 'systemAdmin', target: '[data-guide="nav-usage-analytics"]', optional: true })
  steps.push({ key: 'done' })
  return steps
}
