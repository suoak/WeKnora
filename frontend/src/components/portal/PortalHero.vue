<template>
  <section class="portal-hero" data-guide="portal-home">
    <div class="hero-surface">
      <div class="hero-copy">
        <span class="eyebrow">KnowHub <i aria-hidden="true"></i> {{ t('portalExperience.eyebrow') }}</span>
        <div class="hero-title-row">
          <div>
            <h1>{{ t('portalExperience.heroTitle') }}</h1>
            <p>{{ t('portalExperience.heroDescription') }}</p>
          </div>
          <div class="hero-stats" :aria-label="t('portalExperience.scaleLabel')">
            <template v-if="loading">
              <t-skeleton v-for="item in 3" :key="item" animation="gradient" :row-col="[{ width: '54px', height: '24px' }, { width: '42px', height: '11px' }]" />
            </template>
            <PortalSectionState v-else-if="error" compact @retry="$emit('retry')" />
            <template v-else>
              <div v-for="item in statItems" :key="item.label"><strong>{{ numberFormatter.format(item.value) }}</strong><span>{{ item.label }}</span></div>
              <t-tooltip :content="t('portalExperience.scaleTooltip')" placement="bottom"><t-icon class="stats-info" name="info-circle" /></t-tooltip>
            </template>
          </div>
        </div>
      </div>

      <button type="button" class="hero-search"
        :aria-label="`${t('portalExperience.searchPlaceholder')}. ${hasActiveSpace ? t('portalExperience.currentScope', { space: activeSpaceName }) : t('portalExperience.noActiveScope')}`"
        @click="$emit('search')">
        <span class="search-icon"><t-icon name="search" /></span>
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
.portal-hero{padding:14px 0 0}.hero-surface{padding:16px 18px 12px;border:1px solid var(--td-component-stroke);border-radius:12px;background:color-mix(in srgb,var(--td-bg-color-secondarycontainer) 60%,var(--td-bg-color-container))}.hero-copy{width:100%}.eyebrow{display:flex;align-items:center;gap:7px;color:var(--td-text-color-secondary);font-size:10px;font-weight:600;letter-spacing:.08em;text-transform:uppercase}.eyebrow i{width:4px;height:4px;border-radius:50%;background:var(--td-brand-color)}.hero-title-row{display:flex;align-items:flex-end;justify-content:space-between;gap:30px}.hero-copy h1{margin:5px 0 4px;font-size:clamp(21px,1.8vw,26px);font-weight:650;letter-spacing:-.015em}.hero-copy p{max-width:680px;margin:0;color:var(--td-text-color-secondary);font-size:12px;line-height:1.5;white-space:pre-line}
.hero-stats{min-height:45px;display:flex;align-items:center;flex:none}.hero-stats>div{min-width:78px;padding:0 16px}.hero-stats>div:first-child{padding-left:0}.hero-stats>div+div{border-left:1px solid var(--td-component-stroke)}.hero-stats>.portal-section-state{min-width:240px;padding-right:10px}.hero-stats strong,.hero-stats span{display:block}.hero-stats strong{font-size:21px;font-weight:650;line-height:1}.hero-stats span{margin-top:4px;color:var(--td-text-color-placeholder);font-size:10px}.stats-info{margin-left:5px;color:var(--td-text-color-placeholder);cursor:help}
.hero-search{width:100%;height:42px;display:flex;align-items:center;gap:9px;margin-top:12px;padding:0 10px 0 7px;border:1px solid var(--td-component-border);border-radius:9px;background:var(--td-bg-color-container);box-shadow:0 1px 2px rgba(15,23,42,.03);color:var(--td-text-color-secondary);cursor:text;font:inherit;text-align:left}.hero-search:hover{border-color:color-mix(in srgb,var(--td-brand-color) 70%,var(--td-component-border))}.hero-search:focus-visible{border-color:var(--td-brand-color);outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.search-icon{width:28px;height:28px;display:grid!important;flex:none!important;place-items:center;border-radius:7px;background:var(--td-brand-color-light);color:var(--td-brand-color);font-size:17px}.hero-search>span{min-width:0;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.hero-search em{padding:3px 8px;border-radius:999px;background:var(--td-bg-color-secondarycontainer);color:var(--td-text-color-placeholder);font-size:10px;font-style:normal;white-space:nowrap}.hero-search kbd{color:var(--td-text-color-placeholder);font:10px var(--td-font-family)}
.quick-actions{display:flex;flex-wrap:wrap;gap:2px 20px;margin-top:6px}.quick-actions button{display:flex;align-items:center;gap:5px;padding:3px 0;border:0;background:transparent;color:var(--td-text-color-secondary);cursor:pointer;font:11px var(--td-font-family)}.quick-actions button:hover{color:var(--td-brand-color)}.quick-actions button:focus-visible{border-radius:4px;outline:2px solid var(--td-brand-color-focus);outline-offset:2px}.quick-actions .t-icon{font-size:14px}
@media(max-width:1080px){.hero-title-row{align-items:flex-start;flex-direction:column;gap:10px}.hero-stats{width:100%}}
@media(max-width:680px){.portal-hero{padding-top:10px}.hero-surface{padding:14px 12px 10px}.hero-stats{flex-wrap:wrap}.hero-stats>div{min-width:0;flex:1;padding:0 10px}.hero-stats strong{font-size:19px}.hero-stats>.portal-section-state{min-width:0}.hero-search em,.hero-search kbd{display:none}.quick-actions{justify-content:space-between;gap:2px 10px}}
</style>
