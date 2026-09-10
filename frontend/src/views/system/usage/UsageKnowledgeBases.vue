<template>
  <div class="table-scroll">
    <table v-if="rows.length">
      <thead>
        <tr>
          <th v-for="key in keys" :key="key">
            {{ t(`usageAnalytics.governance.columns.${key}`) }}
          </th>
        </tr>
      </thead>
      <tbody>
        <template v-for="row in rows" :key="row.knowledge_base_id">
          <tr class="clickable" @click="toggle(row)">
            <td>{{ row.knowledge_base_name }}</td>
            <td>{{ row.owner_tenant_name }}</td>
            <td>
              <span class="status" :data-status="row.usage_status">{{
                t(`usageAnalytics.governance.status.${row.usage_status}`)
              }}</span>
            </td>
            <td>{{ number(row.total_accesses) }}</td>
            <td>{{ number(row.model_accesses) }}</td>
            <td>{{ number(row.mcp_calls) }}</td>
            <td>
              <strong>{{
                t('usageAnalytics.governance.usedBySpaces', {
                  count: row.unique_tenants,
                })
              }}</strong>
            </td>
            <td>{{ number(row.unique_principals) }}</td>
            <td>
              {{
                t('usageAnalytics.governance.externalSpaces', {
                  count: row.external_tenants,
                })
              }}
            </td>
            <td>{{ number(row.cross_tenant_accesses) }} ({{ percent(row.cross_tenant_share) }})</td>
            <td>{{ date(row.last_active) }}</td>
            <td>{{ signedPercent(row.recent_growth) }}</td>
          </tr>
          <tr v-if="expanded === row.knowledge_base_id" class="detail">
            <td colspan="12">
              <div v-if="loading === row.knowledge_base_id">
                {{ t('usageAnalytics.loading') }}
              </div>
              <div v-else class="detail-grid">
                <section>
                  <h4>
                    {{ t('usageAnalytics.governance.callerDistribution') }}
                  </h4>
                  <ul>
                    <li
                      v-for="caller in details[row.knowledge_base_id]?.callers || []"
                      :key="String(caller.caller_tenant_id || 'unknown')"
                    >
                      <span>{{ caller.caller_tenant_name || t('usageAnalytics.unattributed') }}</span
                      ><strong>{{ number(caller.assistant_turns + caller.mcp_calls) }}</strong>
                    </li>
                  </ul>
                </section>
                <section>
                  <h4>{{ t('usageAnalytics.governance.usageTrend') }}</h4>
                  <ul>
                    <li v-for="point in details[row.knowledge_base_id]?.trend || []" :key="point.bucket">
                      <span>{{ new Date(point.bucket).toLocaleDateString() }}</span
                      ><strong
                        >{{ number(point.model_accesses) }} / {{ number(point.mcp_calls) }} ·
                        {{ number(point.internal_accesses) }} / {{ number(point.external_accesses) }}</strong
                      >
                    </li>
                  </ul>
                </section>
              </div>
            </td>
          </tr>
        </template>
      </tbody>
    </table>
    <div v-else class="empty">{{ t('usageAnalytics.empty') }}</div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getKnowledgeBaseUsage,
  getUsageTimeSeries,
  type KnowledgeBaseUsageRow,
  type UsageQuery,
  type UsageTimeSeriesPoint,
} from '@/api/usage-analytics'
const props = defineProps<{
  rows: KnowledgeBaseUsageRow[]
  query: UsageQuery
}>()
const { t } = useI18n()
const expanded = ref('')
const loading = ref('')
const details = reactive<Record<string, { callers: KnowledgeBaseUsageRow[]; trend: UsageTimeSeriesPoint[] }>>({})
const keys = [
  'knowledge_base_name',
  'owner_tenant_name',
  'usage_status',
  'total_accesses',
  'model_accesses',
  'mcp_calls',
  'unique_tenants',
  'unique_principals',
  'external_tenants',
  'cross_tenant_accesses',
  'last_active',
  'recent_growth',
]
const number = (value?: number) => new Intl.NumberFormat().format(value ?? 0)
const percent = (value?: number) => `${(value ?? 0).toFixed(1)}%`
const signedPercent = (value?: number) => `${(value ?? 0) > 0 ? '+' : ''}${(value ?? 0).toFixed(1)}%`
const date = (value?: string) => (value ? new Date(value).toLocaleString() : '—')
async function toggle(row: KnowledgeBaseUsageRow) {
  const id = row.knowledge_base_id
  if (expanded.value === id) {
    expanded.value = ''
    return
  }
  expanded.value = id
  if (details[id]) return
  loading.value = id
  try {
    const base = {
      ...props.query,
      knowledge_base_id: id,
      page: 1,
      page_size: 100,
    }
    const [callers, trend] = await Promise.all([
      getKnowledgeBaseUsage({ ...base, group_by: 'caller' }),
      getUsageTimeSeries({
        ...base,
        metric: 'accesses',
        dimension: 'source',
        interval: 'day',
      }),
    ])
    details[id] = { callers: callers.data || [], trend: trend.data || [] }
  } finally {
    loading.value = ''
  }
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
.clickable {
  cursor: pointer;
}
.clickable:hover {
  background: var(--td-bg-color-container-hover);
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
.detail td {
  white-space: normal;
  background: var(--td-bg-color-secondarycontainer);
}
.detail-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 24px;
}
.detail h4 {
  margin: 0 0 8px;
}
.detail ul {
  margin: 0;
  padding: 0;
  list-style: none;
}
.detail li {
  display: flex;
  justify-content: space-between;
  padding: 5px 0;
}
.empty {
  padding: 48px;
  text-align: center;
}
@media (max-width: 900px) {
  .detail-grid {
    grid-template-columns: 1fr;
  }
}
</style>
