<template>
  <button type="button" class="stage-step" :class="{ current, focused }"
    :aria-current="current ? 'step' : undefined"
    :aria-label="`${stage.name} · ${shortDescription} · ${t('portal.flowOverview.stageSpaceCount', { count: spaceCount })}`"
    @click="$emit('navigate', stage.key)">
    <span class="stage-marker"><b>{{ String(index + 1).padStart(2, '0') }}</b><i aria-hidden="true"></i></span>
    <span class="stage-copy">
      <strong :title="stage.name">{{ stage.name }}</strong>
      <small>{{ shortDescription }}</small>
      <em>{{ t('portal.flowOverview.stageSpaceCount', { count: spaceCount }) }}</em>
    </span>
  </button>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PortalStage } from '@/api/portal'

defineProps<{ stage: PortalStage; index: number; shortDescription: string; spaceCount: number; current: boolean; focused: boolean }>()
defineEmits<{ navigate: [stageKey: string] }>()
const { t } = useI18n()
</script>

<style scoped lang="less">
.stage-step{position:relative;display:grid;min-width:0;grid-template-columns:38px minmax(0,1fr);gap:10px;padding:9px 10px 11px;border:0;border-radius:9px;background:transparent;color:var(--portal-text-primary);cursor:pointer;font-family:var(--portal-font-family);text-align:left}.stage-step::before{position:absolute;top:27px;right:calc(100% - 19px);width:calc(100% - 38px);height:2px;background:var(--portal-line-strong);content:''}.stage-step:not(:last-child)::after{position:absolute;top:18px;right:-3px;color:var(--portal-line-strong);content:'›';font-size:14px;font-weight:700}.stage-step:first-child::before{display:none}.stage-marker{position:relative;z-index:1;display:flex;flex-direction:column;align-items:center;gap:5px}.stage-marker b{font-size:12.5px;font-weight:700;line-height:16px}.stage-marker i{box-sizing:border-box;width:12px;height:12px;border:2px solid var(--portal-line-strong);border-radius:50%;background:var(--td-bg-color-container)}.stage-copy{display:flex;min-width:0;flex-direction:column}.stage-copy strong{overflow:hidden;font-size:15px;font-weight:650;line-height:21px;text-overflow:ellipsis;white-space:nowrap}.stage-copy small{overflow:hidden;color:var(--portal-text-secondary);font-size:13px;font-weight:500;line-height:19px;text-overflow:ellipsis;white-space:nowrap}.stage-copy em{margin-top:2px;color:var(--portal-text-muted);font-size:12.5px;font-style:normal;font-weight:500;line-height:18px}.stage-step:hover{background:var(--portal-surface-hover)}.stage-step:focus-visible{outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.stage-step.current,.stage-step.focused{background:color-mix(in srgb,var(--portal-brand-surface) 72%,transparent)}.stage-step.current .stage-marker b,.stage-step.current .stage-copy strong,.stage-step.focused .stage-marker b,.stage-step.focused .stage-copy strong{color:var(--td-brand-color-active)}.stage-step.current .stage-marker i,.stage-step.focused .stage-marker i{border-color:var(--td-brand-color);background:var(--td-brand-color);box-shadow:0 0 0 5px color-mix(in srgb,var(--td-brand-color) 13%,transparent)}
</style>
