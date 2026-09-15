<template>
  <section class="ipd-map">
    <header class="section-header">
      <h2>{{ t('portalMap.ipdTitle') }}</h2>
      <div v-if="!loading && !error" class="map-summary">
        <span>{{ t('portalMap.coverage', { covered: coveredStages, total: stages.length }) }}</span>
        <span>{{ t('portal.spaceCount', { count: summary.ipd.spaces }) }}</span>
      </div>
      <t-skeleton v-else-if="loading" class="summary-loading" animation="gradient" :row-col="[{ width: '220px', height: '18px' }]" />
    </header>

    <PortalSectionState v-if="error" @retry="$emit('retry')" />
    <template v-else>
      <div class="lifecycle-scroll" :aria-label="t('portalMap.stageNavigationLabel')">
        <div v-if="loading" class="lifecycle-rail is-loading">
          <t-skeleton v-for="item in 7" :key="item" animation="gradient" :row-col="[{ width: '72%', height: '18px' }, { width: '70%', height: '14px' }, { width: '48%', height: '14px' }]" />
        </div>
        <nav v-else class="lifecycle-rail">
          <IpdStageCard v-for="(stage,index) in stages" :key="stage.key" :stage="stage" :index="index"
            :short-description="stageShortDescriptions[index] || stage.description || ''" :space-count="spacesFor(stage.key).length"
            :current="stage.key === currentStageKey" :focused="stage.key === focusedStageKey" @navigate="navigateToStage" />
        </nav>
      </div>

      <section class="knowledge-matrix" :aria-label="t('portalMap.allStageSpaces')">
        <header><h3>{{ t('portalMap.allStageSpaces') }}</h3><span v-if="!loading">{{ t('portal.spaceCount', { count: sortedSpaces.length }) }}</span></header>
        <div v-if="loading" class="space-matrix">
          <SpaceSummary v-for="item in 8" :key="item" loading variant="normal" />
        </div>
        <div v-else-if="sortedSpaces.length" ref="matrixRef" class="space-matrix">
          <div v-for="space in sortedSpaces" :key="space.tenant_id" class="matrix-item"
            :class="{ highlighted: highlightedStageKey && space.stages.includes(highlightedStageKey) }"
            :data-stages="space.stages.join('|')">
            <SpaceSummary :space="space" variant="normal" :stage-number="stageIdentity(space).number"
              :stage-name="stageIdentity(space).name" :is-active-space="space.tenant_id === activeTenantId"
              @enter="$emit('enter', $event)" @restricted="$emit('restricted', $event)"
              @search="$emit('search', $event)" @ask="$emit('ask', $event)" />
          </div>
        </div>
        <div v-else class="portal-empty">{{ t('portal.emptyAll') }}</div>
      </section>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PortalSpace, PortalStage } from '@/api/portal'
import { buildKnowledgeHierarchySummary } from '@/config/portalKnowledgeSummary'
import IpdStageCard from './IpdStageCard.vue'
import SpaceSummary from './SpaceSummary.vue'
import PortalSectionState from './PortalSectionState.vue'

const props=defineProps<{stages:PortalStage[];spaces:PortalSpace[];loading:boolean;error:boolean;activeTenantId:number}>()
defineEmits<{enter:[space:PortalSpace];restricted:[space:PortalSpace];search:[space:PortalSpace];ask:[space:PortalSpace];retry:[]}>()
const {t}=useI18n()
const matrixRef=ref<HTMLElement|null>(null)
const focusedStageKey=ref('')
const highlightedStageKey=ref('')
let highlightTimer:ReturnType<typeof setTimeout>|undefined
const stageShortDescriptions=computed(()=>[
  t('portalMap.stageShort.concept1'),t('portalMap.stageShort.concept2'),t('portalMap.stageShort.architecture'),
  t('portalMap.stageShort.design'),t('portalMap.stageShort.development'),t('portalMap.stageShort.testing'),t('portalMap.stageShort.lmt'),
])
const spacesFor=(stage:string)=>props.spaces.filter(space=>space.stages.includes(stage))
const summary=computed(()=>buildKnowledgeHierarchySummary(props.spaces,props.stages.map(stage=>stage.key)))
const coveredStages=computed(()=>props.stages.filter(stage=>spacesFor(stage.key).length>0).length)
const currentStageKey=computed(()=>props.stages.find(stage=>props.spaces.some(space=>space.tenant_id===props.activeTenantId&&space.stages.includes(stage.key)))?.key||'')
const stageOrder=computed(()=>new Map(props.stages.map((stage,index)=>[stage.key,index])))
const sortedSpaces=computed(()=>props.spaces.map((space,index)=>({space,index})).sort((left,right)=>{
  const leftStage=Math.min(...left.space.stages.map(stage=>stageOrder.value.get(stage)??props.stages.length))
  const rightStage=Math.min(...right.space.stages.map(stage=>stageOrder.value.get(stage)??props.stages.length))
  return leftStage-rightStage||left.index-right.index
}).map(item=>item.space))

function stageIdentity(space:PortalSpace){
  const index=props.stages.findIndex(stage=>space.stages.includes(stage.key))
  const stage=index>=0?props.stages[index]:undefined
  return {number:index>=0?String(index+1).padStart(2,'0'):'',name:stage?.name||''}
}
function navigateToStage(stageKey:string){
  focusedStageKey.value=stageKey
  highlightedStageKey.value=stageKey
  if(highlightTimer)clearTimeout(highlightTimer)
  requestAnimationFrame(()=>{
    const target=Array.from(matrixRef.value?.querySelectorAll<HTMLElement>('.matrix-item')||[])
      .find(item=>(item.dataset.stages||'').split('|').includes(stageKey))
    ;(target||matrixRef.value)?.scrollIntoView({behavior:'smooth',block:'center'})
  })
  highlightTimer=setTimeout(()=>{highlightedStageKey.value='';focusedStageKey.value=''},1400)
}
onBeforeUnmount(()=>{if(highlightTimer)clearTimeout(highlightTimer)})
</script>

<style scoped lang="less">
.ipd-map{margin-top:26px}.section-header{display:flex;align-items:center;justify-content:space-between;gap:24px;margin-bottom:12px}.section-header h2{margin:0;color:var(--portal-text-primary);font-size:var(--portal-section-title);font-weight:700;line-height:1.35}.map-summary{display:flex;flex:none;gap:8px;color:var(--portal-text-muted);font-size:12px;font-weight:500}.map-summary span{padding:4px 8px;border-radius:999px;background:var(--portal-surface-soft)}.summary-loading{width:220px;flex:none}.lifecycle-scroll{min-width:0;overflow-x:auto;padding:2px 0 5px;scrollbar-width:thin;scrollbar-color:var(--portal-line-strong) transparent}.lifecycle-rail{display:grid;min-width:980px;grid-template-columns:repeat(7,minmax(132px,1fr));gap:0}.lifecycle-rail.is-loading{gap:18px;padding:12px}.knowledge-matrix{margin-top:13px}.knowledge-matrix>header{display:flex;align-items:center;justify-content:space-between;margin-bottom:10px}.knowledge-matrix h3{margin:0;color:var(--portal-text-primary);font-size:15px;font-weight:650;line-height:22px}.knowledge-matrix header span{color:var(--portal-text-muted);font-size:12px;font-weight:500}.space-matrix{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px}.matrix-item{min-width:0;border-radius:11px;transition:background-color .2s ease,box-shadow .2s ease}.matrix-item.highlighted{background:var(--portal-brand-surface);box-shadow:0 0 0 3px var(--td-brand-color-focus)}.portal-empty{min-height:92px;display:grid;place-items:center;color:var(--portal-text-muted);font-size:13px}
@media(max-width:1599px){.space-matrix{grid-template-columns:repeat(3,minmax(0,1fr))}}
@media(max-width:1399px){.lifecycle-rail{grid-template-columns:repeat(7,150px)}}
@media(max-width:1199px){.space-matrix{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:900px){.ipd-map{margin-top:22px}.section-header{align-items:flex-start;flex-direction:column;gap:8px}.space-matrix{grid-template-columns:1fr}}
</style>
