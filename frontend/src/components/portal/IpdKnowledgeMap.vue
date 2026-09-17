<template>
  <section class="ipd-map">
    <header class="section-header">
      <div class="section-title"><span class="section-title__icon"><t-icon name="map" /></span><div><h2>{{ t('portalMap.ipdTitle') }}</h2><p>{{ t('portalMap.ipdDescription') }}</p></div></div>
      <div v-if="!loading && !error" class="map-summary">
        <span class="lifecycle-metric">{{ t('portalMap.stageCount', { count: stages.length }) }}</span>
        <span class="lifecycle-metric coverage-metric">{{ t('portalMap.coverage', { covered: coveredStages, total: stages.length }) }}</span>
        <span class="asset-metric">{{ t('portalMap.stageInventory', { spaces: summary.ipd.spaces, kb: numberFormatter.format(summary.ipd.knowledgeBases), files: numberFormatter.format(summary.ipd.files) }) }}</span>
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
            :focused="stage.key === focusedStageKey" @navigate="navigateToStage" />
        </nav>
      </div>

      <section ref="groupsRef" class="stage-card-grid" :aria-label="t('portalMap.allStageSpaces')">
        <template v-if="loading">
          <section v-for="item in Math.max(stages.length, 8)" :key="item" class="stage-card is-loading">
            <t-skeleton animation="gradient" :row-col="[{ width: '180px', height: '20px' }, { width: '94px', height: '14px' }]" />
            <div class="stage-spaces"><SpaceSummary loading variant="compact" /></div>
          </section>
        </template>
        <section v-for="(stage,index) in stages" v-else :key="stage.key" class="stage-card"
          :class="[`stage-card--${stage.key}`, { highlighted: highlightedStageKey === stage.key, 'stage-card--wide': spacesFor(stage.key).length > 1, 'stage-card--single': spacesFor(stage.key).length === 1, 'stage-card--current': spacesFor(stage.key).length === 1 && spacesFor(stage.key)[0].tenant_id === activeTenantId }]"
          :data-stage-group="stage.key">
          <header class="stage-card__header">
            <div class="stage-card__identity">
              <span class="stage-card__icon"><t-icon :name="resolveStageIcon(stage.key)" /></span>
              <div><h3>{{ stage.name }}</h3><p>{{ stage.description }}</p></div>
            </div>
            <span>{{ t('portal.spaceCount', { count: spacesFor(stage.key).length }) }}</span>
          </header>
          <b class="stage-card__number">{{ String(index + 1).padStart(2, '0') }}</b>
          <div v-if="spacesFor(stage.key).length === 1" class="stage-spaces stage-spaces--single">
            <SpaceSummary :space="spacesFor(stage.key)[0]" variant="flat"
              :hide-title="sameNormalizedText(stage.name, spacesFor(stage.key)[0].display_name)"
              :suppress-description="sameNormalizedText(stage.description, spacesFor(stage.key)[0].description)"
              :fallback-description="stageFallbackDescription(stage.key)"
              :is-active-space="spacesFor(stage.key)[0].tenant_id === activeTenantId"
              @enter="$emit('enter', $event)" @restricted="$emit('restricted', $event)"
              @search="$emit('search', $event)" @ask="$emit('ask', $event)" />
          </div>
          <div v-else-if="spacesFor(stage.key).length > 1" class="stage-spaces">
            <SpaceSummary v-for="space in spacesFor(stage.key)" :key="space.tenant_id" :space="space" variant="compact"
              :visual-icon="resolveStageSpaceIcon(stage.key, space)"
              :fallback-description="stageFallbackDescription(stage.key)"
              :is-active-space="space.tenant_id === activeTenantId"
              @enter="$emit('enter', $event)" @restricted="$emit('restricted', $event)"
              @search="$emit('search', $event)" @ask="$emit('ask', $event)" />
          </div>
          <div v-else class="stage-empty"><t-icon :name="stage.key === 'insight' ? 'compass' : resolveStageIcon(stage.key)" /><span>{{ t('portalMap.emptyStage') }}</span></div>
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
import { resolveStageIcon, resolveStageSpaceIcon } from '@/config/portalVisualIcons'
import IpdStageCard from './IpdStageCard.vue'
import SpaceSummary from './SpaceSummary.vue'
import PortalSectionState from './PortalSectionState.vue'

const props=defineProps<{stages:PortalStage[];spaces:PortalSpace[];loading:boolean;error:boolean;activeTenantId:number}>()
defineEmits<{enter:[space:PortalSpace];restricted:[space:PortalSpace];search:[space:PortalSpace];ask:[space:PortalSpace];retry:[]}>()
const {t}=useI18n()
const numberFormatter=new Intl.NumberFormat()
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
const normalizedText=(value?:string)=>value?.trim().replace(/\s+/g,' ').toLocaleLowerCase()||''
const sameNormalizedText=(left?:string,right?:string)=>Boolean(normalizedText(left))&&normalizedText(left)===normalizedText(right)
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
.ipd-map{position:relative;isolation:isolate;padding:23px 25px 27px;overflow:hidden;border:1px solid var(--portal-line);border-radius:14px;background:var(--td-bg-color-container);box-shadow:0 7px 24px rgba(15,23,42,.04)}.section-header{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;margin-bottom:18px}.section-title{min-width:0}.section-header h2{margin:0;color:var(--portal-text-primary);font-size:var(--portal-section-title);font-weight:720;letter-spacing:-.012em;line-height:1.35}.section-title p{margin:4px 0 0;color:var(--portal-text-muted);font-size:12.5px;line-height:18px}.map-summary{display:flex;flex:none;gap:8px;color:var(--portal-text-muted);font-size:12.5px;font-weight:550}.map-summary span{padding:5px 10px;border:1px solid var(--portal-line);border-radius:7px;background:var(--portal-surface-soft);box-shadow:0 1px 3px rgba(15,23,42,.025)}.map-summary span:nth-child(2){border-color:color-mix(in srgb,var(--td-brand-color) 22%,var(--portal-line));background:var(--portal-brand-surface);color:var(--td-brand-color-active)}.summary-loading{width:220px;flex:none}.lifecycle-scroll{min-width:0;overflow-x:auto;padding:0 0 14px;scrollbar-width:thin;scrollbar-color:var(--portal-line-strong) transparent}.lifecycle-scroll::-webkit-scrollbar{height:5px}.lifecycle-scroll::-webkit-scrollbar-thumb{border-radius:5px;background:var(--portal-line-strong)}.lifecycle-rail{display:grid;min-width:1040px;gap:0;padding:8px 8px 7px;border:1px solid color-mix(in srgb,var(--td-brand-color) 10%,var(--portal-line));border-radius:12px;background:linear-gradient(180deg,color-mix(in srgb,var(--portal-brand-surface) 38%,var(--td-bg-color-container)),var(--portal-surface-soft));box-shadow:inset 0 1px 0 rgba(255,255,255,.72)}.lifecycle-rail.is-loading{grid-template-columns:repeat(8,minmax(118px,1fr));gap:18px;padding:16px}.stage-card-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));grid-auto-flow:dense;align-items:stretch;gap:15px;margin-top:3px}.stage-card{position:relative;min-width:0;padding:17px 16px 15px;border:1px solid var(--portal-line);border-radius:12px;background:color-mix(in srgb,var(--portal-surface-soft) 62%,var(--td-bg-color-container));box-shadow:0 2px 8px rgba(15,23,42,.025);transition:border-color .16s ease,background-color .16s ease,box-shadow .16s ease}.stage-card--wide{grid-column:span 2}.stage-card.highlighted{border-color:color-mix(in srgb,var(--td-brand-color) 55%,var(--portal-line));background:color-mix(in srgb,var(--portal-brand-surface) 62%,var(--td-bg-color-container));box-shadow:0 0 0 3px var(--td-brand-color-focus)}.stage-card__header{display:flex;align-items:flex-start;justify-content:space-between;gap:14px;margin:0 36px 12px 0}.stage-card__identity h3{margin:0;color:var(--portal-text-primary);font-size:17px;font-weight:690;line-height:23px}.stage-card__identity p{margin:2px 0 0;color:var(--portal-text-muted);font-size:12.5px;font-weight:500;line-height:18px}.stage-card__header>span{flex:none;color:var(--portal-text-muted);font-size:12.5px;font-weight:550;line-height:20px}.stage-card__number{position:absolute;top:-9px;right:13px;width:30px;height:30px;display:grid;place-items:center;border:1px solid color-mix(in srgb,var(--td-brand-color) 18%,var(--portal-line));border-radius:50%;background:var(--td-bg-color-container);color:var(--td-brand-color-active);box-shadow:0 2px 7px rgba(15,23,42,.07);font-size:12.5px;font-weight:780;letter-spacing:.03em}.stage-spaces{display:grid;grid-template-columns:minmax(0,1fr);align-content:start;gap:10px}.stage-card--wide .stage-spaces{grid-template-columns:repeat(2,minmax(0,1fr))}.stage-empty,.portal-empty{min-height:112px;display:grid;place-items:center;border:1px dashed var(--portal-line-strong);border-radius:10px;background:color-mix(in srgb,var(--td-bg-color-container) 58%,transparent);color:var(--portal-text-muted);font-size:12.5px}.stage-card.is-loading>.stage-spaces{margin-top:12px}
.section-title{display:flex;align-items:center;gap:11px}.section-title__icon{width:34px;height:34px;display:grid;flex:none;place-items:center;border:1px solid color-mix(in srgb,var(--td-brand-color) 18%,var(--portal-line));border-radius:9px;background:var(--portal-brand-surface);color:var(--td-brand-color-active);font-size:18px}.stage-card:hover{border-color:color-mix(in srgb,var(--stage-accent,var(--td-brand-color)) 27%,var(--portal-line-strong));box-shadow:0 5px 15px rgba(15,23,42,.045)}.stage-card__identity{display:flex;min-width:0;align-items:flex-start;gap:10px}.stage-card__icon{width:30px;height:30px;display:grid;flex:none;place-items:center;border:1px solid color-mix(in srgb,var(--stage-accent,var(--td-brand-color)) 18%,var(--portal-line));border-radius:8px;background:color-mix(in srgb,var(--stage-accent,var(--td-brand-color)) 9%,var(--td-bg-color-container));color:var(--stage-accent,var(--td-brand-color-active));font-size:17px}.stage-empty{display:flex;align-items:center;justify-content:center;flex-direction:column;gap:7px}.stage-empty>.t-icon{font-size:21px;color:var(--stage-accent,var(--portal-text-muted));opacity:.72}.stage-card--insight{--stage-accent:#7c6ee6}.stage-card--concept_market{--stage-accent:#b7791f}.stage-card--concept_product{--stage-accent:#a16207}.stage-card--architecture{--stage-accent:#3978b8}.stage-card--design{--stage-accent:#1687a7}.stage-card--development{--stage-accent:#0f8b68}.stage-card--testing{--stage-accent:#c66a22}.stage-card--lmt{--stage-accent:#64748b}
.stage-card{display:flex;box-sizing:border-box;height:100%;flex-direction:column}.stage-spaces--single,.stage-empty{flex:1}.stage-spaces--single{align-content:stretch}
.section-title p,.stage-card__identity p{color:var(--portal-text-secondary)}.map-summary span{color:var(--portal-text-metadata);font-weight:600}.map-summary .coverage-metric{border-color:color-mix(in srgb,var(--td-brand-color) 25%,var(--portal-line));background:var(--portal-brand-surface);color:var(--td-brand-color-active)}.map-summary .asset-metric{background:var(--td-bg-color-container);color:var(--portal-text-secondary)}.stage-card{background:var(--td-bg-color-container);box-shadow:none}.stage-card:hover{border-color:color-mix(in srgb,var(--stage-accent,var(--td-brand-color)) 30%,var(--portal-line-strong));background:color-mix(in srgb,var(--stage-accent,var(--td-brand-color)) 2.5%,var(--td-bg-color-container));box-shadow:0 4px 12px rgba(15,23,42,.045)}.stage-card--current{border-color:color-mix(in srgb,var(--td-brand-color) 44%,var(--portal-line));background:color-mix(in srgb,var(--portal-brand-surface) 32%,var(--td-bg-color-container))}.stage-card--current:hover{border-color:color-mix(in srgb,var(--td-brand-color) 52%,var(--portal-line));box-shadow:0 4px 12px rgba(15,118,110,.06)}
@media(max-width:1599px){.stage-card-grid{grid-template-columns:repeat(3,minmax(0,1fr))}}
@media(max-width:1399px){.lifecycle-rail{min-width:1024px}}
@media(max-width:1199px){.stage-card-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:900px){.ipd-map{padding:18px 14px 22px}.section-header{align-items:flex-start;flex-direction:column;gap:8px}.map-summary{flex-wrap:wrap}.stage-card-grid{grid-template-columns:minmax(0,1fr)}.stage-card--wide{grid-column:span 1}.stage-card--wide .stage-spaces{grid-template-columns:minmax(0,1fr)}}
</style>
