<template>
  <section class="public-knowledge">
    <header>
      <h2>{{ t('portal.publicZone.title') }}</h2>
      <span v-if="!loading && !error">{{ t('portal.spaceCount', { count: publicSpaces.length }) }}</span>
      <t-skeleton v-else-if="loading" class="summary-loading" animation="gradient" :row-col="[{ width: '120px', height: '16px' }]" />
    </header>
    <PortalSectionState v-if="error" @retry="$emit('retry')" />
    <div v-else-if="loading" class="public-grid">
      <SpaceSummary v-for="item in 4" :key="item" loading variant="normal" />
    </div>
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
import SpaceSummary from './SpaceSummary.vue'
import PortalSectionState from './PortalSectionState.vue'

const props=defineProps<{spaces:PortalSpace[];loading:boolean;error:boolean;activeTenantId:number}>()
defineEmits<{enter:[space:PortalSpace];restricted:[space:PortalSpace];search:[space:PortalSpace];ask:[space:PortalSpace];retry:[]}>()
const {t}=useI18n()
const publicSpaces=computed(()=>props.spaces.filter(space=>space.category===PUBLIC_KNOWLEDGE_CATEGORY))
</script>

<style scoped lang="less">
.public-knowledge{margin-top:28px;padding-top:24px;border-top:1px solid var(--portal-line)}header{display:flex;align-items:center;justify-content:space-between;gap:24px;margin-bottom:12px}h2{margin:0;color:var(--portal-text-primary);font-size:var(--portal-section-title);font-weight:700;line-height:1.35}header>span{color:var(--portal-text-muted);font-size:12px;font-weight:500}.summary-loading{width:120px;flex:none}.public-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px}.public-empty{min-height:120px;display:grid;place-items:center;color:var(--portal-text-muted);font-size:13px}
@media(max-width:1399px){.public-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:900px){header{align-items:flex-start;flex-direction:column;gap:6px}.summary-loading{width:100%}.public-grid{grid-template-columns:1fr}}
</style>
