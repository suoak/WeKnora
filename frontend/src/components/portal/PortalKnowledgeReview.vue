<template>
  <section class="publication" aria-live="polite">
    <h2>企业公共知识范围</h2>
    <p>空间 Owner 确认普通知识库后，由另一位获授权的 Portal 管理员审核。已有 Portal 发布状态不代表已完成企业共享确认。</p>
    <t-button variant="outline" :loading="loading" @click="load">刷新范围与状态</t-button>
    <p v-if="error" role="alert">{{ error }}</p>
    <template v-else-if="response">
      <p>当前 {{ response.data.length }} 个知识库，{{ approved }} 个已有批准记录。公共 MCP 生产入口保持关闭。</p>
      <t-alert v-if="!response.approval_mutations_enabled" theme="info">当前环境尚未启用企业公开审核；可查看范围和历史，不能提交批准。</t-alert>
      <table><thead><tr><th>知识库</th><th>文档</th><th>未就绪</th><th>外部/未知来源线索</th><th>审批状态</th><th>版本</th><th>操作</th></tr></thead>
        <tbody><tr v-for="row in response.data" :key="row.knowledge_base_id"><td>{{ row.name }}</td><td>{{ row.documents }}</td><td>{{ row.unready }}</td><td>{{ row.external_or_unknown }}</td><td>{{ row.policy_state }}</td><td>{{ row.policy_revision }}</td><td><t-button variant="text" :disabled="busy" @click="open(row)">确认与审核记录</t-button></td></tr></tbody>
      </table>
      <p v-if="!response.data.length">当前空间没有知识库。</p>
    </template>
    <section v-if="selected && snapshot" class="review">
      <h3>{{ selected.name }} · {{ snapshot.state }}</h3>
      <p>范围：本知识库全部 {{ selected.documents }} 篇文档。提出者：{{ snapshot.proposed_by || '尚未确认' }}；审核者：{{ snapshot.reviewed_by || '尚未审核' }}；审核时间：{{ snapshot.reviewed_at || '—' }}。</p>
      <p v-if="snapshot.proposed_by && !snapshot.proposer_active_owner">提出者已不再是有效 Owner，需要当前 Owner 重新确认。</p>
      <p>资源变化后须重新确认；敏感、混合、未知来源保持受限。撤回优先生效，不影响独立的私人授权。</p>
      <t-textarea v-model="reason" :maxlength="512" placeholder="填写确认依据、审核意见或撤回原因（必填）" :disabled="busy" />
      <label><input v-model="ordinary" type="checkbox" :disabled="busy" />我已核对全部文档，确认均为允许企业共享的普通内容，且来源权限明确。</label>
      <label><input v-model="inherit" type="checkbox" :disabled="busy" />允许后续通过可信分类的普通文档继承此范围</label>
      <div class="actions">
        <t-button v-if="snapshot.permissions.owner" :disabled="!can('propose')" :loading="busy" @click="act('propose')">Owner 确认并提交</t-button>
        <t-button v-if="snapshot.permissions.reviewer" :disabled="!can('approve')" :loading="busy" @click="act('approve')">审核并批准企业共享</t-button>
        <t-button theme="danger" variant="outline" :disabled="!can('revoke')" :loading="busy" @click="act('revoke')">撤回公共范围</t-button>
        <t-button variant="text" :disabled="busy" @click="selected = null; snapshot = null">关闭</t-button>
      </div>
      <p v-if="message" role="status">{{ message }}</p>
      <h4>最近 50 条知识库审批记录</h4>
      <table><thead><tr><th>时间</th><th>实际主体 user_id</th><th>操作</th><th>版本与依据</th></tr></thead><tbody><tr v-for="item in history" :key="item.id"><td>{{ item.created_at }}</td><td>{{ item.actor_user_id || '系统继承' }}</td><td>{{ item.action }}</td><td>{{ JSON.stringify(item.details) }}</td></tr></tbody></table>
    </section>
  </section>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { get, post } from '@/utils/request'
import { publicationBase, publicationActionAllowed, type PublicationSnapshot } from './publicationWorkflow'
const props = withDefaults(defineProps<{ tenantId: number; ownerView?: boolean }>(), { ownerView: false })
interface Row { knowledge_base_id: string; name: string; documents: number; unready: number; external_or_unknown: number; policy_state: string; policy_revision: number; inherit_new_ordinary: boolean }
interface Inventory { data: Row[]; public_mcp_enabled: boolean; approval_mutations_enabled: boolean }
interface Audit { id: string; actor_user_id: string; action: string; created_at: string; details: unknown }
const response = ref<Inventory | null>(null), loading = ref(false), error = ref(''), busy = ref(false)
const selected = ref<Row | null>(null), snapshot = ref<PublicationSnapshot | null>(null), history = ref<Audit[]>([])
const reason = ref(''), ordinary = ref(false), inherit = ref(false), message = ref('')
const approved = computed(() => response.value?.data.filter(row => row.policy_state === 'approved').length || 0)
const base = () => publicationBase(props.tenantId, props.ownerView)
let generation = 0
async function load() {
  const request = ++generation, path = base()
  loading.value = true; response.value = null; selected.value = null; snapshot.value = null; error.value = ''
  try { const result = await get(path) as Inventory; if (request === generation) response.value = result }
  catch { if (request === generation) error.value = '无法加载范围，请检查权限或稍后重试。' }
  finally { if (request === generation) loading.value = false }
}
async function open(row: Row) {
  const request = ++generation, path = `${base()}/${encodeURIComponent(row.knowledge_base_id)}`
  selected.value = row; snapshot.value = null; history.value = []; reason.value = ''; ordinary.value = false; message.value = ''; busy.value = true
  try {
    const [review, audit] = await Promise.all([get(path), get(`${path}/history`)]) as [{ data: PublicationSnapshot }, { data: Audit[] }]
    if (request === generation) { snapshot.value = review.data; history.value = audit.data; inherit.value = review.data.inherit_new_ordinary }
  } catch { if (request === generation) error.value = '无法取得当前审批版本，请刷新后重试。' }
  finally { if (request === generation) busy.value = false }
}
function can(action: 'propose' | 'approve' | 'revoke') {
  return !busy.value && !!snapshot.value && publicationActionAllowed(snapshot.value, action, reason.value, ordinary.value)
}
async function act(action: 'propose' | 'approve' | 'revoke') {
  if (!can(action) || !selected.value || !snapshot.value) return
  const request = generation, row = selected.value, path = `${base()}/${encodeURIComponent(row.knowledge_base_id)}/${action}`
  const resources = action === 'revoke' ? snapshot.value.resources.filter(r => r.kind === 'kb') : snapshot.value.resources
  busy.value = true; message.value = ''
  try {
    await post(path, { reason: reason.value.trim(), inherit_new_ordinary: inherit.value, resources })
    if (request === generation) { await open(row); message.value = action === 'approve' ? '审核已保存；能否检索仍取决于入口配置和最新权限。' : '操作已保存。' }
  } catch { if (request === generation) message.value = '操作未完成。权限或资源版本可能已变化，请重新打开记录后确认。' }
  finally { if (request === generation) busy.value = false }
}
watch(() => [props.tenantId, props.ownerView], () => { busy.value = false; void load() }, { immediate: true })
</script>
<style scoped>
.publication{padding:20px;background:var(--td-bg-color-container);border-radius:12px;overflow:auto}.publication p{line-height:1.6}table{width:100%;border-collapse:collapse;margin-top:16px;font-size:13px}th,td{text-align:left;padding:10px;border-bottom:1px solid var(--td-component-border);overflow-wrap:anywhere}.review{margin-top:24px;border-top:1px solid var(--td-component-border);padding-top:16px}.review label{display:block;margin:12px 0}.actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:16px}
</style>
