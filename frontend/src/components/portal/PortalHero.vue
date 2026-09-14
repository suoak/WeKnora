<template>
  <section class="portal-hero" data-guide="portal-home">
    <div class="hero-copy">
      <span class="eyebrow">KnowHub · {{ t('portalExperience.eyebrow') }}</span>
      <h1>{{ t('portalExperience.heroTitle') }}</h1>
      <p>{{ t('portalExperience.heroDescription') }}</p>
    </div>

    <div class="hero-stats" :aria-label="t('portalExperience.scaleLabel')">
      <template v-if="loading">
        <t-skeleton v-for="item in 3" :key="item" animation="gradient" :row-col="[{ width: '72px', height: '30px' }, { width: '48px', height: '14px' }]" />
      </template>
      <template v-else>
        <div v-for="item in statItems" :key="item.label">
          <strong>{{ numberFormatter.format(item.value) }}</strong>
          <span>{{ item.label }}</span>
        </div>
      </template>
      <t-tooltip :content="t('portalExperience.scaleTooltip')" placement="bottom">
        <t-icon class="stats-info" name="info-circle" />
      </t-tooltip>
    </div>

    <button type="button" class="hero-search" @click="$emit('search')">
      <t-icon name="search" />
      <span>{{ t('portalExperience.searchPlaceholder') }}</span>
      <em>{{ hasActiveSpace ? t('portalExperience.currentScope', { space: activeSpaceName }) : t('portalExperience.noActiveScope') }}</em>
      <kbd>Ctrl K</kbd>
    </button>

    <div class="quick-actions" :aria-label="t('portalExperience.quickActionsLabel')">
      <button type="button" @click="$emit('search')"><t-icon name="search" />{{ t('portalExperience.quickActions.search') }}</button>
      <button type="button" @click="$emit('browse')"><t-icon name="folder" />{{ t('portalExperience.quickActions.browse') }}</button>
      <button v-if="supportsAgent" type="button" @click="$emit('agent')"><t-icon name="robot" />{{ t('portalExperience.quickActions.agent') }}</button>
      <button v-if="supportsTools" type="button" @click="$emit('tools')"><t-icon name="tools" />{{ t('portalExperience.quickActions.tools') }}</button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { KnowledgeScale } from '@/config/portalKnowledgeSummary'

const props = defineProps<{
  stats: KnowledgeScale
  loading: boolean
  activeSpaceName: string
  hasActiveSpace: boolean
  supportsAgent: boolean
  supportsTools: boolean
}>()
defineEmits<{ search: []; browse: []; agent: []; tools: [] }>()
const { t } = useI18n()
const numberFormatter = new Intl.NumberFormat()
const statItems = computed(() => [
  { value: props.stats.spaces, label: t('portalExperience.spaces') },
  { value: props.stats.knowledgeBases, label: t('portalExperience.knowledgeBases') },
  { value: props.stats.files, label: t('portalExperience.files') },
])
</script>

<style scoped lang="less">
.portal-hero{padding:28px 0 24px;border-bottom:1px solid var(--td-component-stroke)}
.hero-copy{max-width:760px}.eyebrow{color:var(--td-brand-color);font-size:11px;font-weight:600;letter-spacing:.08em}.hero-copy h1{margin:7px 0 8px;font-size:clamp(22px,2.1vw,28px);font-weight:600;letter-spacing:-.015em}.hero-copy p{max-width:680px;margin:0;color:var(--td-text-color-secondary);font-size:14px;line-height:1.65;white-space:pre-line}
.hero-stats{display:flex;align-items:center;gap:0;margin-top:22px}.hero-stats>div{min-width:116px;padding-right:28px}.hero-stats>div+div{padding-left:28px;border-left:1px solid var(--td-component-stroke)}.hero-stats strong,.hero-stats span{display:block}.hero-stats strong{font-size:25px;font-weight:600;line-height:1.15}.hero-stats span{margin-top:4px;color:var(--td-text-color-placeholder);font-size:12px}.stats-info{margin-left:12px;color:var(--td-text-color-placeholder);cursor:help}
.hero-search{width:min(820px,100%);height:46px;display:flex;align-items:center;gap:10px;margin-top:22px;padding:0 13px;border:1px solid var(--td-component-border);border-radius:10px;background:var(--td-bg-color-container);color:var(--td-text-color-secondary);cursor:text;font:inherit;text-align:left}.hero-search:hover{border-color:var(--td-brand-color)}.hero-search>.t-icon{color:var(--td-brand-color);font-size:19px}.hero-search>span{min-width:0;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.hero-search em{padding:3px 8px;border-radius:999px;background:var(--td-bg-color-secondarycontainer);color:var(--td-text-color-placeholder);font-size:11px;font-style:normal;white-space:nowrap}.hero-search kbd{color:var(--td-text-color-placeholder);font:10px var(--td-font-family)}
.quick-actions{display:flex;flex-wrap:wrap;gap:4px 20px;margin-top:11px}.quick-actions button{display:flex;align-items:center;gap:6px;padding:4px 0;border:0;background:transparent;color:var(--td-text-color-secondary);cursor:pointer;font:12px var(--td-font-family)}.quick-actions button:hover{color:var(--td-brand-color)}.quick-actions .t-icon{font-size:15px}
@media(max-width:680px){.portal-hero{padding:20px 0}.hero-stats>div{min-width:82px;padding-right:14px}.hero-stats>div+div{padding-left:14px}.hero-stats strong{font-size:21px}.hero-search em,.hero-search kbd{display:none}}
</style>
