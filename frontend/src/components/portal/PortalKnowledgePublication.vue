<template>
  <section class="publication" aria-live="polite">
    <h2>企业公共知识</h2>
    <p>Portal 发布仅决定发现范围。历史 KB 须先由知识负责人确认，再由正式授权的发布审核人确认；空间 Owner 和系统管理员身份不会自动取得此审核权。</p>
    <t-button variant="outline" :loading="loading" @click="load">刷新安全确认清单</t-button>
    <p v-if="error" role="alert">{{ error }}</p>
    <template v-else-if="response">
      <p>公共 MCP：{{ response.public_mcp_enabled ? '已启用' : '未启用' }} · {{ response.data.length }} 个 KB · {{ approved }} 个记录已确认 · {{ response.data.length - approved }} 个待审核或受限</p>
      <t-alert theme="info">审核角色正式映射尚待确认，当前只提供清单，不执行公开批准。已有批准记录也需通过实时版本、身份和来源校验后才可检索。未知来源不等于已确认敏感。</t-alert>
      <table><thead><tr><th>知识库</th><th>知识数量</th><th>未就绪</th><th>外部或未知来源线索</th><th>确认记录</th><th>版本</th><th>新增继承</th></tr></thead>
        <tbody><tr v-for="row in response.data" :key="row.knowledge_base_id"><td>{{ row.name }}</td><td>{{ row.documents }}</td><td>{{ row.unready }}</td><td>{{ row.external_or_unknown }}</td><td>{{ row.policy_state }}</td><td>{{ row.policy_revision }}</td><td>{{ row.inherit_new_ordinary ? '已设置，仍需可信来源校验' : '关闭' }}</td></tr></tbody>
      </table>
      <p v-if="!response.data.length">当前空间没有可供首次确认的知识库。</p>
    </template>
  </section>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { get } from '@/utils/request'
const props = defineProps<{ tenantId: number }>()
interface Row { knowledge_base_id: string; name: string; documents: number; unready: number; external_or_unknown: number; policy_state: string; policy_revision: number; inherit_new_ordinary: boolean }
interface Inventory { data: Row[]; public_mcp_enabled: boolean; approval_mutations_enabled: boolean }
const response = ref<Inventory | null>(null), loading = ref(false), error = ref('')
const approved = computed(() => response.value?.data.filter(row => row.policy_state === 'approved').length || 0)
let generation = 0
async function load() {
  const request = ++generation, tenant = props.tenantId
  loading.value = true; response.value = null; error.value = ''
  try {
    const result = await get(`/api/v1/system/admin/portal/spaces/${tenant}/knowledge-publication`) as Inventory
    if (request === generation && tenant === props.tenantId) response.value = result
  } catch { if (request === generation) error.value = '无法加载确认清单，请检查管理权限或稍后重试。' }
  finally { if (request === generation) loading.value = false }
}
watch(() => props.tenantId, load, { immediate: true })
</script>
<style scoped>
.publication{padding:20px;background:var(--td-bg-color-container);border-radius:12px;overflow:auto}.publication p{line-height:1.6}table{width:100%;border-collapse:collapse;margin-top:16px;font-size:13px}th,td{text-align:left;padding:10px;border-bottom:1px solid var(--td-component-border)}
</style>
