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
.portal-hero{padding:18px 0 0}.hero-surface{position:relative;isolation:isolate;box-sizing:border-box;min-height:180px;overflow:hidden;padding:24px 28px 17px;border:1px solid color-mix(in srgb,var(--td-brand-color) 8%,var(--portal-line));border-radius:16px;background:radial-gradient(circle at 82% 8%,color-mix(in srgb,var(--td-brand-color) 9%,transparent),transparent 32%),linear-gradient(115deg,var(--portal-brand-surface),color-mix(in srgb,var(--portal-brand-surface) 52%,var(--portal-surface-soft)))}.hero-surface::before{position:absolute;z-index:-1;inset:0;background-image:linear-gradient(to right,var(--portal-text-primary) 1px,transparent 1px),linear-gradient(to bottom,var(--portal-text-primary) 1px,transparent 1px);background-size:24px 24px;content:'';opacity:.025;pointer-events:none}.hero-copy{width:100%}.eyebrow{display:flex;align-items:center;gap:8px;color:var(--portal-text-secondary);font-size:12.5px;font-weight:650;letter-spacing:.04em}.eyebrow i{width:5px;height:5px;border-radius:50%;background:var(--td-brand-color)}.hero-title-row{display:flex;align-items:flex-end;justify-content:space-between;gap:44px}.hero-copy h1{margin:7px 0 5px;color:var(--portal-text-primary);font-size:var(--portal-page-title);font-weight:700;letter-spacing:-.015em;line-height:1.22}.hero-copy p{max-width:780px;margin:0;color:var(--portal-text-secondary);font-size:14px;font-weight:400;line-height:1.6;white-space:pre-line}
.hero-stats{min-height:58px;display:flex;align-items:center;flex:none}.hero-stats>div{min-width:104px;padding:0 22px}.hero-stats>div:first-child{padding-left:0}.hero-stats>div+div{border-left:1px solid var(--portal-line)}.hero-stats>.portal-section-state{min-width:240px;padding-right:10px}.hero-stats strong,.hero-stats span{display:block}.hero-stats strong{color:var(--portal-text-primary);font-size:32px;font-weight:700;line-height:1}.hero-stats span{margin-top:6px;color:var(--portal-text-muted);font-size:12.5px;font-weight:500}.stats-info{margin-left:6px;color:var(--portal-text-muted);cursor:help}
.hero-search{width:100%;height:46px;display:flex;align-items:center;gap:10px;margin-top:17px;padding:0 12px 0 8px;border:1px solid var(--portal-line-strong);border-radius:10px;background:var(--td-bg-color-container);box-shadow:0 1px 2px rgba(15,23,42,.03);color:var(--portal-text-secondary);cursor:text;font:14px var(--portal-font-family);text-align:left}.hero-search:hover{border-color:color-mix(in srgb,var(--td-brand-color) 65%,var(--portal-line-strong))}.hero-search:focus-visible{border-color:var(--td-brand-color);outline:3px solid var(--td-brand-color-focus);outline-offset:1px}.search-icon{width:30px;height:30px;display:grid!important;flex:none!important;place-items:center;border-radius:8px;background:var(--td-brand-color-light);color:var(--td-brand-color-active);font-size:18px}.hero-search>span{min-width:0;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.hero-search em{padding:4px 9px;border-radius:999px;background:var(--portal-surface-soft);color:var(--portal-text-muted);font-size:12.5px;font-style:normal;font-weight:500;white-space:nowrap}.hero-search kbd{color:var(--portal-text-muted);font:12.5px var(--portal-font-family)}
.quick-actions{display:flex;flex-wrap:wrap;gap:2px 24px;margin-top:8px}.quick-actions button{display:flex;align-items:center;gap:6px;padding:4px 0;border:0;background:transparent;color:var(--portal-text-secondary);cursor:pointer;font:13px var(--portal-font-family)}.quick-actions button:hover{color:var(--td-brand-color-active)}.quick-actions button:focus-visible{border-radius:4px;outline:2px solid var(--td-brand-color-focus);outline-offset:2px}.quick-actions .t-icon{font-size:16px}
@media(max-width:1080px){.hero-title-row{align-items:flex-start;flex-direction:column;gap:10px}.hero-stats{width:100%}}
@media(max-width:680px){.portal-hero{padding-top:12px}.hero-surface{padding:18px 14px 14px}.hero-stats{flex-wrap:wrap}.hero-stats>div{min-width:0;flex:1;padding:0 12px}.hero-stats strong{font-size:26px}.hero-stats>.portal-section-state{min-width:0}.hero-search em,.hero-search kbd{display:none}.quick-actions{justify-content:space-between;gap:2px 12px}}
</style>
