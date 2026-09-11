<template>
  <div class="table-scroll">
    <table v-if="rows.length">
      <thead>
        <tr>
          <th v-for="key in keys" :key="key">
            {{ key === 'mcp_adopted'
              ? t('usageAnalytics.governance.mcpAdoption')
              : t(`usageAnalytics.governance.columns.${key}`) }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in rows" :key="row.tenant_id">
          <td>{{ row.tenant_name }}</td>
          <td>
            <span class="status" :data-status="row.usage_status">{{
              t(`usageAnalytics.governance.status.${row.usage_status}`)
            }}</span>
          </td>
          <td>{{ date(row.last_active) }}</td>
          <td>
            <span class="adoption" :data-adopted="row.mcp_adopted">
              {{ t(`usageAnalytics.governance.filters.${row.mcp_adopted ? 'adopted' : 'notAdopted'}`) }}
            </span>
          </td>
          <td>{{ number(row.kb_used_count) }}</td>
          <td>
            {{ number(row.cross_tenant_accesses + row.owned_kb_cross_tenant_accesses) }}
          </td>
          <td>{{ number(row.total_tokens) }}</td>
          <td>{{ number(row.mcp_calls) }}</td>
        </tr>
      </tbody>
    </table>
    <div v-else class="empty">{{ t('usageAnalytics.empty') }}</div>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { TenantUsageRow } from '@/api/usage-analytics'
defineProps<{ rows: TenantUsageRow[] }>()
const { t } = useI18n()
const keys = [
  'tenant_name',
  'usage_status',
  'last_active',
  'mcp_adopted',
  'kb_used_count',
  'cross_tenant_accesses',
  'total_tokens',
  'mcp_calls',
]
const number = (value?: number) => new Intl.NumberFormat().format(value ?? 0)
const date = (value?: string) => (value ? new Date(value).toLocaleString() : '—')
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
.status {
  display: inline-block;
  padding: 3px 8px;
  border-radius: 10px;
  background: var(--td-bg-color-component);
}
.status[data-status='active'] {
  color: var(--td-success-color);
}
.status[data-status='inactive'],
.status[data-status='never_used'] {
  color: var(--td-error-color);
}
.status[data-status='insufficient_data'] {
  color: var(--td-warning-color);
}
.adoption {
  color: var(--td-text-color-placeholder);
}
.adoption[data-adopted='true'] {
  color: var(--td-success-color);
  font-weight: 600;
}
.empty {
  padding: 48px;
  text-align: center;
  color: var(--td-text-color-placeholder);
}
</style>
