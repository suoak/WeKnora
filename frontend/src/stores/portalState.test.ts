import assert from 'node:assert/strict'
import test from 'node:test'
import { canReviewPortalRequests, portalCategories, PORTAL_ALL_STAGE, uniquePortalSpaces, validAccessReason } from './portalState'
import { buildPortalSpacesPath, portalAccessRequestBody } from '../api/portalContracts'
import type { PortalSpace } from '@/api/portal'

const space = (tenant_id: number, category = 'ntos'): PortalSpace => ({
  tenant_id, category, display_name: `space-${tenant_id}`, description: '', responsible_team: '', contact: '',
  stages: ['design', 'testing'], featured: false, knowledge_base_count: 0, file_count: 0,
  access_state: 'not_member', current_role: null,
  can_request_access: true, interaction_action: 'none',
})

test('portal filters produce metadata search params and omit all stage', () => {
  assert.equal(buildPortalSpacesPath({ q: 'platform', stage: 'all' }), '/api/v1/portal/spaces?q=platform')
  assert.equal(buildPortalSpacesPath({ stage: 'design', category: 'ntos' }), '/api/v1/portal/spaces?stage=design&category=ntos')
})

test('access request body cannot carry role/source and approval visibility is Owner-only', () => {
  assert.deepEqual(portalAccessRequestBody('need access'), { reason: 'need access' })
  assert.equal(canReviewPortalRequests('owner'), true)
  assert.equal(canReviewPortalRequests('admin'), false)
  assert.equal(canReviewPortalRequests('viewer'), false)
})

test('portal defaults to all and deduplicates multi-stage workspaces by tenant', () => {
  assert.equal(PORTAL_ALL_STAGE, 'all')
  assert.deepEqual(uniquePortalSpaces([space(1), space(1), space(2)]).map((item) => item.tenant_id), [1, 2])
})

test('portal categories remain independent metadata and reason validation trims unicode', () => {
  assert.deepEqual(portalCategories([space(1, 'requirements'), space(2, 'ntos'), space(3, 'requirements')]), ['ntos', 'requirements'])
  assert.equal(validAccessReason('  '), false)
  assert.equal(validAccessReason('需要只读权限'), true)
  assert.equal(validAccessReason('x'.repeat(1001)), false)
})
