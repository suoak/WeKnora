<template>
  <section class="stage-card" :class="{ 'is-last': isLast, 'has-active': hasActiveSpace }">
    <header><span class="stage-node">{{ String(index + 1).padStart(2, '0') }}</span><div><h3 :title="stage.name">{{ stage.name }}</h3><p>{{ stage.description }}</p></div></header>
    <div class="stage-body">
      <div class="stage-stats">{{ t('portalMap.stageInventoryCompact', { spaces: stats.spaces, kb: stats.knowledgeBases, files: numberFormatter.format(stats.files) }) }}</div>
      <div v-if="spaces.length" class="stage-spaces">
        <SpaceSummary v-for="space in spaces" :key="space.tenant_id" :space="space" variant="compact"
          :is-active-space="space.tenant_id === activeTenantId" @enter="$emit('enter', $event)" @restricted="$emit('restricted', $event)"
          @search="$emit('search', $event)" @ask="$emit('ask', $event)" />
      </div>
      <div v-else class="stage-empty">{{ t('portalMap.emptyStage') }}</div>
    </div>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PortalSpace, PortalStage } from '@/api/portal'
import type { KnowledgeScale } from '@/config/portalKnowledgeSummary'
import SpaceSummary from './SpaceSummary.vue'
const props=defineProps<{stage:PortalStage;index:number;spaces:PortalSpace[];stats:KnowledgeScale;activeTenantId:number;isLast:boolean}>()
defineEmits<{enter:[space:PortalSpace];restricted:[space:PortalSpace];search:[space:PortalSpace];ask:[space:PortalSpace]}>()
const {t}=useI18n()
const numberFormatter=new Intl.NumberFormat()
const hasActiveSpace=computed(()=>props.spaces.some(space=>space.tenant_id===props.activeTenantId))
</script>
<style scoped lang="less">
.stage-card{position:relative;min-width:0}.stage-card::after{position:absolute;z-index:0;top:12px;left:23px;width:calc(100% + 8px);height:1px;background:var(--td-component-border);content:''}.stage-card.is-last::after{display:none}header{position:relative;z-index:1;display:flex;min-width:0;gap:7px;align-items:flex-start}header>div{min-width:0;padding-right:4px;background:var(--td-bg-color-page)}.stage-node{width:24px;height:24px;display:grid;flex:none;place-items:center;border:1px solid color-mix(in srgb,var(--td-brand-color) 45%,var(--td-component-border));border-radius:50%;background:var(--td-bg-color-page);color:var(--td-brand-color);font-size:9px;font-weight:700}.has-active .stage-node{background:var(--td-brand-color);color:var(--td-text-color-anti)}h3{margin:0;font-size:12px;font-weight:650;line-height:1.3;overflow-wrap:break-word}header p{min-height:28px;margin:2px 0 0;color:var(--td-text-color-secondary);font-size:9px;line-height:1.4}.stage-body{margin-top:7px;padding:8px;border:1px solid var(--td-component-stroke);border-radius:9px;background:color-mix(in srgb,var(--td-bg-color-secondarycontainer) 45%,var(--td-bg-color-container))}.has-active .stage-body{border-color:color-mix(in srgb,var(--td-brand-color) 34%,var(--td-component-stroke))}.stage-stats{margin:0 0 6px;color:var(--td-text-color-placeholder);font-size:9px;line-height:1.4}.stage-spaces{display:flex;flex-direction:column;gap:6px}.stage-empty{min-height:34px;display:grid;place-items:center;border:1px dashed var(--td-component-border);border-radius:7px;color:var(--td-text-color-placeholder);font-size:9px;text-align:center}
</style>
