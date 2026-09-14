<template>
  <section class="ipd-map">
    <header class="section-header"><div><h2>{{ t('portalMap.ipdTitle') }}</h2><p>{{ t('portalMap.ipdDescription') }}</p></div><div class="map-summary"><span>{{ t('portalMap.coverage', { covered: coveredStages, total: stages.length }) }}</span><span>{{ t('portalMap.stageInventory', { spaces: summary.ipd.spaces, kb: summary.ipd.knowledgeBases, files: numberFormatter.format(summary.ipd.files) }) }}</span></div></header>
    <div v-if="loading" class="map-loading"><SpaceSummary v-for="item in 7" :key="item" loading variant="compact" /></div>
    <div v-else class="stage-scroll"><div class="stage-track">
      <IpdStageCard v-for="(stage,index) in stages" :key="stage.key" :stage="stage" :index="index"
        :spaces="spacesFor(stage.key)" :stats="summary.phases[stage.key]" :active-tenant-id="activeTenantId"
        @enter="$emit('enter',$event)" @restricted="$emit('restricted',$event)" @search="$emit('search',$event)" @ask="$emit('ask',$event)" />
    </div></div>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PortalSpace, PortalStage } from '@/api/portal'
import { buildKnowledgeHierarchySummary } from '@/config/portalKnowledgeSummary'
import IpdStageCard from './IpdStageCard.vue'
import SpaceSummary from './SpaceSummary.vue'
const props=defineProps<{stages:PortalStage[];spaces:PortalSpace[];loading:boolean;activeTenantId:number}>()
defineEmits<{enter:[space:PortalSpace];restricted:[space:PortalSpace];search:[space:PortalSpace];ask:[space:PortalSpace]}>()
const {t}=useI18n()
const numberFormatter=new Intl.NumberFormat()
const spacesFor=(stage:string)=>props.spaces.filter(space=>space.stages.includes(stage))
const summary=computed(()=>buildKnowledgeHierarchySummary(props.spaces,props.stages.map(stage=>stage.key)))
const coveredStages=computed(()=>props.stages.filter(stage=>spacesFor(stage.key).length>0).length)
</script>
<style scoped lang="less">
.ipd-map{margin-top:30px}.section-header{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;margin-bottom:13px}.section-header h2{margin:0;font-size:18px;font-weight:600}.section-header p{margin:5px 0 0;color:var(--td-text-color-secondary);font-size:12px}.map-summary{display:flex;flex-wrap:wrap;justify-content:flex-end;gap:6px 15px;color:var(--td-text-color-placeholder);font-size:11px}.map-summary span:first-child{padding:3px 8px;border-radius:999px;background:var(--td-brand-color-light);color:var(--td-brand-color)}.stage-scroll{overflow-x:auto;padding-bottom:8px}.stage-track{display:grid;grid-auto-flow:column;grid-auto-columns:minmax(210px,1fr);min-width:1510px;align-items:start;gap:9px}.map-loading{display:grid;grid-template-columns:repeat(7,1fr);gap:9px}.map-loading .space-summary{min-width:0;min-height:180px}@media(max-width:900px){.section-header{align-items:flex-start;flex-direction:column;gap:8px}.map-summary{justify-content:flex-start}.map-loading{grid-template-columns:repeat(3,1fr)}}
</style>
