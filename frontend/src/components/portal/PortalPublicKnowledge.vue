<template>
  <section class="public-knowledge" aria-live="polite">
    <h2>知识库检索设置</h2>
    <t-button variant="outline" :loading="loading" @click="load">刷新公共范围</t-button>
    <p v-if="error" role="alert">{{ error }}</p>
    <template v-if="scope">
      <p>Portal：{{ scope.portal_published ? '已发布' : '未发布，不提供企业公共读取' }}；{{ scope.knowledge_bases.length }} 个有效知识库，{{ blockedCount }} 个已禁止企业公共检索。</p>
      <table><thead><tr><th>知识库</th><th>文档 / 已就绪</th><th>企业公共范围</th><th>例外版本</th><th>操作</th></tr></thead><tbody>
        <tr v-for="row in scope.knowledge_bases" :key="row.knowledge_base_id" :data-kb-id="row.knowledge_base_id" :data-blocked="row.public_blocked"><td>{{ row.name }}</td><td>{{ row.documents }} / {{ row.ready_documents }}</td><td>{{ row.public_blocked ? '已禁止企业公共检索' : scope.portal_published ? '随 Portal 动态开放' : '未发布' }}</td><td>{{ row.revision }}</td><td><t-button variant="text" :disabled="busy || !scope.exception_management_enabled" @click="select(row)">{{ row.public_blocked ? '恢复 Portal 默认范围' : '禁止企业公共检索' }}</t-button></td></tr>
      </tbody></table>
      <p>取消公共检索不会授予或移除员工本人的独立私人权限。Portal 取消发布、归档、空间停用或 KB 删除后，公共读取按最新状态收回。</p>
    </template>
    <div v-if="selected" class="exception-editor">
      <h3>{{ selected.name }} · {{ selected.public_blocked ? '恢复 Portal 默认范围' : '禁止企业公共检索' }}</h3>
      <p>上次操作：{{ selected.updated_by || '无显式例外' }}；原因：{{ selected.reason || '—' }}</p>
      <t-textarea data-testid="exception-reason" v-model="reason" :maxlength="512" placeholder="填写敏感例外或恢复默认范围的原因" :disabled="busy" />
      <t-button data-testid="exception-save" :theme="selected.public_blocked ? 'primary' : 'danger'" :disabled="!reason.trim()" :loading="busy" @click="save">保存例外设置</t-button>
      <t-button variant="text" :disabled="busy" @click="selected = null">取消</t-button>
    </div>
  </section>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { get, put } from '@/utils/request'
const props = withDefaults(defineProps<{ tenantId: number; ownerView?: boolean }>(), { ownerView: false })
interface KB { knowledge_base_id: string; name: string; documents: number; ready_documents: number; public_blocked: boolean; revision: number; reason: string; updated_by: string }
interface Scope { knowledge_bases: KB[]; portal_published: boolean; exception_management_enabled: boolean; rule: string }
const scope = ref<Scope | null>(null), selected = ref<KB | null>(null), loading = ref(false), busy = ref(false), reason = ref(''), error = ref('')
const blockedCount = computed(() => scope.value?.knowledge_bases.filter(k => k.public_blocked).length || 0)
const base = () => props.ownerView ? `/api/v1/tenants/${props.tenantId}/public-knowledge` : `/api/v1/system/admin/portal/spaces/${props.tenantId}/public-knowledge`
let generation = 0
async function load() {
  const request = ++generation, path = base()
  loading.value = true; error.value = ''; selected.value = null; scope.value = null
  try { const response = await get(path) as { data: Scope }; if (request === generation) scope.value = response.data }
  catch { if (request === generation) error.value = '无法取得公共范围，请检查权限或稍后重试。' }
  finally { if (request === generation) loading.value = false }
}
function select(row: KB) { selected.value = row; reason.value = '' }
async function save() {
  if (!selected.value || !reason.value.trim() || busy.value) return
  const request = generation, row = selected.value, path = `${base()}/${encodeURIComponent(row.knowledge_base_id)}/exception`
  busy.value = true
  try {
    await put(path, { public_blocked: !row.public_blocked, revision: row.revision, reason: reason.value.trim() })
    if (request === generation) await load()
  } catch { if (request === generation) error.value = '设置未保存；权限或版本可能已变化，请刷新后重试。' }
  finally { busy.value = false }
}
watch(() => [props.tenantId, props.ownerView], () => { void load() }, { immediate: true })
</script>
<style scoped>
.public-knowledge{margin-top:20px;padding:20px;background:var(--td-bg-color-container);border-radius:12px;overflow:auto}.public-knowledge p{line-height:1.6}table{width:100%;border-collapse:collapse;margin-top:16px;font-size:13px}th,td{text-align:left;padding:10px;border-bottom:1px solid var(--td-component-border)}.exception-editor{margin-top:18px;border-top:1px solid var(--td-component-border);padding-top:12px}.exception-editor button{margin-top:12px;margin-right:8px}
</style>
