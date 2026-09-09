<template>
  <section v-if="stage" class="stage-context">
    <div class="stage-heading">
      <span class="stage-index">{{ String(stage.display_order + 1).padStart(2, '0') }}</span>
      <div>
        <small>{{ t('portal.stageDetail.current') }}</small>
        <h2>{{ t(`portal.stages.${stage.key}.name`) }}</h2>
        <p>{{ t(`portal.stages.${stage.key}.description`) }}</p>
      </div>
    </div>
    <div class="stage-results">
      <strong>{{ t('portal.stageDetail.resultTitle') }}</strong>
      <span>{{ t('portal.stageDetail.resultCount', { count: spaceCount }) }}</span>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PortalStage } from '@/api/portal'

const props = defineProps<{ selectedStage: string; stages: PortalStage[]; spaceCount: number }>()
const { t } = useI18n()
const stage = computed(() => props.stages.find(item => item.key === props.selectedStage))
</script>

<style scoped lang="less">
.stage-context{display:flex;align-items:center;justify-content:space-between;gap:24px;padding:20px 24px;border:1px solid color-mix(in srgb,var(--td-brand-color) 28%,var(--td-component-border));border-radius:16px;background:linear-gradient(135deg,var(--td-brand-color-light),var(--td-bg-color-container) 58%)}
.stage-heading{display:flex;gap:14px}.stage-index{flex:none;width:42px;height:42px;display:grid;place-items:center;border-radius:12px;background:var(--td-brand-color);color:#fff;font-weight:700}.stage-heading small{color:var(--td-brand-color);font-weight:600}.stage-heading h2{margin:4px 0 7px;font-size:23px}.stage-heading p{margin:0;color:var(--td-text-color-secondary);line-height:1.6}
.stage-results{flex:none;padding-left:24px;border-left:1px solid var(--td-component-stroke);text-align:right}.stage-results strong,.stage-results span{display:block}.stage-results span{margin-top:5px;color:var(--td-text-color-secondary);font-size:13px}
@media(max-width:680px){.stage-context{align-items:flex-start;flex-direction:column}.stage-results{padding:0;border:0;text-align:left}}
</style>
