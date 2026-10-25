<template>
  <section>
    <h4>{{ t('myDocuments') }}</h4>
    <p><t-button variant="outline" :disabled="busy" @click="createPrivate">{{ t('createPrivateKB') }}</t-button></p>
    <t-select v-model="mode" :options="modes" :disabled="busy" />
    <p v-if="mode === 'user_oauth'">
      <t-button v-if="!connection?.connected" :loading="busy" @click="emit('connect', linksText)">{{ t('connect') }}</t-button>
      <template v-else>{{ t('connected') }} <t-button variant="text" :disabled="busy" @click="disconnect">{{ t('disconnect') }}</t-button></template>
    </p>
    <template v-if="connection?.connected">
      <div class="browse-controls">
        <t-select v-model="kind" :options="[{label:t('drive'),value:'drive'},{label:t('wiki'),value:'wiki'}]" :disabled="busy" />
        <t-button :disabled="busy" @click="loadRoot">{{ t('browse') }}</t-button>
        <t-button v-if="trail.length" variant="outline" :disabled="busy" @click="goParent">{{ t('parent') }}</t-button>
        <t-select v-model="filter" :options="[{label:t('allTypes'),value:''},...['docx','sheet','bitable'].map(value=>({label:value,value}))]" />
      </div>
      <p>{{ t('partialCoverage') }}</p>
      <div class="resources">
        <div v-for="entry in visibleEntries" :key="`${entry.space_id}:${entry.id}`" class="resource-row">
          <t-checkbox :checked="selected.has(entry.id)" :disabled="busy" @change="toggle(entry, !selected.has(entry.id))" />
          <span>{{ entry.name }} · {{ entry.type }}</span>
          <t-button v-if="entry.has_children" variant="text" :disabled="busy" @click="open(entry)">{{ t('browse') }}</t-button>
        </div>
      </div>
      <t-button v-if="cursor" variant="outline" :disabled="busy" @click="loadPage(true)">{{ t('more') }}</t-button>
      <p>{{ t('selectedCount', { count: selected.size }) }} <t-button :disabled="busy || !selected.size" @click="addSelected">{{ t('addSelected') }}</t-button></p>
    </template>
    <t-textarea v-model="linksText" :placeholder="t('multipleLinks')" :disabled="busy" :autosize="{ minRows: 4, maxRows: 10 }" />
    <p>{{ links.length }} / 100 · {{ t('exportPending') }}</p>
    <t-button :disabled="busy || !canSubmit" @click="preview">{{ t('preview') }}</t-button>
    <t-button :disabled="busy || !canSubmit" @click="submit">{{ t('createBatch') }}</t-button>
    <div v-if="mode === 'user_oauth' && previews.some(p => p.error_category === 'sharing_approval_required')">
      <t-checkbox v-model="confirmSharing" :disabled="busy">{{ t('sharingConfirm') }}</t-checkbox>
      <t-input v-model="reason" :placeholder="t('sharingReason')" :maxlength="512" :disabled="busy" />
      <t-button :disabled="busy || !confirmSharing || !reason.trim()" @click="approve">{{ t('approveSharing') }}</t-button>
    </div>
    <ul v-if="previews.length"><li v-for="(row, i) in previews" :key="i">{{ row.resource_type }} · {{ row.error_category || row.status }}</li></ul>
    <div v-if="batch">
      <h4>{{ t('batchState') }} · {{ batch.status }}</h4>
      <p>{{ Object.entries(batch.counts).map(([key, value]) => `${key}: ${value}`).join(' · ') }}</p>
      <ul><li v-for="item in batch.items" :key="item.id">{{ item.ordinal + 1 }} · {{ item.status }} <span v-if="item.error_category">· {{ item.error_category }}</span></li></ul>
      <t-button :disabled="busy" @click="retry">{{ t('retryBatch') }}</t-button>
      <details v-if="batch.items.some(item => ['blocked','failed'].includes(item.status))">
        <summary @click="refreshCollaboration">{{ t('permissionChecklist') }}</summary>
        <p>{{ t('collaborationExplanation') }}</p>
        <t-input v-model="contact" :placeholder="t('permissionContact')" :maxlength="128" :disabled="busy" />
        <t-input v-model="collaborationNote" :placeholder="t('permissionNote')" :maxlength="512" :disabled="busy" />
        <div v-for="item in batch.items.filter(item => ['blocked','failed'].includes(item.status))" :key="item.id">
          <p>{{ item.ordinal + 1 }} · {{ item.error_category }}</p>
          <t-button :disabled="busy || !collaborationNote.trim()" variant="text" @click="collaborate(item.id,'note')">{{ t('recordContact') }}</t-button>
          <t-button :disabled="busy || !collaborationNote.trim()" variant="text" @click="collaborate(item.id,'reported')">{{ t('reportedPermission') }}</t-button>
          <t-button :disabled="busy" variant="text" @click="collaborate(item.id,'recheck')">{{ t('recheckPermission') }}</t-button>
          <ul><li v-for="event in collaboration.filter(event => event.item_id === item.id)" :key="event.id">{{ event.contact }} · {{ event.note }} · {{ event.result }}</li></ul>
        </div>
      </details>
    </div>
    <p v-if="message" role="status">{{ message }}</p>
    <details>
      <summary @click="refreshImports">{{ t('sources') }}</summary>
      <div v-for="row in imports" :key="row.id">
        <p>{{ row.resource_type }} · {{ row.status }} · {{ row.source_status || '' }}</p>
        <t-button v-if="row.index_status === 'verified' && ['synced','completed'].includes(row.status) && !row.data_source_id" :disabled="busy" variant="outline" @click="enableUpdates(row.id)">{{ t('enableUpdates') }}</t-button>
        <template v-if="row.data_source_id">
          <span>{{ t(row.source_status === 'active' ? 'sourceActive' : 'sourcePaused') }}</span>
          <t-button v-if="row.source_status === 'active'" variant="text" :disabled="busy" @click="sourceAction(row.id,'sync')">{{ t('syncNow') }}</t-button>
          <t-button v-if="row.source_status === 'active'" variant="text" :disabled="busy" @click="sourceAction(row.id,'pause')">{{ t('pauseSource') }}</t-button>
          <t-button v-if="['paused','error'].includes(row.source_status || '')" variant="text" :disabled="busy" @click="sourceAction(row.id,'rebind')">{{ t('rebindSource') }}</t-button>
          <t-button v-if="['paused','error'].includes(row.source_status || '')" variant="text" :disabled="busy" @click="sourceAction(row.id,'resume')">{{ t('resumeSource') }}</t-button>
        </template>
      </div>
    </details>
  </section>
</template>
<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { createPrivateFeishuKB } from '@/api/feishuPersonal'
import { listFeishuCollaboration, recordFeishuCollaboration, type FeishuCollaboration } from '@/api/feishuPersonal'
import { listFeishuImports, enableFeishuSource, feishuSourceAction, type FeishuImport } from '@/api/feishuPersonal'
import { feishuMessages } from '@/i18n/feishuPersonal'
import { feishuConnections, feishuResources, disconnectFeishu, previewFeishu, createFeishuBatch, getFeishuBatch, retryFeishuBatch, approveFeishuSharing, isFeishuDocumentURL, type FeishuAuthMode, type FeishuConnection, type FeishuResource, type FeishuPreview, type FeishuBatch, type FeishuStatus } from '@/api/feishuPersonal'
const props = defineProps<{ kbId: string; status: FeishuStatus; initialUrl?: string }>()
const emit = defineEmits<{ connect: [links: string]; imported: [] }>()
const { t } = useI18n({ useScope: 'local', messages: feishuMessages })
const router = useRouter()
const connections = ref<FeishuConnection[]>([]), mode = ref<FeishuAuthMode>('user_oauth')
const modes = computed(() => connections.value.map(c => ({ label: t(c.kind === 'user_oauth' ? 'personalMode' : 'applicationMode'), value: c.kind })))
const connection = computed(() => connections.value.find(c => c.kind === mode.value))
const busy = ref(false), message = ref(''), kind = ref('drive'), filter = ref('')
const entries = ref<FeishuResource[]>([]), cursor = ref(''), trail = ref<{parent: string; space: string}[]>([])
const location = ref({ parent: '', space: '' })
const selected = ref(new Map<string, FeishuResource>())
const visibleEntries = computed(() => entries.value.filter(e => !filter.value || e.type === filter.value || e.has_children))
const linksText = ref(props.initialUrl || ''), previews = ref<FeishuPreview[]>([]), confirmSharing = ref(false), reason = ref('')
const links = computed(() => [...new Set(linksText.value.split(/\s+/).map(s => s.trim()).filter(Boolean))])
const canSubmit = computed(() => connection.value?.connected && links.value.length > 0 && links.value.length <= 100)
const batch = ref<FeishuBatch>(), submissionKey = ref('')
const imports = ref<FeishuImport[]>([])
const collaboration = ref<FeishuCollaboration[]>([]), contact = ref(''), collaborationNote = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
let generation = 0, scopeEpoch = 0, mounted = true
function scope() { const epoch = scopeEpoch; return { kb: props.kbId, mode: mode.value, connection: connection.value?.id || '', links: [...links.value], current: () => mounted && epoch === scopeEpoch } }
function stop() { generation++; if (timer) clearTimeout(timer); timer = undefined }
onUnmounted(() => { mounted = false; stop() })
function toggle(entry: FeishuResource, value: boolean) { if (value) selected.value.set(entry.id, entry); else selected.value.delete(entry.id) }
async function refreshConnections() { connections.value = await feishuConnections(); if (!connections.value.some(c => c.kind === mode.value) && connections.value[0]) mode.value = connections.value[0].kind }
watch(() => props.status, () => refreshConnections().catch(() => { message.value = t('failed') }), { immediate: true, deep: true })
watch([mode, kind, () => props.kbId], () => { scopeEpoch++; stop(); submissionKey.value = ''; entries.value = []; cursor.value = ''; selected.value.clear(); trail.value = []; location.value = { parent: '', space: '' }; previews.value = []; batch.value = undefined; imports.value = [] }, { flush: 'sync' })
watch(linksText, () => { scopeEpoch++; submissionKey.value = ''; previews.value = []; confirmSharing.value = false }, { flush: 'sync' })
function query(parent = location.value.parent, space = location.value.space, next = '') { return { auth_mode: mode.value, connection_id: connection.value?.id || '', kind: kind.value, parent_id: parent, space_id: space, cursor: next, page_size: 50 } }
async function act(action: () => Promise<void>) { busy.value = true; message.value = ''; try { await action() } catch (error: any) { message.value = error?.message || t('failed') } finally { busy.value = false } }
async function loadPage(append = false) { await act(async () => { const s = scope(); const page = await feishuResources(query(undefined, undefined, append ? cursor.value : '')); if (!s.current()) return; entries.value = append ? [...entries.value, ...page.items] : page.items; cursor.value = page.cursor || '' }) }
async function loadRoot() { trail.value = []; location.value = { parent: '', space: '' }; await loadPage() }
async function open(entry: FeishuResource) { trail.value.push({ ...location.value }); location.value = entry.type === 'wiki_space' ? { parent: '', space: entry.id } : { parent: entry.id, space: entry.space_id || location.value.space }; await loadPage() }
async function goParent() { location.value = trail.value.pop() || { parent: '', space: '' }; await loadPage() }
async function addSelected() { await act(async () => {
  const s = scope(), baseQuery = query()
  const found = new Set(links.value), queue: {parent: string;space: string;cursor: string}[] = [], visited = new Set<string>()
  function collect(entry: FeishuResource) {
    if (!['folder','wiki_space'].includes(entry.type) && entry.url && isFeishuDocumentURL(entry.url)) found.add(entry.url)
    if (entry.has_children) queue.push(entry.type === 'wiki_space' ? { parent: '', space: entry.id, cursor: '' } : { parent: entry.id, space: entry.space_id || '', cursor: '' })
  }
  selected.value.forEach(collect)
  let requests = 0
  while (queue.length) {
    if (++requests > 20 || found.size > 100) throw new Error(t('selectionLimit'))
    const next = queue.shift()!, key = `${next.space}:${next.parent}:${next.cursor}`
    if (visited.has(key)) continue
    visited.add(key)
    const page = await feishuResources({ ...baseQuery, parent_id: next.parent, space_id: next.space, cursor: next.cursor })
    if (!s.current()) return
    page.items.forEach(collect)
    if (page.cursor) queue.push({ ...next, cursor: page.cursor })
  }
  if (found.size > 100) throw new Error(t('selectionLimit'))
  linksText.value = [...found].join('\n'); selected.value.clear()
}) }
async function disconnect() { await act(async () => { await disconnectFeishu(); await refreshConnections(); entries.value = []; selected.value.clear() }) }
async function createPrivate() { await act(async () => { const kb = await createPrivateFeishuKB(t('privateKBName')); await router.push(`/platform/knowledge-bases/${encodeURIComponent(kb.id)}`) }) }
async function refreshImports() { const kb = props.kbId; try { const rows = await listFeishuImports(kb); if (kb === props.kbId) imports.value = rows } catch { message.value = t('failed') } }
async function enableUpdates(id: string) { await act(async () => { await enableFeishuSource(props.kbId, id); await refreshImports() }) }
async function sourceAction(id: string, action: 'sync'|'pause'|'resume'|'rebind') { await act(async () => { await feishuSourceAction(props.kbId, id, action); await refreshImports() }) }
async function preview() { await act(async () => { const s = scope(); const rows = await previewFeishu(s.kb, s.mode, s.connection, s.links); if (s.current()) previews.value = rows }) }
async function approve() { await act(async () => { const s = scope(), rows = [...previews.value], note = reason.value, confirmed = confirmSharing.value; for (const row of rows.filter(p => p.error_category === 'sharing_approval_required')) { if (!s.current()) return; await approveFeishuSharing(s.kb, s.connection, row.url, note, confirmed) } if (!s.current()) return; const next = await previewFeishu(s.kb, s.mode, s.connection, s.links); if (s.current()) { previews.value = next; confirmSharing.value = false } }) }
async function poll(id: string, version: number) { try { const next = await getFeishuBatch(id); if (!mounted || version !== generation) return; batch.value = next; if (next.status === 'succeeded' || next.status === 'partial_failed') { emit('imported'); return } } catch { /* A read failure never resubmits a batch. */ } if (mounted && version === generation) timer = setTimeout(() => poll(id, version), 4000) }
async function submit() { await act(async () => { const s = scope(); if (!submissionKey.value) submissionKey.value = crypto.randomUUID(); const next = await createFeishuBatch(s.kb, s.mode, s.connection, s.links, submissionKey.value); if (!s.current()) return; batch.value = next; stop(); void poll(next.batch.id, generation) }) }
async function retry() { if (batch.value) { const id = batch.value.batch.id, s = scope(); await act(async () => { await retryFeishuBatch(id); if (!s.current()) return; stop(); void poll(id, generation) }) } }
async function refreshCollaboration() { const id = batch.value?.batch.id, s = scope(); if (!id) return; try { const rows = await listFeishuCollaboration(id); if (s.current() && batch.value?.batch.id === id) collaboration.value = rows } catch { if (s.current()) message.value = t('failed') } }
async function collaborate(item: string, action: 'note'|'reported'|'recheck') { const id = batch.value?.batch.id, s = scope(); if (!id) return; await act(async () => { await recordFeishuCollaboration(id, item, action, contact.value, collaborationNote.value); if (s.current()) await refreshCollaboration() }) }
</script>
<style scoped>
.browse-controls { display: flex; gap: 8px; margin-top: 12px; flex-wrap: wrap; }
.browse-controls > :first-child, .browse-controls > :last-child { width: 160px; }
.resources { max-height: 260px; overflow: auto; }
.resource-row { display: flex; gap: 8px; align-items: center; padding: 6px 0; }
.resource-row span { flex: 1; overflow-wrap: anywhere; }
</style>
