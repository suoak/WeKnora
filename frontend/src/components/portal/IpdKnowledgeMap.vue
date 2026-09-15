<template>
  <section class="ipd-map">
    <header class="section-header"><div><h2>{{ t('portalMap.ipdTitle') }}</h2><p>{{ t('portalMap.ipdDescription') }}</p></div><div v-if="!loading && !error" class="map-summary"><span>{{ t('portalMap.coverage', { covered: coveredStages, total: stages.length }) }}</span><span>{{ t('portalMap.stageInventory', { spaces: summary.ipd.spaces, kb: summary.ipd.knowledgeBases, files: numberFormatter.format(summary.ipd.files) }) }}</span></div><t-skeleton v-else-if="loading" class="summary-loading" animation="gradient" :row-col="[{ width: '220px', height: '18px' }]" /></header>
    <PortalSectionState v-if="error" @retry="$emit('retry')" />
    <div v-else class="stage-scroll"><div class="stage-track">
      <template v-if="loading">
        <div v-for="item in 7" :key="item" class="stage-loading-card"><t-skeleton animation="gradient" :row-col="[{ width: '70%', height: '18px' }, { width: '92%', height: '28px' }, { width: '100%', height: '12px' }]" /><SpaceSummary loading variant="compact" /></div>
      </template>
      <template v-else>
        <IpdStageCard v-for="(stage,index) in stages" :key="stage.key" :stage="stage" :index="index" :is-last="index === stages.length - 1"
          :spaces="spacesFor(stage.key)" :stats="summary.phases[stage.key]" :active-tenant-id="activeTenantId"
          @enter="$emit('enter',$event)" @restricted="$emit('restricted',$event)" @search="$emit('search',$event)" @ask="$emit('ask',$event)" />
      </template>
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
import PortalSectionState from './PortalSectionState.vue'
const props=defineProps<{stages:PortalStage[];spaces:PortalSpace[];loading:boolean;error:boolean;activeTenantId:number}>()
defineEmits<{enter:[space:PortalSpace];restricted:[space:PortalSpace];search:[space:PortalSpace];ask:[space:PortalSpace];retry:[]}>()
const {t}=useI18n()
const numberFormatter=new Intl.NumberFormat()
const spacesFor=(stage:string)=>props.spaces.filter(space=>space.stages.includes(stage))
const summary=computed(()=>buildKnowledgeHierarchySummary(props.spaces,props.stages.map(stage=>stage.key)))
const coveredStages=computed(()=>props.stages.filter(stage=>spacesFor(stage.key).length>0).length)
</script>
<style scoped lang="less">
.ipd-map{margin-top:18px}.section-header{display:flex;align-items:flex-end;justify-content:space-between;gap:20px;margin-bottom:10px}.section-header h2{margin:0;font-size:17px;font-weight:650}.section-header p{margin:3px 0 0;color:var(--td-text-color-secondary);font-size:11px}.map-summary{display:flex;flex-wrap:wrap;justify-content:flex-end;gap:5px 12px;color:var(--td-text-color-placeholder);font-size:10px}.map-summary span:first-child{padding:3px 7px;border-radius:999px;background:var(--td-bg-color-secondarycontainer);color:var(--td-text-color-secondary)}.summary-loading{width:220px;flex:none}.stage-scroll{min-width:0;overflow-x:visible;padding-bottom:5px;scrollbar-width:thin;scrollbar-color:var(--td-component-border) transparent}.stage-track{display:grid;grid-template-columns:repeat(7,minmax(0,1fr));align-items:start;gap:8px}.stage-loading-card{min-width:0;padding:10px;border:1px solid var(--td-component-stroke);border-radius:10px;background:var(--td-bg-color-container)}.stage-loading-card .space-summary{min-height:74px;margin-top:10px}
@media(max-width:1399px){.stage-scroll{overflow-x:auto;overscroll-behavior-inline:contain;mask-image:linear-gradient(to right,#000 0,#000 calc(100% - 30px),transparent 100%)}.stage-track{grid-template-columns:none;grid-auto-flow:column;grid-auto-columns:minmax(172px,1fr);min-width:max-content;padding-right:30px}}
@media(max-width:900px){.section-header{align-items:flex-start;flex-direction:column;gap:7px}.map-summary{justify-content:flex-start}}
</style>
