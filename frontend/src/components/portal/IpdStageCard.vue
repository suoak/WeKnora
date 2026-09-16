<template>
  <button type="button" class="stage-step" :class="{ current, focused }"
    :aria-current="current ? 'step' : undefined"
    :aria-label="`${stage.name} · ${shortDescription} · ${t('portal.flowOverview.stageSpaceCount', { count: spaceCount })}`"
    @click="$emit('navigate', stage.key)">
    <span class="stage-marker"><b>{{ String(index + 1).padStart(2, '0') }}</b></span>
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
.stage-step{position:relative;display:flex;min-width:0;align-items:center;flex-direction:column;padding:11px 8px 14px;border:0;border-radius:10px;background:transparent;color:var(--portal-text-primary);cursor:pointer;font-family:var(--portal-font-family);text-align:center;transition:background-color .16s ease}.stage-step::before{position:absolute;z-index:0;top:27px;right:50%;width:100%;height:2px;background:color-mix(in srgb,var(--td-brand-color) 20%,var(--portal-line-strong));content:''}.stage-step:first-child::before{display:none}.stage-marker{position:relative;z-index:1;display:grid;width:34px;height:34px;place-items:center;border:2px solid color-mix(in srgb,var(--td-brand-color) 18%,var(--portal-line-strong));border-radius:50%;background:var(--td-bg-color-container);box-shadow:0 0 0 5px color-mix(in srgb,var(--td-bg-color-container) 90%,transparent),0 2px 6px rgba(15,23,42,.06)}.stage-marker b{color:var(--portal-text-secondary);font-size:12.5px;font-weight:750;letter-spacing:.02em;line-height:1}.stage-copy{display:flex;min-width:0;width:100%;align-items:center;flex-direction:column;margin-top:8px}.stage-copy strong{max-width:100%;overflow:hidden;font-size:15px;font-weight:680;line-height:21px;text-overflow:ellipsis;white-space:nowrap}.stage-copy small{max-width:100%;overflow:hidden;color:var(--portal-text-secondary);font-size:13px;font-weight:500;line-height:19px;text-overflow:ellipsis;white-space:nowrap}.stage-copy em{margin-top:2px;color:var(--portal-text-muted);font-size:12.5px;font-style:normal;font-weight:500;line-height:18px}.stage-step:hover{background:color-mix(in srgb,var(--portal-surface-hover) 78%,transparent)}.stage-step:focus-visible{outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.stage-step.current,.stage-step.focused{background:color-mix(in srgb,var(--portal-brand-surface) 66%,transparent)}.stage-step.current .stage-marker,.stage-step.focused .stage-marker{border-color:var(--td-brand-color);background:var(--td-brand-color);box-shadow:0 0 0 5px color-mix(in srgb,var(--td-brand-color) 13%,transparent),0 3px 8px rgba(15,118,110,.14)}.stage-step.current .stage-marker b,.stage-step.focused .stage-marker b{color:#fff}.stage-step.current .stage-copy strong,.stage-step.focused .stage-copy strong{color:var(--td-brand-color-active)}
</style>
