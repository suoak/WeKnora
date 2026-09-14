<template>
  <section class="stage-card">
    <header><span>{{ String(index + 1).padStart(2, '0') }}</span><div><h3 :title="stage.name">{{ stage.name }}</h3><p>{{ stage.description }}</p></div></header>
    <div class="stage-stats">{{ t('portalMap.stageInventoryCompact', { spaces: stats.spaces, kb: stats.knowledgeBases, files: numberFormatter.format(stats.files) }) }}</div>
    <div v-if="spaces.length" class="stage-spaces">
      <SpaceSummary v-for="space in spaces" :key="space.tenant_id" :space="space" variant="compact"
        :is-active-space="space.tenant_id === activeTenantId" @enter="$emit('enter', $event)" @restricted="$emit('restricted', $event)"
        @search="$emit('search', $event)" @ask="$emit('ask', $event)" />
    </div>
    <div v-else class="stage-empty">{{ t('portalMap.emptyStage') }}</div>
  </section>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PortalSpace, PortalStage } from '@/api/portal'
import type { KnowledgeScale } from '@/config/portalKnowledgeSummary'
import SpaceSummary from './SpaceSummary.vue'
defineProps<{stage:PortalStage;index:number;spaces:PortalSpace[];stats:KnowledgeScale;activeTenantId:number}>()
defineEmits<{enter:[space:PortalSpace];restricted:[space:PortalSpace];search:[space:PortalSpace];ask:[space:PortalSpace]}>()
const {t}=useI18n()
const numberFormatter=new Intl.NumberFormat()
</script>
<style scoped lang="less">
.stage-card{min-width:0;padding:10px;border:1px solid var(--td-component-stroke);border-radius:10px;background:var(--td-bg-color-container)}header{display:flex;min-width:0;gap:6px}header>span{width:23px;height:23px;display:grid;flex:none;place-items:center;border-radius:6px;background:var(--td-brand-color-light);color:var(--td-brand-color);font-size:10px;font-weight:700}header>div{min-width:0}h3{margin:1px 0 0;font-size:12px;font-weight:600;line-height:1.3;overflow-wrap:break-word}header p{min-height:30px;margin:3px 0 0;color:var(--td-text-color-secondary);font-size:10px;line-height:1.45}.stage-stats{min-height:29px;margin:8px 0 7px;padding-top:7px;border-top:1px solid var(--td-component-stroke);color:var(--td-text-color-placeholder);font-size:9px;line-height:1.45}.stage-spaces{display:flex;flex-direction:column;gap:6px}.stage-empty{min-height:64px;display:grid;place-items:center;border:1px dashed var(--td-component-border);border-radius:8px;color:var(--td-text-color-placeholder);font-size:10px;text-align:center}
</style>
