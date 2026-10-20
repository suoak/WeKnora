<template>
  <t-dialog :visible="visible" :header="t('title')" :footer="false" @close="emit('update:visible', false)">
    <p v-if="!status.available">{{ t('unavailable') }}</p>
    <template v-else>
      <p v-if="status.auth_mode === 'application_grant'">{{ t(tablesEnabled ? 'applicationTablesInstructions' : 'applicationInstructions') }}</p>
      <t-button v-if="status.auth_mode !== 'application_grant' && !status.connected" :loading="busy" @click="connect">{{ t('connect') }}</t-button>
      <div v-else-if="status.auth_mode !== 'application_grant'">{{ t('connected') }} <t-button variant="text" :disabled="busy" @click="disconnect">{{ t('disconnect') }}</t-button></div>
      <t-input v-model="url" :placeholder="t('url')" :disabled="busy" style="margin: 16px 0" />
      <p style="font-size: var(--app-text-sm); color: var(--td-text-color-secondary)">{{ t('sharingShort') }}</p>
      <t-button :disabled="!canImport || !url.trim()" :loading="busy" @click="runImport">{{ t('import') }}</t-button>
      <details v-if="rows.length" style="margin-top: 16px">
        <summary>{{ t('recent') }}</summary>
        <div v-for="row in rows.slice(0, 5)" :key="row.id" style="margin-top: 12px">
          <span>{{ row.resource_type }} · {{ importStatus(row.status) }}</span>
          <p v-if="row.index_status">{{ t('indexState') }}: {{ t(row.index_status) }} · {{ t('wikiState') }}: {{ t(row.wiki_status || 'not_started') }}</p>
          <p v-if="row.error_category">{{ importError(row.error_category) }}</p>
          <a :href="row.url" target="_blank" rel="noopener noreferrer">{{ t('source') }}</a>
          <router-link v-if="row.knowledge_id" :to="{ path: `/platform/knowledge-bases/${props.kbId}`, query: { knowledge_id: row.knowledge_id } }" style="margin-left: 12px">{{ t('open') }}</router-link>
        </div>
      </details>
    </template>
    <p v-if="resultMessage" role="status">{{ resultMessage }}</p>
  </t-dialog>
</template>
<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import { feishuMessages, FEISHU_PENDING } from '@/i18n/feishuPersonal'
import { feishuStatus, authorizeFeishu, disconnectFeishu, importFeishu, listFeishuImports, type FeishuStatus, type FeishuImport } from '@/api/feishuPersonal'
const props = defineProps<{ visible: boolean; kbId: string; initialUrl?: string }>()
const emit = defineEmits<{ 'update:visible': [value: boolean]; imported: [] }>()
const { t } = useI18n({ useScope: 'local', messages: feishuMessages })
const status = ref<FeishuStatus>({ configured: false, available: false, connected: false })
const canImport = computed(() => status.value.available && (status.value.auth_mode === 'application_grant' || status.value.connected))
const tablesEnabled = computed(() => ['sheet', 'bitable'].every(type => status.value.supported_resource_types?.includes(type)))
const rows = ref<FeishuImport[]>([])
function importError(category: string) { return t(['candidate_media_not_supported', 'candidate_content_incomplete'].includes(category) ? category : 'recoveryReview') }
function importStatus(value: string) { return t(['synced', 'completed', 'attention', 'needs_attention', 'indexed', 'index_pending', 'accepted'].includes(value) ? value : 'pending') }
const url = ref(''), busy = ref(false), resultMessage = ref('')
// Admission may precede public Knowledge creation. Keep observing that handoff
// after the dialog closes; the existing KB list then polls parse/index status.
const awaitingKnowledge = new Set<string>()
let timer: ReturnType<typeof setTimeout> | undefined
let generation = 0
let mounted = true
let historyTimer: ReturnType<typeof setTimeout> | undefined
function stopHistory() { if (historyTimer) clearTimeout(historyTimer); historyTimer = undefined }
async function refreshHistory(kb: string) {
  try {
    const imports = await listFeishuImports(kb)
    if (mounted && props.visible && props.kbId === kb) rows.value = imports
  } catch { /* A status read cannot change a durable task. */ }
  if (mounted && props.visible && props.kbId === kb) historyTimer = setTimeout(() => refreshHistory(kb), 5000)
}
function stopPolling() { generation++; if (timer) clearTimeout(timer); timer = undefined }
onUnmounted(() => { mounted = false; stopPolling(); stopHistory() })
watch(() => props.kbId, () => { awaitingKnowledge.clear(); rows.value = []; stopPolling(); stopHistory() })
async function pollAccepted(kb: string, version: number) {
  if (!mounted || version !== generation || props.kbId !== kb || !awaitingKnowledge.size) return
  try {
    const rows = await listFeishuImports(kb)
    if (!mounted || version !== generation || props.kbId !== kb) return
    let refresh = false
    for (const row of rows) {
      if (!awaitingKnowledge.has(row.id)) continue
      if (row.knowledge_id) { awaitingKnowledge.delete(row.id); refresh = true }
      else if (row.status === 'needs_attention' || row.status === 'attention') {
        awaitingKnowledge.delete(row.id)
        MessagePlugin.error(t('failed'))
      }
    }
    if (refresh) emit('imported')
  } catch { /* Background work is durable; a polling failure must not resubmit it. */ }
  if (version === generation && props.kbId === kb && awaitingKnowledge.size) timer = setTimeout(() => pollAccepted(kb, version), 3000)
}
watch(() => props.visible, async value => {
  stopHistory()
  if (!value) return
  const kb = props.kbId
  url.value = props.initialUrl || ''; resultMessage.value = ''
  try {
    const next = await feishuStatus()
    if (props.visible && props.kbId === kb) status.value = next
    const imports = await listFeishuImports(kb)
    if (props.visible && props.kbId === kb) rows.value = imports
    if (mounted && props.visible && props.kbId === kb) historyTimer = setTimeout(() => refreshHistory(kb), 5000)
  } catch { if (props.visible && props.kbId === kb) resultMessage.value = t('failed') }
})
async function connect() {
  busy.value = true
  try {
    sessionStorage.setItem(FEISHU_PENDING, JSON.stringify({ kbId: props.kbId, url: url.value, path: location.pathname }))
    const result = await authorizeFeishu(), target = new URL(result.authorization_url)
    if (target.origin !== 'https://accounts.feishu.cn') throw new Error('invalid_authorization_origin')
    location.assign(target.href)
  } catch { resultMessage.value = t('failed') } finally { busy.value = false }
}
async function disconnect() {
  busy.value = true
  try { await disconnectFeishu(); status.value = await feishuStatus() } catch { resultMessage.value = t('failed') } finally { busy.value = false }
}
async function runImport() {
  const kb = props.kbId
  busy.value = true; resultMessage.value = ''
  try {
    const row = await importFeishu(kb, url.value)
    if (mounted && props.kbId === kb) {
      if (row.id && !row.knowledge_id) awaitingKnowledge.add(row.id)
      emit('imported')
      emit('update:visible', false)
      stopPolling()
      if (awaitingKnowledge.size) timer = setTimeout(() => pollAccepted(kb, generation), 3000)
    }
    MessagePlugin.success(t('queued'))
  } catch (error: any) { resultMessage.value = error?.message || t('failed') }
  finally { busy.value = false }
}
</script>
