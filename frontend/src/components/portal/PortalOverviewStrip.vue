<template>
  <section class="overview-strip" :aria-label="t('portalExperience.eyebrow')">
    <div class="overview-copy">
      <span class="overview-mark"><t-icon name="data-base" /></span>
      <strong>{{ t('portalExperience.eyebrow') }}</strong>
    </div>
    <t-skeleton v-if="loading" class="overview-loading" animation="gradient" :row-col="[{ width: '360px', height: '28px' }]" />
    <p v-else class="asset-summary">{{ formatAssetSummary(metrics, t, numberFormatter) }}</p>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { KnowledgeScale } from '@/config/portalKnowledgeSummary'
import { formatAssetSummary } from '@/config/portalAssetSummary'

defineProps<{ metrics: KnowledgeScale; loading: boolean }>()
const { t } = useI18n()
const numberFormatter = new Intl.NumberFormat()
</script>

<style scoped lang="less">
.overview-strip{display:flex;min-height:58px;align-items:center;justify-content:space-between;gap:24px;margin-bottom:14px;padding:10px 18px;border:1px solid var(--portal-line);border-radius:12px;background:linear-gradient(102deg,var(--td-bg-color-container),color-mix(in srgb,var(--portal-brand-surface) 42%,var(--td-bg-color-container)));box-shadow:0 3px 12px rgba(15,23,42,.025)}.overview-copy{display:flex;min-width:0;align-items:center;gap:11px}.overview-mark{width:34px;height:34px;display:grid;flex:none;place-items:center;border:1px solid color-mix(in srgb,var(--td-brand-color) 18%,var(--portal-line));border-radius:9px;background:var(--portal-brand-surface);color:var(--td-brand-color-active);font-size:18px}.overview-copy strong{color:var(--portal-text-primary);font-size:15px;font-weight:700}.asset-summary{flex:none;margin:0;color:var(--portal-text-primary);font-size:13px;font-weight:650;line-height:20px}.overview-loading{width:300px;flex:none}@media(max-width:760px){.overview-strip{align-items:flex-start;flex-direction:column;gap:10px;padding:14px}.asset-summary{white-space:normal}.overview-loading{width:100%}}
</style>
