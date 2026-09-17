<template>
  <section class="overview-strip" :aria-label="t('portalExperience.scaleLabel')">
    <div class="overview-copy">
      <span class="overview-mark"><t-icon name="data-base" /></span>
      <div><strong>{{ t('portalExperience.eyebrow') }}</strong><p>{{ t('portalExperience.scaleLabel') }}</p></div>
    </div>
    <t-skeleton v-if="loading" class="overview-loading" animation="gradient" :row-col="[{ width: '360px', height: '28px' }]" />
    <dl v-else>
      <div><dt>{{ numberFormatter.format(metrics.spaces) }}</dt><dd>{{ t('portalExperience.spaces') }}</dd></div>
      <div><dt>{{ numberFormatter.format(metrics.knowledgeBases) }}</dt><dd>{{ t('portalExperience.knowledgeBases') }}</dd></div>
      <div><dt>{{ numberFormatter.format(metrics.files) }}</dt><dd>{{ t('portalExperience.files') }}</dd></div>
    </dl>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { KnowledgeScale } from '@/config/portalKnowledgeSummary'

defineProps<{ metrics: KnowledgeScale; loading: boolean }>()
const { t } = useI18n()
const numberFormatter = new Intl.NumberFormat()
</script>

<style scoped lang="less">
.overview-strip{display:flex;min-height:74px;align-items:center;justify-content:space-between;gap:24px;margin-bottom:14px;padding:10px 18px;border:1px solid var(--portal-line);border-radius:12px;background:linear-gradient(102deg,var(--td-bg-color-container),color-mix(in srgb,var(--portal-brand-surface) 58%,var(--td-bg-color-container)));box-shadow:0 3px 12px rgba(15,23,42,.025)}.overview-copy{display:flex;min-width:0;align-items:center;gap:11px}.overview-mark{width:34px;height:34px;display:grid;flex:none;place-items:center;border:1px solid color-mix(in srgb,var(--td-brand-color) 18%,var(--portal-line));border-radius:9px;background:var(--portal-brand-surface);color:var(--td-brand-color-active);font-size:18px}.overview-copy strong{color:var(--portal-text-primary);font-size:15px;font-weight:700}.overview-copy p{margin:2px 0 0;color:var(--portal-text-muted);font-size:12.5px;line-height:18px}.overview-strip dl{display:flex;flex:none;margin:0}.overview-strip dl>div{min-width:104px;padding:0 17px;border-left:1px solid var(--portal-line)}dt{color:var(--portal-text-primary);font-size:19px;font-weight:740;line-height:24px}dd{margin:0;color:var(--portal-text-muted);font-size:12px;line-height:17px}.overview-loading{width:360px;flex:none}@media(max-width:760px){.overview-strip{align-items:flex-start;flex-direction:column;gap:10px;padding:14px}.overview-strip dl{width:100%}.overview-strip dl>div{min-width:0;flex:1;padding:0 10px}.overview-strip dl>div:first-child{padding-left:0;border-left:0}.overview-loading{width:100%}}
</style>
