import type { PortalSpace, PortalStage } from '@/api/portal'

export const PORTAL_ALL_STAGE = 'all'

const STAGE_FALLBACK: Record<string, { name: string; description: string }> = {
  concept_market: { name: '概念1', description: '市场机会与概念探索' },
  concept_product: { name: '概念2', description: '产品概念与需求定义' },
  architecture: { name: '架构', description: '系统与技术架构' },
  design: { name: '设计', description: '方案与详细设计' },
  development: { name: '开发', description: '研发实现与构建' },
  testing: { name: '测试', description: '验证、测试与质量保障' },
  lmt: { name: 'LMT', description: '上市与生命周期管理' },
}

export function displayPortalStage(stage: PortalStage) {
  const fallback = STAGE_FALLBACK[stage.key]
  return {
    ...stage,
    name: stage.name?.trim() || fallback?.name || stage.key,
    description: stage.description?.trim() || fallback?.description || '',
  }
}

export function uniquePortalSpaces(spaces: PortalSpace[]): PortalSpace[] {
  const seen = new Set<number>()
  return spaces.filter((space) => {
    if (seen.has(space.tenant_id)) return false
    seen.add(space.tenant_id)
    return true
  })
}

export function portalCategories(spaces: PortalSpace[]): string[] {
  return [...new Set(spaces.map((space) => space.category.trim()).filter(Boolean))].sort((a, b) => a.localeCompare(b))
}

export function validAccessReason(reason: string): boolean {
  const length = [...reason.trim()].length
  return length > 0 && length <= 1000
}

export function canReviewPortalRequests(role: string | null | undefined): boolean {
  return role === 'owner'
}
