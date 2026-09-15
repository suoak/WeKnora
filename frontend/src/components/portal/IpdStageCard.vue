<template>
  <button type="button" class="stage-step" :class="{ active, current }" role="tab" :aria-selected="active"
    :aria-label="`${stage.name} · ${t('portal.flowOverview.stageSpaceCount', { count: spaceCount })}`" @click="$emit('select', stage.key)">
    <span class="stage-marker"><b>{{ String(index + 1).padStart(2, '0') }}</b><i aria-hidden="true"></i></span>
    <span class="stage-copy">
      <strong :title="stage.name">{{ stage.name }}</strong>
      <small :title="stage.description">{{ stage.description }}</small>
      <em>{{ t('portal.flowOverview.stageSpaceCount', { count: spaceCount }) }}</em>
    </span>
  </button>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PortalStage } from '@/api/portal'

defineProps<{ stage: PortalStage; index: number; spaceCount: number; active: boolean; current: boolean }>()
defineEmits<{ select: [stageKey: string] }>()
const { t } = useI18n()
</script>

<style scoped lang="less">
.stage-step{position:relative;display:grid;min-width:0;grid-template-columns:34px minmax(0,1fr);gap:9px;padding:8px 10px 10px;border:0;border-radius:9px;background:transparent;color:var(--portal-text-primary);cursor:pointer;font-family:var(--portal-font-family);text-align:left}.stage-step::before{position:absolute;top:25px;right:calc(100% - 17px);width:calc(100% - 34px);height:2px;background:var(--portal-line);content:''}.stage-step:first-child::before{display:none}.stage-marker{position:relative;z-index:1;display:flex;flex-direction:column;align-items:center;gap:4px}.stage-marker b{font-size:12px;font-weight:700;line-height:16px}.stage-marker i{width:10px;height:10px;border:2px solid var(--portal-line-strong);border-radius:50%;background:var(--portal-page-surface)}.stage-copy{display:flex;min-width:0;flex-direction:column}.stage-copy strong{overflow:hidden;font-size:15px;font-weight:650;line-height:20px;text-overflow:ellipsis;white-space:nowrap}.stage-copy small{overflow:hidden;color:var(--portal-text-secondary);font-size:12px;font-weight:400;line-height:18px;text-overflow:ellipsis;white-space:nowrap}.stage-copy em{margin-top:3px;color:var(--portal-text-muted);font-size:12px;font-style:normal;font-weight:500;line-height:18px}.stage-step:hover{background:var(--portal-surface-hover)}.stage-step:focus-visible{outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.stage-step.active{background:var(--portal-brand-surface)}.stage-step.active .stage-marker b,.stage-step.active .stage-copy strong{color:var(--td-brand-color-active)}.stage-step.active .stage-marker i{border-color:var(--td-brand-color);background:var(--td-brand-color);box-shadow:0 0 0 3px var(--td-brand-color-focus)}.stage-step.current:not(.active) .stage-marker i{border-color:var(--td-brand-color);background:var(--portal-page-surface)}
</style>
