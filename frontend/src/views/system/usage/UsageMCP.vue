<template>
  <div class="table-scroll">
    <table v-if="rows.length">
      <thead>
        <tr>
          <th v-for="key in keys" :key="key">
            {{ t(`usageAnalytics.columns.${key}`) }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, index) in rows" :key="`${row.tool_name || row.client_name || row.knowledge_base_id}:${index}`">
          <td v-for="key in keys" :key="key">{{ cell(row, key) }}</td>
        </tr>
      </tbody>
    </table>
    <div v-else class="empty">{{ t('usageAnalytics.empty') }}</div>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MCPUsageRow } from '@/api/usage-analytics'
const props = defineProps<{
  rows: MCPUsageRow[]
  groupBy: 'tool' | 'client' | 'knowledge_base'
}>()
const { t } = useI18n()
const keys = computed<(keyof MCPUsageRow)[]>(() => [
  props.groupBy === 'client' ? 'client_name' : props.groupBy === 'knowledge_base' ? 'knowledge_base_name' : 'tool_name',
  'calls',
  'successful_calls',
  'success_rate',
  'average_latency_ms',
  'unattributed_calls',
  'last_active',
])
function cell(row: MCPUsageRow, key: keyof MCPUsageRow) {
  const value = row[key]
  if (key === 'last_active') return value ? new Date(String(value)).toLocaleString() : '—'
  if (key === 'success_rate') return `${Number(value || 0).toFixed(1)}%`
  return typeof value === 'number' ? new Intl.NumberFormat().format(value) : String(value || '—')
}
</script>
<style scoped lang="less">
.table-scroll {
  overflow: auto;
}
table {
  border-collapse: collapse;
  width: 100%;
  font-size: 13px;
}
th,
td {
  text-align: left;
  white-space: nowrap;
  padding: 12px 14px;
  border-bottom: 1px solid var(--td-component-stroke);
}
th {
  background: var(--td-bg-color-secondarycontainer);
}
.empty {
  padding: 48px;
  text-align: center;
}
</style>
