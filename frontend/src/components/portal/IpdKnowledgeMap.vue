<template>
  <section class="ipd-map">
    <header class="section-header">
      <div><span class="section-kicker">{{ t('portal.brandSubtitle') }}</span><h2>{{ t('portalMap.ipdTitle') }}</h2><p>{{ t('portalMap.ipdDescription') }}</p></div>
      <div v-if="!loading && !error" class="map-summary"><span>{{ t('portalMap.coverage', { covered: coveredStages, total: stages.length }) }}</span><span>{{ t('portal.spaceCount', { count: summary.ipd.spaces }) }}</span></div>
      <t-skeleton v-else-if="loading" class="summary-loading" animation="gradient" :row-col="[{ width: '220px', height: '18px' }]" />
    </header>

    <PortalSectionState v-if="error" @retry="$emit('retry')" />
    <template v-else>
      <div class="lifecycle-scroll" :aria-label="t('portalMap.stageNavigationLabel')">
        <div v-if="loading" class="lifecycle-rail is-loading"><t-skeleton v-for="item in 7" :key="item" animation="gradient" :row-col="[{ width: '72%', height: '18px' }, { width: '88%', height: '14px' }, { width: '48%', height: '14px' }]" /></div>
        <div v-else class="lifecycle-rail" role="tablist">
          <IpdStageCard v-for="(stage,index) in stages" :key="stage.key" :stage="stage" :index="index"
            :space-count="spacesFor(stage.key).length" :active="stage.key === activeStageKey" :current="stage.key === currentStageKey"
            @select="selectStage" />
        </div>
      </div>

      <section v-if="loading" class="stage-panel is-loading"><t-skeleton animation="gradient" :row-col="[{ width: '180px', height: '22px' }, { width: '260px', height: '16px' }]" /><div class="stage-space-grid"><SpaceSummary v-for="item in 3" :key="item" loading variant="normal" /></div></section>
      <section v-else-if="activeStage" class="stage-panel" role="tabpanel">
        <header class="stage-panel-header">
          <div class="active-stage-number">{{ activeStageNumber }}</div>
          <div><h3>{{ activeStage.name }}</h3><p>{{ activeStage.description }}</p></div>
          <span>{{ t('portal.flowOverview.stageSpaceCount', { count: activeStageSpaces.length }) }}</span>
        </header>
        <div v-if="activeStageSpaces.length" class="stage-space-grid">
          <SpaceSummary v-for="space in activeStageSpaces" :key="space.tenant_id" :space="space" variant="normal"
            :is-active-space="space.tenant_id === activeTenantId" @enter="$emit('enter', $event)" @restricted="$emit('restricted', $event)"
            @search="$emit('search', $event)" @ask="$emit('ask', $event)" />
        </div>
        <div v-else class="stage-empty">{{ t('portalMap.emptyStage') }}</div>
      </section>
      <div v-else class="portal-empty">{{ t('portal.emptyAll') }}</div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PortalSpace, PortalStage } from '@/api/portal'
import { buildKnowledgeHierarchySummary } from '@/config/portalKnowledgeSummary'
import { defaultPortalStageKey } from '@/stores/portalState'
import IpdStageCard from './IpdStageCard.vue'
import SpaceSummary from './SpaceSummary.vue'
import PortalSectionState from './PortalSectionState.vue'

const props=defineProps<{stages:PortalStage[];spaces:PortalSpace[];loading:boolean;error:boolean;activeTenantId:number}>()
defineEmits<{enter:[space:PortalSpace];restricted:[space:PortalSpace];search:[space:PortalSpace];ask:[space:PortalSpace];retry:[]}>()
const {t}=useI18n()
const activeStageKey=ref('')
const selectionTouched=ref(false)
const spacesFor=(stage:string)=>props.spaces.filter(space=>space.stages.includes(stage))
const summary=computed(()=>buildKnowledgeHierarchySummary(props.spaces,props.stages.map(stage=>stage.key)))
const coveredStages=computed(()=>props.stages.filter(stage=>spacesFor(stage.key).length>0).length)
const currentStageKey=computed(()=>props.stages.find(stage=>props.spaces.some(space=>space.tenant_id===props.activeTenantId&&space.stages.includes(stage.key)))?.key||'')
const defaultStageKey=computed(()=>defaultPortalStageKey(props.stages,props.spaces,props.activeTenantId))
const activeStage=computed(()=>props.stages.find(stage=>stage.key===activeStageKey.value)||null)
const activeStageSpaces=computed(()=>activeStage.value?spacesFor(activeStage.value.key):[])
const activeStageNumber=computed(()=>String(Math.max(0,props.stages.findIndex(stage=>stage.key===activeStageKey.value))+1).padStart(2,'0'))

function selectStage(stageKey:string){selectionTouched.value=true;activeStageKey.value=stageKey}
function selectDefaultStage(){activeStageKey.value=defaultStageKey.value}
watch(defaultStageKey,(stageKey)=>{if(!selectionTouched.value)activeStageKey.value=stageKey},{immediate:true})
watch(()=>props.activeTenantId,()=>{selectionTouched.value=false;selectDefaultStage()})
watch(()=>props.stages,()=>{if(!props.stages.some(stage=>stage.key===activeStageKey.value)){selectionTouched.value=false;selectDefaultStage()}},{deep:false})
</script>

<style scoped lang="less">
.ipd-map{margin-top:28px}.section-header{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;margin-bottom:14px}.section-kicker{display:block;margin-bottom:3px;color:var(--td-brand-color-active);font-size:12px;font-weight:650;letter-spacing:.06em}.section-header h2{margin:0;color:var(--portal-text-primary);font-size:var(--portal-section-title);font-weight:700;line-height:1.35}.section-header p{margin:4px 0 0;color:var(--portal-text-secondary);font-size:13px;line-height:1.5}.map-summary{display:flex;flex:none;gap:8px;color:var(--portal-text-muted);font-size:12px;font-weight:500}.map-summary span{padding:4px 8px;border-radius:999px;background:var(--portal-surface-soft)}.summary-loading{width:220px;flex:none}.lifecycle-scroll{min-width:0;overflow-x:auto;padding:2px 0 5px;scrollbar-width:thin;scrollbar-color:var(--portal-line-strong) transparent}.lifecycle-rail{display:grid;min-width:980px;grid-template-columns:repeat(7,minmax(132px,1fr));gap:0}.lifecycle-rail.is-loading{gap:18px;padding:12px}.stage-panel{margin-top:12px;padding:18px 20px 20px;border-radius:12px;background:var(--portal-surface-soft)}.stage-panel-header{display:flex;align-items:center;gap:11px;margin-bottom:14px}.active-stage-number{width:38px;height:38px;display:grid;flex:none;place-items:center;border-radius:10px;background:var(--td-brand-color);color:var(--td-text-color-anti);font-size:13px;font-weight:700}.stage-panel-header h3{margin:0;color:var(--portal-text-primary);font-size:18px;font-weight:700;line-height:24px}.stage-panel-header p{margin:1px 0 0;color:var(--portal-text-secondary);font-size:13px;line-height:18px}.stage-panel-header>span{margin-left:auto;color:var(--portal-text-muted);font-size:12px;font-weight:500}.stage-space-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px}.stage-empty,.portal-empty{min-height:92px;display:grid;place-items:center;color:var(--portal-text-muted);font-size:13px}.stage-panel.is-loading .stage-space-grid{margin-top:14px}
@media(max-width:1599px){.stage-space-grid{grid-template-columns:repeat(3,minmax(0,1fr))}}
@media(max-width:1399px){.lifecycle-rail{grid-template-columns:repeat(7,150px)}}
@media(max-width:1100px){.stage-space-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:900px){.ipd-map{margin-top:22px}.section-header{align-items:flex-start;flex-direction:column;gap:8px}.stage-panel{padding:16px}.stage-panel-header{align-items:flex-start;flex-wrap:wrap}.stage-panel-header>span{width:100%;margin-left:49px}.stage-space-grid{grid-template-columns:1fr}}
</style>
