<template>
  <div class="usage-analytics">
    <header class="usage-header">
      <div>
        <h2>{{ t('usageAnalytics.title') }}</h2>
        <p>{{ t('usageAnalytics.governance.description') }}</p>
      </div>
      <div class="usage-range">
        <select
          v-if="activeTab === 'spaces' || activeTab === 'knowledgeBases'"
          v-model="status"
          :aria-label="t('usageAnalytics.governance.usageStatus')"
          @change="reload"
        >
          <option value="">
            {{ t('usageAnalytics.governance.filters.allStatuses') }}
          </option>
          <option v-for="item in statuses" :key="item" :value="item">
            {{ t(`usageAnalytics.governance.status.${item}`) }}
          </option>
        </select>
        <select
          v-if="activeTab === 'spaces'"
          v-model="mcpAdoption"
          :aria-label="t('usageAnalytics.governance.mcpAdoption')"
          @change="reload"
        >
          <option value="">
            {{ t('usageAnalytics.governance.filters.allMCP') }}
          </option>
          <option value="adopted">
            {{ t('usageAnalytics.governance.filters.adopted') }}
          </option>
          <option value="not_adopted">
            {{ t('usageAnalytics.governance.filters.notAdopted') }}
          </option>
        </select>
        <select
          v-if="activeTab === 'spaces' || activeTab === 'knowledgeBases'"
          v-model="crossTenant"
          :aria-label="t('usageAnalytics.governance.crossSpaceUsage')"
          @change="reload"
        >
          <option value="">
            {{ t('usageAnalytics.governance.filters.allReuse') }}
          </option>
          <option value="with">
            {{ t('usageAnalytics.governance.filters.withReuse') }}
          </option>
          <option value="without">
            {{ t('usageAnalytics.governance.filters.withoutReuse') }}
          </option>
        </select>
        <select
          v-if="activeTab === 'token'"
          v-model="usageClass"
          :aria-label="t('usageAnalytics.tokenType')"
          @change="reload"
        >
          <option value="">{{ t('usageAnalytics.filters.all') }}</option>
          <option value="foreground">
            {{ t('usageAnalytics.filters.foreground') }}
          </option>
          <option value="background">
            {{ t('usageAnalytics.filters.background') }}
          </option>
        </select>
        <select
          v-if="activeTab === 'mcp'"
          v-model="direction"
          :aria-label="t('usageAnalytics.direction')"
          @change="reload"
        >
          <option value="">{{ t('usageAnalytics.filters.all') }}</option>
          <option value="inbound">
            {{ t('usageAnalytics.filters.inbound') }}
          </option>
          <option value="outbound">
            {{ t('usageAnalytics.filters.outbound') }}
          </option>
        </select>
        <select
          v-if="activeTab === 'mcp'"
          v-model="mcpGroup"
          :aria-label="t('usageAnalytics.governance.groupBy')"
          @change="reload"
        >
          <option value="tool">
            {{ t('usageAnalytics.governance.groups.tool') }}
          </option>
          <option value="client">
            {{ t('usageAnalytics.governance.groups.client') }}
          </option>
          <option value="knowledge_base">
            {{ t('usageAnalytics.governance.groups.knowledgeBase') }}
          </option>
        </select>
        <select
          v-if="activeTab === 'spaces' || activeTab === 'knowledgeBases'"
          v-model="sort"
          :aria-label="t('usageAnalytics.sort')"
          @change="reload"
        >
          <option v-for="item in sortOptions" :key="item" :value="item">
            {{ t(`usageAnalytics.governance.sorts.${item}`) }}
          </option>
        </select>
        <select v-model="rangeDays" :aria-label="t('usageAnalytics.range')" @change="reload">
          <option :value="7">7 {{ t('usageAnalytics.days') }}</option>
          <option :value="30">30 {{ t('usageAnalytics.days') }}</option>
          <option :value="90">90 {{ t('usageAnalytics.days') }}</option>
        </select>
        <button type="button" :disabled="loading" @click="reload">
          {{ t('usageAnalytics.refresh') }}
        </button>
      </div>
    </header>
    <div v-if="collectingSince" class="usage-notice">
      {{
        t('usageAnalytics.collectingSince', {
          date: formatDate(collectingSince),
        })
      }}
      · {{ t('usageAnalytics.governance.coverage') }}
    </div>
    <div v-if="error" class="usage-error">{{ error }}</div>
    <nav class="usage-tabs" :aria-label="t('usageAnalytics.title')">
      <button
        v-for="tab in tabs"
        :key="tab"
        type="button"
        :class="{ active: activeTab === tab }"
        @click="selectTab(tab)"
      >
        {{ t(`usageAnalytics.tabs.${tab}`) }}
      </button>
    </nav>
    <div v-if="loading" class="usage-state">
      {{ t('usageAnalytics.loading') }}
    </div>
    <template v-else>
      <UsageOverview
        v-if="activeTab === 'overview'"
        :overview="overview"
        :timeseries="timeseries"
        :operations="operations"
      />
      <section v-else class="usage-card table-card">
        <div v-if="activeTab === 'token'" class="scope-note">
          {{ t('usageAnalytics.tokenScopePhase2') }}
        </div>
        <div v-if="activeTab === 'mcp'" class="scope-note">
          {{ t('usageAnalytics.mcpScope') }}
        </div>
        <UsageSpaces v-if="activeTab === 'spaces'" :rows="spaces" /><UsageKnowledgeBases
          v-else-if="activeTab === 'knowledgeBases'"
          :rows="knowledgeBases"
          :query="query"
        /><UsageToken v-else-if="activeTab === 'token'" :rows="models" /><UsageMCP
          v-else
          :rows="mcp"
          :group-by="mcpGroup"
        />
        <footer v-if="total > pageSize" class="pager">
          <button type="button" :disabled="page <= 1" @click="changePage(page - 1)">‹</button
          ><span>{{ page }} / {{ Math.ceil(total / pageSize) }}</span
          ><button type="button" :disabled="page * pageSize >= total" @click="changePage(page + 1)">›</button>
        </footer>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getKnowledgeBaseUsage,
  getMCPUsage,
  getUsageModels,
  getUsageOperations,
  getUsageOverview,
  getUsageTenants,
  getUsageTimeSeries,
  type KnowledgeBaseUsageRow,
  type MCPUsageRow,
  type ModelUsageRow,
  type OperationUsageRow,
  type TenantUsageRow,
  type UsageGovernanceStatus,
  type UsageQuery,
  type UsageTimeSeriesPoint,
} from '@/api/usage-analytics'
import type { UsageOverview as UsageOverviewData } from '@/api/usage-analytics'
import UsageKnowledgeBases from './usage/UsageKnowledgeBases.vue'
import UsageMCP from './usage/UsageMCP.vue'
import UsageOverview from './usage/UsageOverview.vue'
import UsageSpaces from './usage/UsageSpaces.vue'
import UsageToken from './usage/UsageToken.vue'
type Tab = 'overview' | 'spaces' | 'knowledgeBases' | 'token' | 'mcp'
const { t } = useI18n()
const tabs: Tab[] = ['overview', 'spaces', 'knowledgeBases', 'token', 'mcp']
const statuses: UsageGovernanceStatus[] = ['active', 'low_activity', 'inactive', 'never_used', 'insufficient_data']
const activeTab = ref<Tab>('overview')
const rangeDays = ref(30)
const sort = ref('last_active')
const status = ref<'' | UsageGovernanceStatus>('')
const mcpAdoption = ref<'' | 'adopted' | 'not_adopted'>('')
const crossTenant = ref<'' | 'with' | 'without'>('')
const usageClass = ref<'' | 'foreground' | 'background'>('')
const direction = ref<'' | 'inbound' | 'outbound'>('')
const mcpGroup = ref<'tool' | 'client' | 'knowledge_base'>('tool')
const loading = ref(false)
const error = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const collectingSince = ref<string>()
const overview = ref<UsageOverviewData>()
const timeseries = ref<UsageTimeSeriesPoint[]>([])
const operations = ref<OperationUsageRow[]>([])
const spaces = ref<TenantUsageRow[]>([])
const knowledgeBases = ref<KnowledgeBaseUsageRow[]>([])
const models = ref<ModelUsageRow[]>([])
const mcp = ref<MCPUsageRow[]>([])
const sortOptions = computed(() =>
  activeTab.value === 'knowledgeBases'
    ? ['unique_tenants', 'accesses', 'model_accesses', 'mcp_accesses', 'cross_tenant', 'recent_growth', 'last_active']
    : ['last_active', 'tokens', 'mcp_calls', 'active_principals', 'cross_tenant'],
)
watch(activeTab, () => {
  sort.value = activeTab.value === 'knowledgeBases' ? 'unique_tenants' : 'last_active'
})
const query = computed<UsageQuery>(() => ({
  from: new Date(Date.now() - rangeDays.value * 86400000).toISOString(),
  to: new Date().toISOString(),
  interval: 'day',
  page: page.value,
  page_size: pageSize,
  sort: activeTab.value === 'spaces' || activeTab.value === 'knowledgeBases' ? sort.value : undefined,
  status:
    (activeTab.value === 'spaces' || activeTab.value === 'knowledgeBases') && status.value ? status.value : undefined,
  include_inactive: activeTab.value === 'spaces' ? true : undefined,
  mcp_adoption: activeTab.value === 'spaces' && mcpAdoption.value ? mcpAdoption.value : undefined,
  cross_tenant:
    (activeTab.value === 'spaces' || activeTab.value === 'knowledgeBases') && crossTenant.value
      ? crossTenant.value
      : undefined,
  group_by:
    activeTab.value === 'knowledgeBases' ? 'knowledge_base' : activeTab.value === 'mcp' ? mcpGroup.value : undefined,
  usage_class: activeTab.value === 'token' && usageClass.value ? usageClass.value : undefined,
  direction: activeTab.value === 'mcp' && direction.value ? direction.value : undefined,
}))
async function load() {
  loading.value = true
  error.value = ''
  try {
    if (activeTab.value === 'overview') {
      const [summary, trend, distribution] = await Promise.all([
        getUsageOverview(query.value),
        getUsageTimeSeries(query.value),
        getUsageOperations(query.value),
      ])
      overview.value = summary
      timeseries.value = trend.data || []
      operations.value = distribution.data || []
      collectingSince.value = summary.collecting_since || trend.collecting_since
      return
    }
    const loaders = {
      spaces: getUsageTenants,
      knowledgeBases: getKnowledgeBaseUsage,
      token: getUsageModels,
      mcp: getMCPUsage,
    }
    const result = await loaders[activeTab.value](query.value)
    total.value = result.total || 0
    collectingSince.value = result.collecting_since
    if (activeTab.value === 'spaces') spaces.value = result.data as TenantUsageRow[]
    else if (activeTab.value === 'knowledgeBases') knowledgeBases.value = result.data as KnowledgeBaseUsageRow[]
    else if (activeTab.value === 'token') models.value = result.data as ModelUsageRow[]
    else mcp.value = result.data as MCPUsageRow[]
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : t('usageAnalytics.loadFailed')
  } finally {
    loading.value = false
  }
}
function selectTab(tab: Tab) {
  activeTab.value = tab
  page.value = 1
  void load()
}
function changePage(value: number) {
  page.value = value
  void load()
}
function reload() {
  page.value = 1
  void load()
}
function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : '—'
}
onMounted(load)
</script>

<style scoped lang="less">
.usage-analytics {
  padding: 24px;
  color: var(--td-text-color-primary);
}
.usage-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
}
.usage-header h2 {
  margin: 0 0 6px;
  font-size: 22px;
}
.usage-header p {
  margin: 0;
  color: var(--td-text-color-secondary);
}
.usage-range {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}
.usage-range select,
.usage-range button {
  height: 34px;
  border: 1px solid var(--td-component-border);
  border-radius: 6px;
  background: var(--td-bg-color-container);
  padding: 0 10px;
  color: inherit;
}
.usage-notice,
.scope-note {
  margin: 16px 0;
  padding: 10px 12px;
  border-radius: 6px;
  background: var(--td-brand-color-light);
  color: var(--td-brand-color);
}
.usage-error {
  margin: 16px 0;
  padding: 10px 12px;
  border-radius: 6px;
  background: var(--td-error-color-light);
  color: var(--td-error-color);
}
.usage-tabs {
  display: flex;
  gap: 20px;
  border-bottom: 1px solid var(--td-component-stroke);
  margin: 22px 0 16px;
}
.usage-tabs button {
  border: 0;
  background: none;
  padding: 10px 2px;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  border-bottom: 2px solid transparent;
}
.usage-tabs button.active {
  color: var(--td-brand-color);
  border-bottom-color: var(--td-brand-color);
}
.usage-card {
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-container);
}
.table-card {
  overflow: hidden;
}
.scope-note {
  margin: 16px;
}
.usage-state {
  padding: 48px;
  text-align: center;
  color: var(--td-text-color-placeholder);
}
.pager {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
}
.pager button {
  width: 32px;
  height: 30px;
}
@media (max-width: 900px) {
  .usage-header {
    flex-direction: column;
  }
  .usage-range {
    justify-content: flex-start;
  }
}
</style>
