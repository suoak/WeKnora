<template>
  <div class="usage-analytics">
    <header class="usage-header">
      <div>
        <h2>{{ t('usageAnalytics.title') }}</h2>
        <p>{{ t('usageAnalytics.description') }}</p>
      </div>
      <div class="usage-range">
        <select v-if="activeTab === 'spaces'" v-model="sort" :aria-label="t('usageAnalytics.sort')" @change="reload">
          <option value="tokens">{{ t('usageAnalytics.sorts.tokens') }}</option>
          <option value="mcp_calls">{{ t('usageAnalytics.sorts.mcpCalls') }}</option>
          <option value="active_principals">{{ t('usageAnalytics.sorts.activePrincipals') }}</option>
          <option value="last_active">{{ t('usageAnalytics.sorts.lastActive') }}</option>
        </select>
        <select v-model="rangeDays" :aria-label="t('usageAnalytics.range')" @change="reload">
          <option :value="7">7 {{ t('usageAnalytics.days') }}</option>
          <option :value="30">30 {{ t('usageAnalytics.days') }}</option>
          <option :value="90">90 {{ t('usageAnalytics.days') }}</option>
        </select>
        <button type="button" :disabled="loading" @click="reload">{{ t('usageAnalytics.refresh') }}</button>
      </div>
    </header>

    <div v-if="collectingSince" class="usage-notice">
      {{ t('usageAnalytics.collectingSince', { date: formatDate(collectingSince) }) }}
    </div>
    <div v-if="error" class="usage-error">{{ error }}</div>

    <nav class="usage-tabs" :aria-label="t('usageAnalytics.title')">
      <button v-for="tab in tabs" :key="tab" type="button" :class="{ active: activeTab === tab }" @click="selectTab(tab)">
        {{ t(`usageAnalytics.tabs.${tab}`) }}
      </button>
    </nav>

    <div v-if="loading" class="usage-state">{{ t('usageAnalytics.loading') }}</div>

    <template v-else-if="activeTab === 'overview'">
      <div class="kpi-grid">
        <article><span>{{ t('usageAnalytics.metrics.tokens') }}</span><strong>{{ number(overview?.total_tokens) }}</strong></article>
        <article><span>{{ t('usageAnalytics.metrics.mcpCalls') }}</span><strong>{{ number(overview?.mcp_calls) }}</strong></article>
        <article><span>{{ t('usageAnalytics.metrics.activeTenants') }}</span><strong>{{ number(overview?.active_tenants) }}</strong></article>
        <article><span>{{ t('usageAnalytics.metrics.activePrincipals') }}</span><strong>{{ number(overview?.active_principals) }}</strong></article>
      </div>
      <div class="summary-grid">
        <section class="usage-card">
          <h3>{{ t('usageAnalytics.tokenBreakdown') }}</h3>
          <dl>
            <div><dt>{{ t('usageAnalytics.columns.input') }}</dt><dd>{{ number(overview?.input_tokens) }}</dd></div>
            <div><dt>{{ t('usageAnalytics.columns.output') }}</dt><dd>{{ number(overview?.output_tokens) }}</dd></div>
            <div><dt>{{ t('usageAnalytics.columns.cacheRead') }}</dt><dd>{{ number(overview?.cache_read_tokens) }}</dd></div>
            <div><dt>{{ t('usageAnalytics.columns.cacheWrite') }}</dt><dd>{{ number(overview?.cache_write_tokens) }}</dd></div>
          </dl>
        </section>
        <section class="usage-card">
          <h3>MCP</h3>
          <dl>
            <div><dt>{{ t('usageAnalytics.metrics.totalCalls') }}</dt><dd>{{ number(overview?.mcp_calls) }}</dd></div>
            <div><dt>{{ t('usageAnalytics.columns.successRate') }}</dt><dd>{{ percent(overview?.mcp_success_rate) }}</dd></div>
            <div><dt>{{ t('usageAnalytics.columns.avgLatency') }}</dt><dd>{{ number(overview?.mcp_average_latency_ms) }} ms</dd></div>
            <div><dt>{{ t('usageAnalytics.metrics.unattributed') }}</dt><dd>{{ number(overview?.unattributed_mcp_calls) }}</dd></div>
          </dl>
        </section>
      </div>
      <section class="usage-card">
        <h3>{{ t('usageAnalytics.trend') }}</h3>
        <div v-if="!timeseries.length" class="usage-state">{{ t('usageAnalytics.empty') }}</div>
        <div v-else class="trend-list">
          <div v-for="point in timeseries" :key="point.bucket">
            <time>{{ formatDate(point.bucket) }}</time>
            <div class="trend-bar"><i :style="{ width: `${trendWidth(point.total_tokens)}%` }" /></div>
            <strong>{{ number(point.total_tokens) }}</strong>
          </div>
        </div>
      </section>
    </template>

    <section v-else class="usage-card table-card">
      <div v-if="activeTab === 'token'" class="scope-note">{{ t('usageAnalytics.tokenScope') }}</div>
      <div v-if="activeTab === 'mcp'" class="scope-note">{{ t('usageAnalytics.mcpScope') }}</div>
      <div class="table-scroll">
        <table v-if="rows.length">
          <thead><tr><th v-for="column in columns" :key="column.key">{{ column.label }}</th></tr></thead>
          <tbody>
            <tr v-for="(row, index) in rows" :key="rowKey(row, index)">
              <td v-for="column in columns" :key="column.key">{{ cell(row, column.key) }}</td>
            </tr>
          </tbody>
        </table>
        <div v-else class="usage-state">{{ t('usageAnalytics.empty') }}</div>
      </div>
      <footer v-if="total > pageSize" class="pager">
        <button type="button" :disabled="page <= 1" @click="changePage(page - 1)">‹</button>
        <span>{{ page }} / {{ Math.ceil(total / pageSize) }}</span>
        <button type="button" :disabled="page * pageSize >= total" @click="changePage(page + 1)">›</button>
      </footer>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getKnowledgeBaseUsage, getMCPUsage, getUsageModels, getUsageOverview,
  getUsageTenants, getUsageTimeSeries,
  type UsageOverview, type UsageTimeSeriesPoint,
} from '@/api/usage-analytics'

type Tab = 'overview' | 'spaces' | 'token' | 'mcp' | 'knowledgeBases'
type Row = Record<string, unknown>
const { t } = useI18n()
const tabs: Tab[] = ['overview', 'spaces', 'token', 'mcp', 'knowledgeBases']
const activeTab = ref<Tab>('overview')
const rangeDays = ref(30)
const sort = ref('tokens')
const loading = ref(false)
const error = ref('')
const overview = ref<UsageOverview>()
const timeseries = ref<UsageTimeSeriesPoint[]>([])
const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const collectingSince = ref<string>()

const query = computed(() => ({
  from: new Date(Date.now() - rangeDays.value * 86400000).toISOString(),
  to: new Date().toISOString(),
  interval: 'day' as const,
  page: page.value,
  page_size: pageSize,
  sort: activeTab.value === 'spaces' ? sort.value : undefined,
}))

const columnKeys: Record<Exclude<Tab, 'overview'>, string[]> = {
  spaces: ['tenant_name', 'active_principals', 'assistant_turns', 'agent_turns', 'input_tokens', 'output_tokens', 'total_tokens', 'mcp_calls', 'mcp_success_rate', 'mcp_avg_latency_ms', 'last_active'],
  token: ['model_id', 'model_type', 'assistant_turns', 'input_tokens', 'output_tokens', 'total_tokens', 'cache_read_tokens', 'cache_write_tokens', 'last_active'],
  mcp: ['tool_name', 'calls', 'successful_calls', 'success_rate', 'average_latency_ms', 'unattributed_calls', 'last_active'],
  knowledgeBases: ['knowledge_base_name', 'owner_tenant_name', 'caller_tenant_name', 'assistant_turns', 'mcp_calls', 'last_active'],
}
const columns = computed(() => activeTab.value === 'overview' ? [] : columnKeys[activeTab.value].map(key => ({ key, label: t(`usageAnalytics.columns.${key}`) })))

function number(value?: number) { return new Intl.NumberFormat().format(value ?? 0) }
function percent(value?: number) { return `${(value ?? 0).toFixed(1)}%` }
function formatDate(value?: string) { return value ? new Date(value).toLocaleString() : '—' }
function trendWidth(value: number) { const max = Math.max(...timeseries.value.map(item => item.total_tokens), 1); return Math.max(2, value / max * 100) }
function rowKey(row: Row, index: number) {
  return String(row.tenant_id ?? row.model_id ?? row.tool_name ?? (row.knowledge_base_id ? `${row.knowledge_base_id}:${row.caller_tenant_id ?? 'unknown'}` : index))
}
function cell(row: Row, key: string) {
  const value = row[key]
  if (key === 'last_active') return formatDate(value as string | undefined)
  if (key.includes('success_rate')) return percent(value as number)
  if (typeof value === 'number') return number(value)
  if ((key === 'caller_tenant_name' || key === 'caller_tenant_id') && !value) return t('usageAnalytics.unattributed')
  return String(value || '—')
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    if (activeTab.value === 'overview') {
      const [summary, trend] = await Promise.all([getUsageOverview(query.value), getUsageTimeSeries(query.value)])
      overview.value = summary
      timeseries.value = trend.data || []
      collectingSince.value = summary.collecting_since || trend.collecting_since
      return
    }
    const loaders = { spaces: getUsageTenants, token: getUsageModels, mcp: getMCPUsage, knowledgeBases: getKnowledgeBaseUsage }
    const result = await loaders[activeTab.value](query.value)
    rows.value = (result.data || []) as unknown as Row[]
    total.value = result.total || 0
    collectingSince.value = result.collecting_since
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : t('usageAnalytics.loadFailed')
  } finally {
    loading.value = false
  }
}
function selectTab(tab: Tab) { activeTab.value = tab; page.value = 1; void load() }
function changePage(value: number) { page.value = value; void load() }
function reload() { page.value = 1; void load() }
onMounted(load)
</script>

<style scoped lang="less">
.usage-analytics { padding: 24px; color: var(--td-text-color-primary); }
.usage-header { display:flex; align-items:flex-start; justify-content:space-between; gap:20px; h2{margin:0 0 6px;font-size:22px} p{margin:0;color:var(--td-text-color-secondary)} }
.usage-range { display:flex;gap:8px; select,button{height:34px;border:1px solid var(--td-component-border);border-radius:6px;background:var(--td-bg-color-container);padding:0 12px;color:inherit} }
.usage-notice,.scope-note { margin:16px 0;padding:10px 12px;border-radius:6px;background:var(--td-brand-color-light);color:var(--td-brand-color); }
.usage-error { margin:16px 0;padding:10px 12px;border-radius:6px;background:var(--td-error-color-light);color:var(--td-error-color); }
.usage-tabs { display:flex;gap:20px;border-bottom:1px solid var(--td-component-stroke);margin:22px 0 16px;button{border:0;background:none;padding:10px 2px;color:var(--td-text-color-secondary);cursor:pointer;border-bottom:2px solid transparent}.active{color:var(--td-brand-color);border-bottom-color:var(--td-brand-color)} }
.kpi-grid { display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px;margin-bottom:14px;article{padding:18px;border:1px solid var(--td-component-stroke);border-radius:8px;background:var(--td-bg-color-container);display:flex;flex-direction:column;gap:10px}span{color:var(--td-text-color-secondary)}strong{font-size:25px} }
.summary-grid { display:grid;grid-template-columns:1fr 1fr;gap:14px;margin-bottom:14px; }
.usage-card { border:1px solid var(--td-component-stroke);border-radius:8px;background:var(--td-bg-color-container);padding:18px;h3{margin:0 0 14px}dl{margin:0}dl div{display:flex;justify-content:space-between;padding:9px 0;border-bottom:1px solid var(--td-component-stroke)}dt{color:var(--td-text-color-secondary)}dd{margin:0;font-weight:600} }
.table-card { padding:0;overflow:hidden;.scope-note{margin:16px}.table-scroll{overflow:auto}table{border-collapse:collapse;width:100%;font-size:13px}th,td{text-align:left;white-space:nowrap;padding:12px 14px;border-bottom:1px solid var(--td-component-stroke)}th{background:var(--td-bg-color-secondarycontainer);font-weight:600} }
.trend-list>div { display:grid;grid-template-columns:170px 1fr 100px;gap:12px;align-items:center;margin:8px 0}.trend-bar{height:8px;background:var(--td-bg-color-component);border-radius:5px;overflow:hidden}.trend-bar i{display:block;height:100%;background:var(--td-brand-color);border-radius:5px}.trend-list strong{text-align:right}
.usage-state { padding:48px;text-align:center;color:var(--td-text-color-placeholder) }.pager{display:flex;justify-content:flex-end;align-items:center;gap:10px;padding:12px 16px;button{width:32px;height:30px}}
@media(max-width:900px){.kpi-grid{grid-template-columns:1fr 1fr}.summary-grid{grid-template-columns:1fr}.usage-header{flex-direction:column}.trend-list>div{grid-template-columns:120px 1fr 80px}}
</style>
