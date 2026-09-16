import assert from 'node:assert/strict'
import test from 'node:test'
import type { PortalSpace } from '@/api/portal'
import { PORTAL_STAGE_ICONS, resolveDevelopmentSpaceIcon, resolvePublicSpaceIcon, resolveStageIcon, resolveStageSpaceIcon } from './portalVisualIcons'

const space=(display_name:string,category='',description=''):PortalSpace=>({
  tenant_id:1,display_name,description,category,responsible_team:'',contact:'',stages:[],featured:false,
  knowledge_base_count:0,file_count:0,access_state:'accessible',current_role:'viewer',can_request_access:false,
  access_request_pending:false,membership_suspended:false,interaction_action:'enter',
})

test('every stable stage key resolves to a semantic icon and unknown stages fall back safely',()=>{
  const keys=['insight','concept_market','concept_product','architecture','design','development','testing','lmt']
  assert.deepEqual(Object.keys(PORTAL_STAGE_ICONS),keys)
  for(const key of keys)assert.notEqual(resolveStageIcon(key),'folder')
  assert.equal(resolveStageIcon('future_stage'),'folder')
})

test('development resolver is visual-only, semantic, and safe for unknown dynamic spaces',()=>{
  assert.equal(resolveDevelopmentSpaceIcon(space('嵌入式研发')),'cpu')
  assert.equal(resolveDevelopmentSpaceIcon(space('端侧应用')),'mobile')
  assert.equal(resolveDevelopmentSpaceIcon(space('硬件平台')),'server')
  assert.equal(resolveDevelopmentSpaceIcon(space('软件研发')),'code')
  assert.equal(resolveDevelopmentSpaceIcon(space('未来空间')),'file-code')
  assert.equal(resolveStageSpaceIcon('testing',space('任意空间')),'bug')
})

test('public resolver uses conservative semantics and always has a folder fallback',()=>{
  assert.equal(resolveStageIcon('concept_product'),'focus')
  assert.equal(resolvePublicSpaceIcon(space('安全规范')),'secured')
  assert.equal(resolvePublicSpaceIcon(space('术语库')),'translate')
  assert.equal(resolvePublicSpaceIcon(space('公共项目')),'tree-catalog')
  assert.equal(resolvePublicSpaceIcon(space('知识指南')),'book-open')
  assert.equal(resolvePublicSpaceIcon(space('未分类空间')),'folder')
})
