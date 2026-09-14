<template>
  <section class="portal-hero" data-guide="portal-home">
    <div class="hero-overview">
      <div class="hero-copy">
        <span class="eyebrow">KnowHub · {{ t('portalExperience.eyebrow') }}</span>
        <h1>{{ t('portalExperience.heroTitle') }}</h1>
        <p>{{ t('portalExperience.heroDescription') }}</p>
      </div>

      <div class="hero-stats" :aria-label="t('portalExperience.scaleLabel')">
        <template v-if="loading">
          <t-skeleton v-for="item in 3" :key="item" animation="gradient" :row-col="[{ width: '64px', height: '26px' }, { width: '46px', height: '12px' }]" />
        </template>
        <PortalSectionState v-else-if="error" compact @retry="$emit('retry')" />
        <template v-else>
          <div v-for="item in statItems" :key="item.label">
            <strong>{{ numberFormatter.format(item.value) }}</strong>
            <span>{{ item.label }}</span>
          </div>
          <t-tooltip :content="t('portalExperience.scaleTooltip')" placement="bottom">
            <t-icon class="stats-info" name="info-circle" />
          </t-tooltip>
        </template>
      </div>
    </div>

    <button type="button" class="hero-search"
      :aria-label="`${t('portalExperience.searchPlaceholder')}. ${hasActiveSpace ? t('portalExperience.currentScope', { space: activeSpaceName }) : t('portalExperience.noActiveScope')}`"
      @click="$emit('search')">
      <t-icon name="search" />
      <span>{{ t('portalExperience.searchPlaceholder') }}</span>
      <em>{{ hasActiveSpace ? t('portalExperience.currentScope', { space: activeSpaceName }) : t('portalExperience.noActiveScope') }}</em>
      <kbd>Ctrl K</kbd>
    </button>

    <div class="quick-actions" :aria-label="t('portalExperience.quickActionsLabel')">
      <t-tooltip :content="t('portalExperience.quickActions.search')"><button type="button" @click="$emit('search')"><t-icon name="search" />{{ t('portalExperience.quickActions.search') }}</button></t-tooltip>
      <t-tooltip :content="t('portalExperience.quickActions.browse')"><button type="button" @click="$emit('browse')"><t-icon name="folder" />{{ t('portalExperience.quickActions.browse') }}</button></t-tooltip>
      <t-tooltip v-if="supportsAgent" :content="t('portalExperience.quickActions.agent')"><button type="button" @click="$emit('agent')"><t-icon name="robot" />{{ t('portalExperience.quickActions.agent') }}</button></t-tooltip>
      <t-tooltip v-if="supportsTools" :content="t('portalExperience.quickActions.tools')"><button type="button" @click="$emit('tools')"><t-icon name="tools" />{{ t('portalExperience.quickActions.tools') }}</button></t-tooltip>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { KnowledgeScale } from '@/config/portalKnowledgeSummary'
import PortalSectionState from './PortalSectionState.vue'

const props = defineProps<{
  stats: KnowledgeScale
  loading: boolean
  error: boolean
  activeSpaceName: string
  hasActiveSpace: boolean
  supportsAgent: boolean
  supportsTools: boolean
}>()
defineEmits<{ search: []; browse: []; agent: []; tools: []; retry: [] }>()
const { t } = useI18n()
const numberFormatter = new Intl.NumberFormat()
const statItems = computed(() => [
  { value: props.stats.spaces, label: t('portalExperience.spaces') },
  { value: props.stats.knowledgeBases, label: t('portalExperience.knowledgeBases') },
  { value: props.stats.files, label: t('portalExperience.files') },
])
</script>

<style scoped lang="less">
.portal-hero{padding:20px 0 18px;border-bottom:1px solid var(--td-component-stroke)}
.hero-overview{display:flex;align-items:flex-end;justify-content:space-between;gap:36px}.hero-copy{max-width:680px}.eyebrow{color:var(--td-brand-color);font-size:11px;font-weight:600;letter-spacing:.08em}.hero-copy h1{margin:5px 0 6px;font-size:clamp(22px,2vw,27px);font-weight:600;letter-spacing:-.015em}.hero-copy p{max-width:640px;margin:0;color:var(--td-text-color-secondary);font-size:13px;line-height:1.55;white-space:pre-line}
.hero-stats{min-height:48px;display:flex;align-items:center;gap:0;flex:none}.hero-stats>div{min-width:92px;padding-right:20px}.hero-stats>div+div{padding-left:20px;border-left:1px solid var(--td-component-stroke)}.hero-stats>.portal-section-state{min-width:250px;padding-right:12px}.hero-stats strong,.hero-stats span{display:block}.hero-stats strong{font-size:23px;font-weight:600;line-height:1.1}.hero-stats span{margin-top:3px;color:var(--td-text-color-placeholder);font-size:11px}.stats-info{margin-left:10px;color:var(--td-text-color-placeholder);cursor:help}
.hero-search{width:min(820px,100%);height:42px;display:flex;align-items:center;gap:9px;margin-top:15px;padding:0 12px;border:1px solid var(--td-component-border);border-radius:9px;background:var(--td-bg-color-container);color:var(--td-text-color-secondary);cursor:text;font:inherit;text-align:left}.hero-search:hover{border-color:var(--td-brand-color)}.hero-search:focus-visible{border-color:var(--td-brand-color);outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.hero-search>.t-icon{color:var(--td-brand-color);font-size:18px}.hero-search>span{min-width:0;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.hero-search em{padding:3px 8px;border-radius:999px;background:var(--td-bg-color-secondarycontainer);color:var(--td-text-color-placeholder);font-size:11px;font-style:normal;white-space:nowrap}.hero-search kbd{color:var(--td-text-color-placeholder);font:10px var(--td-font-family)}
.quick-actions{display:flex;flex-wrap:wrap;gap:3px 18px;margin-top:8px}.quick-actions button{display:flex;align-items:center;gap:6px;padding:4px 0;border:0;background:transparent;color:var(--td-text-color-secondary);cursor:pointer;font:12px var(--td-font-family)}.quick-actions button:hover{color:var(--td-brand-color)}.quick-actions button:focus-visible{border-radius:4px;outline:2px solid var(--td-brand-color-focus);outline-offset:2px}.quick-actions .t-icon{font-size:15px}
@media(max-width:1080px){.hero-overview{align-items:flex-start;flex-direction:column;gap:14px}}
@media(max-width:680px){.portal-hero{padding:17px 0}.hero-stats{width:100%}.hero-stats>div{min-width:0;flex:1;padding-right:12px}.hero-stats>div+div{padding-left:12px}.hero-stats strong{font-size:21px}.hero-stats>.portal-section-state{min-width:0}.hero-search em,.hero-search kbd{display:none}}
</style>
