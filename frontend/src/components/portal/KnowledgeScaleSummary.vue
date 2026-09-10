<template>
  <div class="knowledge-scale" :class="`knowledge-scale--${tone}`">
    <span><strong>{{ format(stats.spaces) }}</strong>{{ t('portal.flowOverview.spacesMetric') }}</span>
    <span><strong>{{ format(stats.knowledgeBases) }}</strong>{{ t('portal.flowOverview.knowledgeBasesMetric') }}</span>
    <span><strong>{{ format(stats.files) }}</strong>{{ t('portal.flowOverview.filesMetric') }}</span>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { KnowledgeScale } from '@/config/portalKnowledgeSummary'

withDefaults(defineProps<{ stats: KnowledgeScale; tone?: 'default' | 'compact' }>(), { tone: 'default' })
const { locale, t } = useI18n()
const format = (value: number) => new Intl.NumberFormat(locale.value).format(value)
</script>

<style scoped lang="less">
.knowledge-scale{display:flex;align-items:center;flex-wrap:wrap;gap:10px 26px;color:var(--td-text-color-secondary);font-size:12px}.knowledge-scale span{display:flex;align-items:baseline;gap:6px}.knowledge-scale strong{color:var(--td-text-color-primary);font-size:22px;line-height:1}.knowledge-scale--compact{gap:6px 14px}.knowledge-scale--compact strong{font-size:14px}.knowledge-scale--compact span:not(:last-child)::after{content:'·';margin-left:8px;color:var(--td-text-color-placeholder)}
</style>
