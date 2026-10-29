import type { MCPAccessKey } from '../../api/mcpAccessKeys'
import { getCredentialLifecycleState, type CredentialStatusFilter } from './mcpAccessLifecycle'

export const credentialPageSize = 5
export function credentialListState(rows: MCPAccessKey[], now = Date.now()) {
  const counts = { active: 0, expired: 0, revoked: 0 }
  for (const row of rows) counts[getCredentialLifecycleState(row, now)]++
  const states = (Object.keys(counts) as Array<keyof typeof counts>).filter(state => counts[state] > 0)
  const labels = { active: '已启用', expired: '已过期', revoked: '已停用' }
  const filters: Array<{ value: CredentialStatusFilter; label: string; count: number }> = rows.length
    ? [{ value: 'all', label: '全部', count: rows.length }, ...states.map(value => ({ value, label: labels[value], count: counts[value] }))]
    : []
  const summary = `共 ${rows.length} 个 Key` + (states.length === 1 ? `，均${labels[states[0]]}` : counts.active ? `，${counts.active} 个已启用` : '')
  return { filters, summary, multipleStates: states.length > 1, hasMany: rows.length > credentialPageSize }
}
export function clampCredentialPage(page: number, count: number) {
  return Math.max(1, Math.min(Math.floor(page) || 1, Math.max(1, Math.ceil(count / credentialPageSize))))
}
