<template>
  <div>
    <div class="kpi-grid kpi-grid--primary">
      <article>
        <span>{{ t('usageAnalytics.metrics.activeTenants') }} (30d)</span
        ><strong>{{ number(overview?.active_tenants) }}</strong>
      </article>
      <article>
        <span>{{ t('usageAnalytics.governance.metrics.activeKnowledgeBases') }} (30d)</span
        ><strong>{{ number(overview?.active_knowledge_bases) }}</strong>
      </article>
      <article>
        <span>{{ t('usageAnalytics.governance.metrics.crossSpaceUsage') }}</span
        ><strong>{{ number(overview?.cross_tenant_usage) }}</strong>
      </article>
      <article>
        <span>{{ t('usageAnalytics.governance.mcpAdoption') }}</span
        ><strong>{{ percent(overview?.mcp_platform_penetration) }}</strong>
      </article>
    </div>

    <div class="kpi-grid kpi-grid--secondary">
      <article>
        <span>{{ t('usageAnalytics.metrics.tokens') }}</span
        ><strong>{{ number(overview?.total_tokens) }}</strong>
      </article>
      <article>
        <span>{{ t('usageAnalytics.metrics.mcpCalls') }}</span
        ><strong>{{ number(overview?.mcp_calls) }}</strong>
      </article>
      <article>
        <span>{{ t('usageAnalytics.governance.metrics.inactiveKnowledgeBases') }} (90d)</span
        ><strong>{{ number(overview?.inactive_knowledge_bases) }}</strong>
      </article>
      <article>
        <span>{{ t('usageAnalytics.columns.successRate') }}</span
        ><strong>{{ percent(overview?.mcp_success_rate) }}</strong>
      </article>
    </div>

    <div class="summary-grid">
      <section class="usage-card">
        <h3>{{ t('usageAnalytics.governance.mcpAdoption') }}</h3>
        <dl>
          <div>
            <dt>
              {{ t('usageAnalytics.governance.metrics.mcpActiveSpaces') }}
            </dt>
            <dd>{{ number(overview?.mcp_active_tenants) }}</dd>
          </div>
          <div>
            <dt>
              {{ t('usageAnalytics.governance.metrics.platformPenetration') }}
            </dt>
            <dd>{{ percent(overview?.mcp_platform_penetration) }}</dd>
          </div>
          <div>
            <dt>
              {{ t('usageAnalytics.governance.metrics.activeSpaceAdoption') }}
            </dt>
            <dd>{{ percent(overview?.mcp_adoption_among_active_spaces) }}</dd>
          </div>
          <div>
            <dt>{{ t('usageAnalytics.columns.avgLatency') }}</dt>
            <dd>{{ number(overview?.mcp_average_latency_ms) }} ms</dd>
          </div>
        </dl>
      </section>
      <section class="usage-card">
        <h3>{{ t('usageAnalytics.governance.attentionNeeded') }}</h3>
        <div v-if="!overview?.attention_needed?.length" class="empty">
          {{ t('usageAnalytics.governance.noAttention') }}
        </div>
        <ul v-else>
          <li v-for="item in overview.attention_needed" :key="`${item.code}:${item.entity_id || ''}`">
            {{ attentionLabel(item) }}
          </li>
        </ul>
      </section>
    </div>

    <div class="summary-grid">
      <section class="usage-card">
        <h3>{{ t('usageAnalytics.trend') }} (7/30d)</h3>
        <div v-if="!timeseries.length" class="empty">
          {{ t('usageAnalytics.empty') }}
        </div>
        <div v-else class="trend-list">
          <div v-for="point in timeseries" :key="point.bucket">
            <time>{{ date(point.bucket) }}</time
            ><span>{{ number(point.total_tokens) }}</span>
          </div>
        </div>
      </section>
      <section class="usage-card">
        <h3>{{ t('usageAnalytics.governance.topKnowledgeBases') }}</h3>
        <div v-if="!overview?.top_knowledge_bases?.length" class="empty">
          {{ t('usageAnalytics.empty') }}
        </div>
        <ol v-else>
          <li v-for="kb in overview.top_knowledge_bases" :key="kb.knowledge_base_id">
            <span>{{ kb.knowledge_base_name }}</span
            ><strong>{{
              t('usageAnalytics.governance.usedBySpaces', {
                count: kb.unique_tenants,
              })
            }}</strong>
          </li>
        </ol>
      </section>
    </div>

    <section class="usage-card">
      <h3>{{ t('usageAnalytics.operationDistribution') }}</h3>
      <div v-for="item in operations" :key="item.operation" class="operation">
        <span>{{ operationLabel(item.operation) }}</span
        ><strong>{{ percent(item.percentage) }}</strong>
      </div>
      <div v-if="!operations.length" class="empty">
        {{ t('usageAnalytics.empty') }}
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { OperationUsageRow, UsageAttention, UsageOverview, UsageTimeSeriesPoint } from '@/api/usage-analytics'

defineProps<{
  overview?: UsageOverview
  timeseries: UsageTimeSeriesPoint[]
  operations: OperationUsageRow[]
}>()
const { t } = useI18n()
const number = (value?: number) => new Intl.NumberFormat().format(value ?? 0)
const percent = (value?: number) => `${(value ?? 0).toFixed(1)}%`
const date = (value: string) => new Date(value).toLocaleDateString()
function operationLabel(value: string) {
  const key = `usageAnalytics.operations.${value}`
  const label = t(key)
  return label === key ? value.replaceAll('_', ' ') : label
}
function attentionLabel(item: UsageAttention) {
  return t(`usageAnalytics.governance.attention.${item.code}`, {
    value: number(item.value),
    name: item.entity_name || '',
  })
}
</script>

<style scoped lang="less">
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 14px;
}
.kpi-grid--primary article {
  border-top: 2px solid var(--td-brand-color);
}
.kpi-grid--secondary article {
  padding: 14px 18px;
}
.kpi-grid--secondary article strong {
  font-size: 20px;
}
article,
.usage-card {
  padding: 18px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-container);
}
article {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
article span,
.empty,
dt {
  color: var(--td-text-color-secondary);
}
article strong {
  font-size: 25px;
}
.summary-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
  margin-bottom: 14px;
}
.usage-card h3 {
  margin: 0 0 14px;
}
.usage-card dl {
  margin: 0;
}
.usage-card dl div,
.operation,
ol li {
  display: flex;
  justify-content: space-between;
  padding: 9px 0;
  border-bottom: 1px solid var(--td-component-stroke);
}
dd {
  margin: 0;
  font-weight: 600;
}
.trend-list > div {
  display: flex;
  justify-content: space-between;
  padding: 7px 0;
}
ul,
ol {
  margin: 0;
  padding-left: 20px;
}
@media (max-width: 900px) {
  .kpi-grid {
    grid-template-columns: 1fr 1fr;
  }
  .summary-grid {
    grid-template-columns: 1fr;
  }
}
</style>
