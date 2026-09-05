<template>
  <div class="mcp-keys">
    <div class="head">
      <div>
        <h2>MCP Access Keys</h2>
        <p>一把 Key 可连接多个已授权空间；权限随成员关系和共享状态实时变化。</p>
      </div>
      <t-button @click="openCreate">创建 Key</t-button>
    </div>

    <t-alert v-if="!publicUrl" theme="warning">
      管理员尚未正确配置 MCP_PUBLIC_URL，仍可创建 Key，但不能生成可直接复制的完整配置。
    </t-alert>

    <t-table :data="keys" :columns="columns" row-key="id">
      <template #tenant_scopes="{ row }">
        {{ scopeSummary(row) }}
      </template>
      <template #expires_at="{ row }">
        {{ row.expires_at ? formatTime(row.expires_at) : '永不过期' }}
      </template>
      <template #operation="{ row }">
        <t-space>
          <t-button variant="text" @click="showTemplate(row)">配置模板</t-button>
          <t-button variant="text" @click="openEdit(row)">编辑范围</t-button>
          <t-popconfirm content="撤销后无法恢复，确认继续？" @confirm="revoke(row.id)">
            <t-button theme="danger" variant="text">撤销</t-button>
          </t-popconfirm>
        </t-space>
      </template>
    </t-table>

    <t-dialog
      v-model:visible="editorVisible"
      :header="editingID ? '编辑 MCP Key' : '创建多空间 MCP Key'"
      width="780px"
      :confirm-btn="{ content: editingID ? '保存' : '创建' }"
      @confirm="save"
    >
      <t-form label-align="top">
        <t-form-item label="名称">
          <t-input v-model="name" maxlength="128" />
        </t-form-item>
        <t-form-item label="有效期">
          <div class="expiry-row">
            <t-checkbox v-model="neverExpires">永不过期</t-checkbox>
            <input v-if="!neverExpires" v-model="customExpiry" class="native-datetime" type="datetime-local" />
            <span v-if="!neverExpires" class="hint">留空时默认 90 天</span>
          </div>
        </t-form-item>
        <t-form-item label="授权空间">
          <div class="scope-list">
            <section v-for="option in options" :key="option.tenant_id" class="scope-card">
              <t-checkbox
                :checked="selectedTenantIds.includes(option.tenant_id)"
                @change="toggleTenant(option.tenant_id, Boolean($event))"
              >
                {{ option.tenant_name }}
              </t-checkbox>
              <div v-if="selectedTenantIds.includes(option.tenant_id)" class="scope-detail">
                <t-radio-group v-model="draftScopes[option.tenant_id].mode">
                  <t-radio value="all">动态全部 KB（包括未来新增及当前可访问的共享 KB）</t-radio>
                  <t-radio value="selected">仅指定 KB</t-radio>
                </t-radio-group>
                <div v-if="draftScopes[option.tenant_id].mode === 'selected'" class="kb-groups">
                  <div>
                    <strong>本空间 KB</strong>
                    <t-checkbox-group v-model="draftScopes[option.tenant_id].selected">
                      <t-checkbox v-for="kb in option.owned_knowledge_bases" :key="ownedRef(kb.id)" :value="ownedRef(kb.id)">
                        {{ kb.name }}
                      </t-checkbox>
                    </t-checkbox-group>
                  </div>
                  <div>
                    <strong>共享空间 KB</strong>
                    <t-checkbox-group v-model="draftScopes[option.tenant_id].selected">
                      <t-checkbox v-for="share in option.shared_knowledge_bases" :key="sharedRef(share.knowledge_base.id, share.share_id)" :value="sharedRef(share.knowledge_base.id, share.share_id)">
                        {{ share.knowledge_base.name }} · {{ share.org_name }} · {{ share.permission }}
                      </t-checkbox>
                    </t-checkbox-group>
                  </div>
                </div>
              </div>
            </section>
          </div>
        </t-form-item>
      </t-form>
    </t-dialog>

    <t-dialog v-model:visible="configVisible" header="保存 Key 并复制 MCP 配置" width="860px" @close="clearPlaintextToken">
      <t-alert theme="warning">
        {{ token === TOKEN_PLACEHOLDER ? '此处为占位模板；如已遗失 Key，请撤销旧 Key 并创建新 Key。' : '完整配置中的 Key 只显示一次，关闭后无法再次获取。请立即保存，且不要提交到代码仓库。' }}
      </t-alert>
      <div class="token-row">
        <code>{{ token }}</code>
        <t-button size="small" variant="outline" @click="copySensitive(token)">复制 Key</t-button>
      </div>
      <t-tabs v-model="tab">
        <t-tab-panel value="generic" label="通用 HTTP JSON"><pre>{{ jsonConfig }}</pre></t-tab-panel>
        <t-tab-panel value="claude" label="Claude Code">
          <p class="hint">逐空间执行命令，或使用下方 JSON 配置。</p>
          <pre>{{ commands }}</pre>
          <pre>{{ jsonConfig }}</pre>
        </t-tab-panel>
        <t-tab-panel value="cursor" label="Cursor">
          <p class="hint">可保存为用户级 ~/.cursor/mcp.json，或项目级 .cursor/mcp.json。</p>
          <pre>{{ jsonConfig }}</pre>
        </t-tab-panel>
        <t-tab-panel value="cherry" label="Cherry Studio">
          <div v-for="space in configSpaces" :key="space.tenantId" class="cherry-card">
            <div><strong>{{ space.tenantName }}</strong></div>
            <div>类型：Streamable HTTP</div>
            <div>URL：{{ publicUrl }} <t-button size="small" variant="text" @click="copySensitive(publicUrl)">复制</t-button></div>
            <div>Authorization：Bearer {{ token }} <t-button size="small" variant="text" @click="copySensitive(`Bearer ${token}`)">复制</t-button></div>
            <div>X-Tenant-ID：{{ space.tenantId }} <t-button size="small" variant="text" @click="copySensitive(String(space.tenantId))">复制</t-button></div>
          </div>
        </t-tab-panel>
      </t-tabs>
      <div class="space-copy">
        <span>单空间复制：</span>
        <t-button v-for="space in configSpaces" :key="space.tenantId" size="small" variant="outline" :disabled="!publicUrl" @click="copySpace(space)">
          {{ space.tenantName }}
        </t-button>
      </div>
      <p class="hint">验证时请确认客户端显示连接成功，并尝试列出一个已授权知识库。配置包含敏感凭证，请仅保存到可信设备。</p>
      <template #footer>
        <t-button :disabled="!publicUrl" @click="copySensitive(jsonConfig)">复制全部 JSON</t-button>
        <t-button v-if="tab === 'claude'" variant="outline" :disabled="!publicUrl" @click="copySensitive(commands)">复制 Claude 命令</t-button>
        <t-button variant="outline" :disabled="!publicUrl" @click="download">下载 JSON</t-button>
      </template>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import {
  createMCPAccessKey,
  getMCPAccessKeyScopeOptions,
  listMCPAccessKeys,
  revokeMCPAccessKey,
  updateMCPAccessKey,
  type MCPAccessKey,
  type MCPKBRef,
  type MCPScopeOption,
  type KBScopeMode,
} from '@/api/mcpAccessKeys'
import { copyWithToast } from '@/utils/clipboard'
import { buildMCPServers, claudeCommands, stringifyMCPConfig, type MCPConfigSpace } from './mcpConfig'

const TOKEN_PLACEHOLDER = '<YOUR_MCP_KEY>'
interface DraftScope { mode: KBScopeMode; selected: string[] }

const keys = ref<MCPAccessKey[]>([])
const options = ref<MCPScopeOption[]>([])
const publicUrl = ref('')
const editorVisible = ref(false)
const configVisible = ref(false)
const editingID = ref<number | null>(null)
const name = ref('')
const selectedTenantIds = ref<number[]>([])
const draftScopes = ref<Record<number, DraftScope>>({})
const neverExpires = ref(false)
const customExpiry = ref('')
const token = ref('')
const tab = ref('generic')
const configSpaces = ref<MCPConfigSpace[]>([])

const columns = [
  { colKey: 'name', title: '名称' },
  { colKey: 'tenant_scopes', title: '授权范围' },
  { colKey: 'expires_at', title: '有效期' },
  { colKey: 'operation', title: '操作', width: 260 },
]

const jsonConfig = computed(() => stringifyMCPConfig(publicUrl.value, token.value, configSpaces.value))
const commands = computed(() => claudeCommands(publicUrl.value, token.value, configSpaces.value))

const ownedRef = (kbID: string) => `owned:${kbID}`
const sharedRef = (kbID: string, shareID: string) => `shared:${kbID}:${shareID}`

function ensureDraft(tenantID: number): DraftScope {
  if (!draftScopes.value[tenantID]) draftScopes.value[tenantID] = { mode: 'all', selected: [] }
  return draftScopes.value[tenantID]
}

function toggleTenant(tenantID: number, checked: boolean) {
  ensureDraft(tenantID)
  selectedTenantIds.value = checked
    ? Array.from(new Set([...selectedTenantIds.value, tenantID]))
    : selectedTenantIds.value.filter(id => id !== tenantID)
}

function decodeRef(value: string): MCPKBRef {
  const [source, kbID, shareID] = value.split(':')
  return source === 'shared'
    ? { knowledge_base_id: kbID, source_type: 'shared', kb_share_id: shareID }
    : { knowledge_base_id: kbID, source_type: 'owned' }
}

function payloadScopes() {
  return selectedTenantIds.value.map(tenantID => {
    const draft = ensureDraft(tenantID)
    return {
      tenant_id: tenantID,
      kb_scope_mode: draft.mode,
      knowledge_bases: draft.mode === 'selected' ? draft.selected.map(decodeRef) : [],
    }
  })
}

function spacesForScopes(scopes: MCPAccessKey['tenant_scopes']): MCPConfigSpace[] {
  return scopes.map(scope => ({
    tenantId: scope.tenant_id,
    tenantName: options.value.find(option => option.tenant_id === scope.tenant_id)?.tenant_name || String(scope.tenant_id),
  }))
}

function resetEditor() {
  editingID.value = null
  name.value = ''
  selectedTenantIds.value = []
  draftScopes.value = {}
  neverExpires.value = false
  customExpiry.value = ''
}

function openCreate() {
  resetEditor()
  editorVisible.value = true
}

function openEdit(key: MCPAccessKey) {
  resetEditor()
  editingID.value = key.id
  name.value = key.name
  selectedTenantIds.value = key.tenant_scopes.map(scope => scope.tenant_id)
  for (const scope of key.tenant_scopes) {
    draftScopes.value[scope.tenant_id] = {
      mode: scope.kb_scope_mode,
      selected: scope.knowledge_bases.map(kb => kb.source_type === 'shared'
        ? sharedRef(kb.knowledge_base_id, kb.kb_share_id || '')
        : ownedRef(kb.knowledge_base_id)),
    }
  }
  neverExpires.value = !key.expires_at
  customExpiry.value = key.expires_at ? toDatetimeLocal(key.expires_at) : ''
  editorVisible.value = true
}

async function save() {
  if (!name.value.trim() || !selectedTenantIds.value.length) {
    MessagePlugin.warning('请填写名称并至少选择一个空间')
    return
  }
  if (selectedTenantIds.value.some(id => ensureDraft(id).mode === 'selected' && !ensureDraft(id).selected.length)) {
    MessagePlugin.warning('“仅指定 KB”的空间必须至少选择一个知识库')
    return
  }
  const expiry = !neverExpires.value && customExpiry.value ? Math.floor(new Date(customExpiry.value).getTime() / 1000) : undefined
  const payload = {
    name: name.value.trim(),
    never_expires: neverExpires.value,
    expires_at_unix: expiry,
    tenant_scopes: payloadScopes(),
  }
  if (editingID.value) {
    await updateMCPAccessKey(editingID.value, payload)
    editorVisible.value = false
    await load()
    MessagePlugin.success('MCP Key 授权范围已更新')
    return
  }
  const response = await createMCPAccessKey(payload)
  if (!response.data) return
  token.value = response.data.token || ''
  publicUrl.value = response.data.mcp_public_url || publicUrl.value
  configSpaces.value = spacesForScopes(response.data.tenant_scopes)
  editorVisible.value = false
  configVisible.value = true
  await load()
}

function showTemplate(key: MCPAccessKey) {
  token.value = TOKEN_PLACEHOLDER
  configSpaces.value = spacesForScopes(key.tenant_scopes)
  configVisible.value = true
}

function clearPlaintextToken() {
  token.value = ''
  configSpaces.value = []
}

async function load() {
  const [keyResponse, optionResponse] = await Promise.all([listMCPAccessKeys(), getMCPAccessKeyScopeOptions()])
  keys.value = keyResponse.data || []
  options.value = optionResponse.data || []
  publicUrl.value = optionResponse.mcp_public_url || keyResponse.mcp_public_url || ''
}

async function revoke(id: number) {
  await revokeMCPAccessKey(id)
  await load()
}

async function copySensitive(value: string) {
  await copyWithToast(value, 'common.copied')
  MessagePlugin.warning('配置包含敏感凭证，请仅粘贴到可信的 AI 工具中')
}

async function copySpace(space: MCPConfigSpace) {
  await copySensitive(JSON.stringify(buildMCPServers(publicUrl.value, token.value, [space]), null, 2))
}

function download() {
  const objectURL = URL.createObjectURL(new Blob([jsonConfig.value], { type: 'application/json' }))
  const link = document.createElement('a')
  link.href = objectURL
  link.download = 'weknora-mcp.json'
  link.click()
  URL.revokeObjectURL(objectURL)
}

function scopeSummary(key: MCPAccessKey) {
  return key.tenant_scopes.map(scope => `${options.value.find(item => item.tenant_id === scope.tenant_id)?.tenant_name || scope.tenant_id}（${scope.kb_scope_mode === 'all' ? '全部 KB' : `${scope.knowledge_bases.length} 个 KB`}）`).join('、')
}

function formatTime(value: string) {
  return new Date(value).toLocaleString()
}

function toDatetimeLocal(value: string) {
  const date = new Date(value)
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)
}

watch(configVisible, visible => { if (!visible) clearPlaintextToken() })
onBeforeUnmount(clearPlaintextToken)
onMounted(load)
</script>

<style scoped>
.mcp-keys { display: flex; flex-direction: column; gap: 16px; }
.head { display: flex; justify-content: space-between; align-items: start; }
.head h2 { margin: 0; }
.head p, .hint { color: var(--td-text-color-secondary); }
.scope-list { display: grid; gap: 12px; width: 100%; }
.scope-card, .cherry-card { border: 1px solid var(--td-component-border); border-radius: 8px; padding: 12px; }
.scope-detail, .kb-groups { display: grid; gap: 12px; margin: 10px 0 0 24px; }
.kb-groups strong { display: block; margin-bottom: 6px; }
.expiry-row, .space-copy { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; }
.token-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; padding: 10px 12px; border: 1px solid var(--td-component-border); border-radius: 8px; overflow-wrap: anywhere; }
.native-datetime { border: 1px solid var(--td-component-border); border-radius: 4px; padding: 6px 10px; color: inherit; background: transparent; }
pre { white-space: pre-wrap; max-height: 380px; overflow: auto; background: var(--td-bg-color-secondarycontainer); padding: 16px; border-radius: 8px; }
.cherry-card { margin-top: 10px; line-height: 30px; overflow-wrap: anywhere; }
.space-copy { margin-top: 14px; }
</style>
