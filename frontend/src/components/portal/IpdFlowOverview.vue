<template>
  <section class="flow-overview">
    <header class="flow-header">
      <div><span>{{ t('portal.flowOverview.eyebrow') }}</span><h2>{{ t('portal.flowOverview.title') }}</h2><p>{{ t('portal.flowOverview.description') }}</p></div>
      <div class="flow-actions">
        <t-button v-if="selectedStage!=='all'" variant="text" @click="$emit('select','all')">{{ t('portal.flowOverview.viewAll') }}</t-button>
        <div class="coverage" :style="{background:coverageBackground}"><strong>{{ coveredStages }}/{{ stages.length }}</strong><small>{{ t('portal.flowOverview.coverageMetric') }}</small></div>
      </div>
    </header>

    <div class="metrics">
      <div><strong>{{ spaces.length }}</strong><span>{{ t('portal.flowOverview.spacesMetric') }}</span></div>
      <div><strong>{{ knowledgeBaseCount }}</strong><span>{{ t('portal.flowOverview.knowledgeBasesMetric') }}</span></div>
      <div><strong>{{ fileCount }}</strong><span>{{ t('portal.flowOverview.filesMetric') }}</span></div>
      <div><strong>{{ coveredStages }}</strong><span>{{ t('portal.flowOverview.coveredStagesMetric') }}</span></div>
    </div>

    <div class="flow-scroll">
      <div class="flow-track">
        <button v-for="(stage,index) in stages" :key="stage.key" class="stage-column"
          :class="{ active:selectedStage===stage.key, empty:!spacesFor(stage.key).length }"
          @click="$emit('select',stage.key)">
          <span class="phase"><i>{{ String(index+1).padStart(2,'0') }}</i><span>{{ t(`portal.stages.${stage.key}.name`) }}</span></span>
          <small class="stage-description">{{ t(`portal.stages.${stage.key}.description`) }}</small>
          <span class="space-total">{{ t('portal.flowOverview.stageSpaceCount',{count:spacesFor(stage.key).length}) }}</span>
          <span v-if="!spacesFor(stage.key).length" class="empty-label">{{ t('portal.flowOverview.noSpaces') }}</span>
          <span v-for="space in spacesFor(stage.key).slice(0,4)" v-else :key="space.tenant_id" class="space-pill">
            <strong>{{ space.display_name }}</strong>
            <small>{{ t('portal.flowOverview.inventory',{kb:space.knowledge_base_count,files:space.file_count}) }}</small>
          </span>
          <span v-if="spacesFor(stage.key).length>4" class="more">{{ t('portal.flowOverview.moreSpaces',{count:spacesFor(stage.key).length-4}) }}</span>
        </button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PortalSpace, PortalStage } from '@/api/portal'

const props=defineProps<{stages:PortalStage[];spaces:PortalSpace[];selectedStage:string}>()
defineEmits<{select:[stage:string]}>()
const {t}=useI18n()
const spacesFor=(stage:string)=>props.spaces.filter(space=>space.stages.includes(stage))
const coveredStages=computed(()=>props.stages.filter(stage=>spacesFor(stage.key).length>0).length)
const coverageBackground=computed(()=>`conic-gradient(var(--td-brand-color) ${props.stages.length?coveredStages.value/props.stages.length*100:0}%,var(--td-bg-color-secondarycontainer) 0)`)
const knowledgeBaseCount=computed(()=>props.spaces.reduce((sum,space)=>sum+space.knowledge_base_count,0))
const fileCount=computed(()=>props.spaces.reduce((sum,space)=>sum+space.file_count,0))
</script>

<style scoped lang="less">
.flow-overview{overflow:hidden;border:1px solid var(--td-component-border);border-radius:18px;background:var(--td-bg-color-container);box-shadow:0 12px 36px rgba(15,23,42,.06)}
.flow-header{display:flex;align-items:center;justify-content:space-between;gap:24px;padding:24px 26px 18px}.flow-header>div>span{color:var(--td-brand-color);font-size:11px;font-weight:700;letter-spacing:.14em}.flow-header h2{margin:5px 0 7px;font-size:24px}.flow-header p{max-width:720px;margin:0;color:var(--td-text-color-secondary);line-height:1.6}.flow-actions{display:flex;align-items:center;gap:12px}.coverage{position:relative;isolation:isolate;flex:none;width:82px;height:82px;display:grid;place-content:center;border-radius:50%;text-align:center}.coverage::before{content:'';position:absolute;z-index:-1;inset:7px;border-radius:50%;background:var(--td-bg-color-container)}.coverage strong{font-size:20px}.coverage small{color:var(--td-text-color-secondary);font-size:10px}
.metrics{display:grid;grid-template-columns:repeat(4,1fr);margin:0 26px 22px;border:1px solid var(--td-component-stroke);border-radius:12px;background:var(--td-bg-color-secondarycontainer)}.metrics>div{padding:14px 18px;border-right:1px solid var(--td-component-stroke)}.metrics>div:last-child{border:0}.metrics strong,.metrics span{display:block}.metrics strong{font-size:22px}.metrics span{margin-top:3px;color:var(--td-text-color-secondary);font-size:12px}
.flow-scroll{overflow-x:auto;padding:2px 26px 26px}.flow-track{display:grid;grid-auto-flow:column;grid-auto-columns:minmax(165px,1fr);min-width:1190px;gap:10px}.stage-column{position:relative;min-height:260px;padding:16px 13px;border:1px solid var(--td-component-border);border-radius:14px;background:linear-gradient(180deg,var(--td-bg-color-container),var(--td-bg-color-secondarycontainer));color:var(--td-text-color-primary);cursor:pointer;text-align:left;transition:.18s ease}.stage-column:not(:last-child)::after{content:'›';position:absolute;z-index:2;right:-9px;top:25px;width:16px;height:22px;display:grid;place-items:center;border-radius:8px;background:var(--td-bg-color-container);color:var(--td-brand-color);font-size:18px;font-weight:700}.stage-column:hover,.stage-column.active{border-color:var(--td-brand-color);transform:translateY(-2px);box-shadow:0 9px 20px rgba(15,118,110,.12)}.stage-column.active{background:linear-gradient(180deg,var(--td-brand-color-light),var(--td-bg-color-container))}.stage-column.empty{opacity:.72}.phase{display:flex;align-items:center;gap:8px}.phase i{width:28px;height:28px;display:grid;place-items:center;border-radius:8px;background:var(--td-brand-color);color:#fff;font-size:11px;font-style:normal;font-weight:700}.phase>span{font-size:14px;font-weight:700}.stage-description{display:block;min-height:32px;margin:8px 0;color:var(--td-text-color-secondary);line-height:1.45}.space-total{display:block;margin:10px 0 7px;color:var(--td-brand-color);font-size:11px;font-weight:600}.space-pill{display:block;margin-top:6px;padding:8px;border:1px solid var(--td-component-stroke);border-radius:8px;background:var(--td-bg-color-container)}.space-pill strong{display:block;overflow:hidden;font-size:12px;text-overflow:ellipsis;white-space:nowrap}.space-pill small{display:block;margin-top:3px;color:var(--td-text-color-placeholder);font-size:10px}.empty-label,.more{display:block;margin-top:14px;color:var(--td-text-color-placeholder);font-size:11px}.more{color:var(--td-brand-color)}
@media(max-width:680px){.flow-header{align-items:flex-start;padding:20px;}.coverage{width:66px;height:66px}.metrics{grid-template-columns:repeat(2,1fr);margin:0 20px 18px}.metrics>div:nth-child(2){border-right:0}.metrics>div:nth-child(-n+2){border-bottom:1px solid var(--td-component-stroke)}.flow-scroll{padding:2px 20px 20px}}
</style>
