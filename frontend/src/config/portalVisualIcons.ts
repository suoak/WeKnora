import type { PortalSpace } from '@/api/portal'

export const PORTAL_STAGE_ICONS: Readonly<Record<string, string>> = Object.freeze({
  insight: 'compass',
  concept_market: 'lightbulb',
  concept_product: 'focus',
  architecture: 'sitemap',
  design: 'pen',
  development: 'git-branch',
  testing: 'bug',
  lmt: 'refresh',
})

export const resolveStageIcon = (stageKey: string): string => PORTAL_STAGE_ICONS[stageKey] || 'folder'
const searchableSpaceText = (space: PortalSpace) => `${space.display_name} ${space.description} ${space.category}`.toLocaleLowerCase()

export function resolveDevelopmentSpaceIcon(space: PortalSpace): string {
  const text = searchableSpaceText(space)
  if (/嵌入|嵌软|embedded|firmware|芯片|microchip/.test(text)) return 'cpu'
  if (/端侧|移动|手机|mobile|terminal|device/.test(text)) return 'mobile'
  if (/硬件|hardware|server|服务器/.test(text)) return 'server'
  if (/软件|软研|代码|software|code|研发/.test(text)) return 'code'
  return 'file-code'
}

export const resolveStageSpaceIcon = (stageKey: string, space: PortalSpace): string =>
  stageKey === 'development' ? resolveDevelopmentSpaceIcon(space) : resolveStageIcon(stageKey)

export function resolvePublicSpaceIcon(space: PortalSpace): string {
  const text = searchableSpaceText(space)
  if (/安全|security|合规|risk/.test(text)) return 'secured'
  if (/术语|语义|词汇|terminology|semantic|language/.test(text)) return 'translate'
  if (/项目|project|portfolio/.test(text)) return 'tree-catalog'
  if (/公共|知识|knowledge|标准|规范|guide/.test(text)) return 'book-open'
  return 'folder'
}
