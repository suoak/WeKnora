<template>
  <section class="stage-card">
    <header><span>{{ String(index + 1).padStart(2, '0') }}</span><div><h3>{{ stage.name }}</h3><p>{{ stage.description }}</p></div></header>
    <div class="stage-stats">{{ t('portalMap.stageInventory', { spaces: stats.spaces, kb: stats.knowledgeBases, files: numberFormatter.format(stats.files) }) }}</div>
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
.stage-card{min-width:210px;padding:14px;border:1px solid var(--td-component-stroke);border-radius:11px;background:var(--td-bg-color-container)}header{display:flex;gap:10px}header>span{width:27px;height:27px;display:grid;flex:none;place-items:center;border-radius:7px;background:var(--td-brand-color-light);color:var(--td-brand-color);font-size:11px;font-weight:700}h3{margin:1px 0 0;font-size:14px;font-weight:600}header p{min-height:34px;margin:4px 0 0;color:var(--td-text-color-secondary);font-size:11px;line-height:1.45}.stage-stats{margin:11px 0 9px;padding-top:9px;border-top:1px solid var(--td-component-stroke);color:var(--td-text-color-placeholder);font-size:10px;white-space:nowrap}.stage-spaces{display:flex;flex-direction:column;gap:7px}.stage-empty{min-height:70px;display:grid;place-items:center;border:1px dashed var(--td-component-border);border-radius:9px;color:var(--td-text-color-placeholder);font-size:11px}
</style>
