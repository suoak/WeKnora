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
        <tr v-for="row in rows" :key="`${row.model_id}:${row.model_type}`">
          <td v-for="key in keys" :key="key">{{ cell(row, key) }}</td>
        </tr>
      </tbody>
    </table>
    <div v-else class="empty">{{ t('usageAnalytics.empty') }}</div>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ModelUsageRow } from '@/api/usage-analytics'
defineProps<{ rows: ModelUsageRow[] }>()
const { t } = useI18n()
const keys: (keyof ModelUsageRow)[] = [
  'model_id',
  'model_type',
  'assistant_turns',
  'input_tokens',
  'output_tokens',
  'total_tokens',
  'cache_read_tokens',
  'cache_write_tokens',
  'last_active',
]
function cell(row: ModelUsageRow, key: keyof ModelUsageRow) {
  const value = row[key]
  if (key === 'last_active') return value ? new Date(String(value)).toLocaleString() : '—'
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
