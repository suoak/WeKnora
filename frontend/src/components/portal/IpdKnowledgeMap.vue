<template>
  <section class="ipd-map">
    <header class="section-header">
      <h2>{{ t('portalMap.ipdTitle') }}</h2>
      <div v-if="!loading && !error" class="map-summary">
        <span>{{ t('portalMap.stageCount', { count: stages.length }) }}</span>
        <span>{{ t('portalMap.coverage', { covered: coveredStages, total: stages.length }) }}</span>
        <span>{{ t('portal.spaceCount', { count: summary.ipd.spaces }) }}</span>
      </div>
      <t-skeleton v-else-if="loading" class="summary-loading" animation="gradient" :row-col="[{ width: '220px', height: '18px' }]" />
    </header>

    <PortalSectionState v-if="error" @retry="$emit('retry')" />
    <template v-else>
      <div class="lifecycle-scroll" :aria-label="t('portalMap.stageNavigationLabel')">
        <div v-if="loading" class="lifecycle-rail is-loading">
          <t-skeleton v-for="item in Math.max(stages.length, 8)" :key="item" animation="gradient" :row-col="[{ width: '72%', height: '18px' }, { width: '70%', height: '14px' }, { width: '48%', height: '14px' }]" />
        </div>
        <nav v-else class="lifecycle-rail" :style="{ gridTemplateColumns: `repeat(${Math.max(stages.length, 1)}, minmax(118px, 1fr))` }">
          <IpdStageCard v-for="(stage,index) in stages" :key="stage.key" :stage="stage" :index="index"
            :short-description="stage.description || ''" :space-count="spacesFor(stage.key).length"
            :current="stage.key === currentStageKey" :focused="stage.key === focusedStageKey" @navigate="navigateToStage" />
        </nav>
      </div>

      <section ref="groupsRef" class="stage-groups" :aria-label="t('portalMap.allStageSpaces')">
        <template v-if="loading">
          <section v-for="item in 3" :key="item" class="stage-group is-loading">
            <t-skeleton animation="gradient" :row-col="[{ width: '180px', height: '20px' }, { width: '94px', height: '14px' }]" />
            <div class="stage-space-grid"><SpaceSummary v-for="card in item === 3 ? 3 : 1" :key="card" loading variant="normal" /></div>
          </section>
        </template>
        <section v-for="(stage,index) in stages" v-else :key="stage.key" class="stage-group"
          :class="{ highlighted: highlightedStageKey === stage.key }" :data-stage-group="stage.key">
          <header class="stage-group__header">
            <div class="stage-group__identity">
              <b>{{ String(index + 1).padStart(2, '0') }}</b>
              <div><h3>{{ stage.name }}</h3><p>{{ stage.description }}</p></div>
            </div>
            <span>{{ t('portal.spaceCount', { count: spacesFor(stage.key).length }) }}</span>
          </header>
          <div v-if="spacesFor(stage.key).length" class="stage-space-grid">
            <SpaceSummary v-for="space in spacesFor(stage.key)" :key="space.tenant_id" :space="space" variant="normal"
              :stage-number="String(index + 1).padStart(2, '0')" :stage-name="stage.name"
              :fallback-description="stageFallbackDescription(stage.key)"
              :is-active-space="space.tenant_id === activeTenantId"
              @enter="$emit('enter', $event)" @restricted="$emit('restricted', $event)"
              @search="$emit('search', $event)" @ask="$emit('ask', $event)" />
          </div>
          <div v-else class="stage-empty">{{ t('portalMap.emptyStage') }}</div>
        </section>
        <div v-if="!loading && !stages.length" class="portal-empty">{{ t('portal.emptyAll') }}</div>
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
const groupsRef=ref<HTMLElement|null>(null)
const focusedStageKey=ref('')
const highlightedStageKey=ref('')
let highlightTimer:ReturnType<typeof setTimeout>|undefined
const stageLocaleSuffix:Record<string,string>={insight:'insight',concept_market:'concept1',concept_product:'concept2',architecture:'architecture',design:'design',development:'development',testing:'testing',lmt:'lmt'}
// Presentation-only fallback: localized copy is never written back to Space data.
const stageFallbackDescription=(stageKey:string)=>t(`portalMap.stageFallback.${stageLocaleSuffix[stageKey]||stageKey}`)
const spacesFor=(stage:string)=>props.spaces.filter(space=>space.stages.includes(stage))
const summary=computed(()=>buildKnowledgeHierarchySummary(props.spaces,props.stages.map(stage=>stage.key)))
const coveredStages=computed(()=>props.stages.filter(stage=>spacesFor(stage.key).length>0).length)
const currentStageKey=computed(()=>props.stages.find(stage=>props.spaces.some(space=>space.tenant_id===props.activeTenantId&&space.stages.includes(stage.key)))?.key||'')
function navigateToStage(stageKey:string){
  focusedStageKey.value=stageKey
  highlightedStageKey.value=stageKey
  if(highlightTimer)clearTimeout(highlightTimer)
  requestAnimationFrame(()=>{
    const target=groupsRef.value?.querySelector<HTMLElement>(`[data-stage-group="${CSS.escape(stageKey)}"]`)
    ;(target||groupsRef.value)?.scrollIntoView({behavior:'smooth',block:'center'})
  })
  highlightTimer=setTimeout(()=>{highlightedStageKey.value='';focusedStageKey.value=''},800)
}
onBeforeUnmount(()=>{if(highlightTimer)clearTimeout(highlightTimer)})
</script>

<style scoped lang="less">
.ipd-map{position:relative;isolation:isolate;margin-top:30px;padding:23px 25px 27px;overflow:hidden;border:1px solid color-mix(in srgb,var(--td-brand-color) 12%,var(--portal-line));border-radius:16px;background-color:color-mix(in srgb,var(--td-bg-color-container) 96%,var(--portal-brand-surface));background-image:linear-gradient(to right,color-mix(in srgb,var(--td-brand-color) 3%,transparent) 1px,transparent 1px),linear-gradient(to bottom,color-mix(in srgb,var(--td-brand-color) 3%,transparent) 1px,transparent 1px),radial-gradient(circle at 88% 1%,color-mix(in srgb,var(--td-brand-color) 7%,transparent),transparent 27%);background-size:32px 32px,32px 32px,auto;box-shadow:0 8px 26px rgba(15,23,42,.04),inset 0 1px 0 rgba(255,255,255,.7)}.section-header{display:flex;align-items:center;justify-content:space-between;gap:24px;margin-bottom:18px}.section-header h2{margin:0;color:var(--portal-text-primary);font-size:var(--portal-section-title);font-weight:720;letter-spacing:-.012em;line-height:1.35}.map-summary{display:flex;flex:none;gap:8px;color:var(--portal-text-muted);font-size:12.5px;font-weight:550}.map-summary span{padding:5px 10px;border:1px solid color-mix(in srgb,var(--portal-line) 82%,transparent);border-radius:7px;background:color-mix(in srgb,var(--td-bg-color-container) 84%,transparent);box-shadow:0 1px 3px rgba(15,23,42,.025)}.map-summary span:nth-child(2){border-color:color-mix(in srgb,var(--td-brand-color) 22%,var(--portal-line));background:color-mix(in srgb,var(--portal-brand-surface) 75%,var(--td-bg-color-container));color:var(--td-brand-color-active)}.summary-loading{width:220px;flex:none}.lifecycle-scroll{min-width:0;overflow-x:auto;padding:0 0 13px;scrollbar-width:thin;scrollbar-color:var(--portal-line-strong) transparent}.lifecycle-rail{display:grid;min-width:1040px;gap:0;padding:9px 8px 7px;border:1px solid color-mix(in srgb,var(--td-brand-color) 10%,var(--portal-line));border-radius:12px;background:linear-gradient(180deg,color-mix(in srgb,var(--td-bg-color-container) 92%,transparent),color-mix(in srgb,var(--portal-surface-soft) 80%,transparent));box-shadow:inset 0 1px 0 rgba(255,255,255,.72)}.lifecycle-rail.is-loading{grid-template-columns:repeat(8,minmax(118px,1fr));gap:18px;padding:16px}.stage-groups{margin-top:3px}.stage-group{padding:20px 2px 22px;border-top:1px solid color-mix(in srgb,var(--portal-line-strong) 72%,transparent);transition:background-color .16s ease,box-shadow .16s ease}.stage-group:first-child{border-top:0}.stage-group.highlighted{border-radius:12px;background:color-mix(in srgb,var(--portal-brand-surface) 68%,transparent);box-shadow:0 0 0 3px var(--td-brand-color-focus)}.stage-group__header{display:flex;align-items:flex-start;justify-content:space-between;gap:24px;margin-bottom:12px}.stage-group__identity{display:flex;align-items:flex-start;gap:12px}.stage-group__identity>b{min-width:32px;color:var(--td-brand-color-active);font-size:13px;font-weight:780;letter-spacing:.08em;line-height:24px}.stage-group__identity h3{margin:0;color:var(--portal-text-primary);font-size:17px;font-weight:690;line-height:24px}.stage-group__identity p{margin:1px 0 0;color:var(--portal-text-muted);font-size:12.5px;font-weight:500;line-height:18px}.stage-group__header>span{padding:2px 7px;border-radius:6px;background:color-mix(in srgb,var(--portal-surface-soft) 82%,transparent);color:var(--portal-text-muted);font-size:12.5px;font-weight:550;line-height:20px}.stage-space-grid{display:grid;grid-template-columns:repeat(4,minmax(240px,340px));gap:14px}.stage-empty,.portal-empty{min-height:38px;display:flex;max-width:340px;align-items:center;padding-left:44px;color:var(--portal-text-muted);font-size:12.5px}.stage-group.is-loading>.stage-space-grid{margin-top:12px}
@media(max-width:1599px){.stage-space-grid{grid-template-columns:repeat(3,minmax(240px,340px))}}
@media(max-width:1399px){.lifecycle-rail{min-width:1024px}}
@media(max-width:1199px){.stage-space-grid{grid-template-columns:repeat(2,minmax(240px,340px))}}
@media(max-width:900px){.ipd-map{margin-top:24px;padding:18px 14px 22px}.section-header{align-items:flex-start;flex-direction:column;gap:8px}.map-summary{flex-wrap:wrap}.stage-space-grid{grid-template-columns:minmax(240px,340px)}.stage-group{padding:18px 0 20px}}
</style>
