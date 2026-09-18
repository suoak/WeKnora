import assert from 'node:assert/strict'
import test from 'node:test'
import { resolvePortalResponsibleTeam } from './portalSpacePresentation'

test('each Portal card resolves responsible_team from its own space record', () => {
  const developmentA = { tenant_id: 101, responsible_team: 'A负责人', contact: '共享联系人' }
  const developmentB = { tenant_id: 102, responsible_team: 'B负责人', contact: '共享联系人' }
  const publicSpace = { tenant_id: 201, responsible_team: '公共负责人', contact: '当前空间联系人' }
  const projectSpace = { tenant_id: 202, responsible_team: '项目负责人', contact: '当前空间联系人' }

  assert.equal(resolvePortalResponsibleTeam(developmentA, '未配置'), 'A负责人')
  assert.equal(resolvePortalResponsibleTeam(developmentB, '未配置'), 'B负责人')
  assert.equal(resolvePortalResponsibleTeam(publicSpace, '未配置'), '公共负责人')
  assert.equal(resolvePortalResponsibleTeam(projectSpace, '未配置'), '项目负责人')
})

test('empty responsible_team uses only the explicit unconfigured fallback', () => {
  const space = { tenant_id: 301, responsible_team: '  ', contact: '不能作为负责人' }
  assert.equal(resolvePortalResponsibleTeam(space, '未配置'), '未配置')
  assert.equal(resolvePortalResponsibleTeam(undefined, '未配置'), '未配置')
})
