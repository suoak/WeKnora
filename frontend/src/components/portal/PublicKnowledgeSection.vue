<template>
  <section class="public-knowledge">
    <header><div><span class="section-kicker">{{ t('portal.publicZone.eyebrow') }}</span><h2>{{ t('portal.publicZone.title') }}</h2></div><span v-if="!loading && !error">{{ t('portalMap.stageInventory', { spaces: stats.spaces, kb: stats.knowledgeBases, files: numberFormatter.format(stats.files) }) }}</span><t-skeleton v-else-if="loading" class="summary-loading" animation="gradient" :row-col="[{ width: '180px', height: '16px' }]" /></header>
    <PortalSectionState v-if="error" @retry="$emit('retry')" />
    <div v-else class="public-surface">
      <div v-if="loading" class="public-grid"><SpaceSummary v-for="item in 2" :key="item" loading variant="normal" /></div>
      <div v-else-if="publicSpaces.length" class="public-grid">
        <SpaceSummary v-for="space in publicSpaces" :key="space.tenant_id" :space="space" variant="normal"
          :is-active-space="space.tenant_id === activeTenantId" @enter="$emit('enter',$event)"
          @restricted="$emit('restricted',$event)" @search="$emit('search',$event)" @ask="$emit('ask',$event)" />
      </div>
      <div v-else class="public-empty">{{ t('portal.publicZone.empty') }}</div>
      <aside><span><t-icon name="book-open" /></span><p>{{ t('portal.publicZone.description') }}</p></aside>
    </div>
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
.public-knowledge{margin-top:30px;padding-top:26px;border-top:1px solid var(--portal-line)}header{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;margin-bottom:14px}.section-kicker{display:block;margin-bottom:3px;color:var(--td-brand-color-active);font-size:12px;font-weight:650;letter-spacing:.06em}h2{margin:0;color:var(--portal-text-primary);font-size:var(--portal-section-title);font-weight:700;line-height:1.35}header>span{color:var(--portal-text-muted);font-size:12px;font-weight:500}.summary-loading{width:180px;flex:none}.public-surface{display:grid;grid-template-columns:minmax(580px,740px) minmax(240px,1fr);gap:28px;padding:20px;border-radius:12px;background:var(--portal-surface-soft)}.public-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,360px));gap:12px}.public-empty{min-height:120px;display:grid;place-items:center;color:var(--portal-text-muted);font-size:13px}.public-surface aside{display:flex;max-width:420px;align-items:flex-start;gap:12px;justify-self:end;padding:12px}.public-surface aside>span{width:38px;height:38px;display:grid;flex:none;place-items:center;border-radius:10px;background:var(--portal-brand-surface);color:var(--td-brand-color-active);font-size:20px}.public-surface aside p{margin:0;color:var(--portal-text-secondary);font-size:13px;line-height:1.7}
@media(max-width:1180px){.public-surface{grid-template-columns:1fr}.public-surface aside{max-width:none;justify-self:start;padding:4px 0}}
@media(max-width:900px){header{align-items:flex-start;flex-direction:column;gap:6px}.summary-loading{width:100%}.public-surface{padding:16px}.public-grid{grid-template-columns:1fr}}
</style>
