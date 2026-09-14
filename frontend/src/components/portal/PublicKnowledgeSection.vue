<template>
  <section class="public-knowledge">
    <header><div><h2>{{ t('portal.publicZone.title') }}</h2><p>{{ t('portal.publicZone.description') }}</p></div><span v-if="!loading && !error">{{ t('portalMap.stageInventory', { spaces: stats.spaces, kb: stats.knowledgeBases, files: numberFormatter.format(stats.files) }) }}</span><t-skeleton v-else-if="loading" class="summary-loading" animation="gradient" :row-col="[{ width: '180px', height: '16px' }]" /></header>
    <PortalSectionState v-if="error" @retry="$emit('retry')" />
    <div v-else-if="loading" class="public-grid"><SpaceSummary v-for="item in 3" :key="item" loading variant="normal" /></div>
    <div v-else-if="publicSpaces.length" class="public-grid">
      <SpaceSummary v-for="space in publicSpaces" :key="space.tenant_id" :space="space" variant="normal"
        :is-active-space="space.tenant_id === activeTenantId" @enter="$emit('enter',$event)"
        @restricted="$emit('restricted',$event)" @search="$emit('search',$event)" @ask="$emit('ask',$event)" />
    </div>
    <div v-else class="public-empty">{{ t('portal.publicZone.empty') }}</div>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PortalSpace } from '@/api/portal'
import { PUBLIC_KNOWLEDGE_CATEGORY } from '@/config/publicKnowledgeSpaces'
import { summarizeKnowledgeSpaces } from '@/config/portalKnowledgeSummary'
import SpaceSummary from './SpaceSummary.vue'
import PortalSectionState from './PortalSectionState.vue'
const props=defineProps<{spaces:PortalSpace[];loading:boolean;error:boolean;activeTenantId:number}>()
defineEmits<{enter:[space:PortalSpace];restricted:[space:PortalSpace];search:[space:PortalSpace];ask:[space:PortalSpace];retry:[]}>()
const {t}=useI18n()
const numberFormatter=new Intl.NumberFormat()
const publicSpaces=computed(()=>props.spaces.filter(space=>space.category===PUBLIC_KNOWLEDGE_CATEGORY))
const stats=computed(()=>summarizeKnowledgeSpaces(publicSpaces.value))
</script>
<style scoped lang="less">
.public-knowledge{margin-top:24px;padding-top:22px;border-top:1px solid var(--td-component-stroke)}header{display:flex;align-items:flex-end;justify-content:space-between;gap:20px;margin-bottom:11px}h2{margin:0;font-size:18px;font-weight:600}header p{margin:4px 0 0;color:var(--td-text-color-secondary);font-size:12px}header>span{color:var(--td-text-color-placeholder);font-size:11px}.summary-loading{width:180px;flex:none}.public-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:9px}.public-empty{min-height:104px;display:grid;place-items:center;border:1px dashed var(--td-component-border);border-radius:10px;color:var(--td-text-color-placeholder);font-size:12px}@media(max-width:1599px){.public-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:900px){header{align-items:flex-start;flex-direction:column;gap:6px}.summary-loading{width:100%}.public-grid{grid-template-columns:1fr}}
</style>
