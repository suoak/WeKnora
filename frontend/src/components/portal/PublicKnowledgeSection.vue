<template>
  <section class="public-knowledge">
    <header>
      <div class="public-title"><span class="public-title__icon"><t-icon name="earth" /></span><div><h2>{{ t('portal.publicZone.title') }}</h2><p>{{ t('portal.views.publicDescription') }}</p></div></div>
      <span v-if="!loading && !error">{{ t('portal.spaceCount', { count: publicSpaces.length }) }}</span>
      <t-skeleton v-else-if="loading" class="summary-loading" animation="gradient" :row-col="[{ width: '120px', height: '16px' }]" />
    </header>
    <PortalSectionState v-if="error" @retry="$emit('retry')" />
    <div v-else-if="loading" class="public-grid">
      <SpaceSummary v-for="item in 4" :key="item" loading variant="normal" />
    </div>
    <div v-else-if="publicSpaces.length" class="public-grid">
      <SpaceSummary v-for="space in publicSpaces" :key="space.tenant_id" :space="space" variant="normal"
        :visual-icon="resolvePublicSpaceIcon(space)"
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
import { resolvePublicSpaceIcon } from '@/config/portalVisualIcons'
import SpaceSummary from './SpaceSummary.vue'
import PortalSectionState from './PortalSectionState.vue'

const props=defineProps<{spaces:PortalSpace[];stageKeys:string[];loading:boolean;error:boolean;activeTenantId:number}>()
defineEmits<{enter:[space:PortalSpace];restricted:[space:PortalSpace];search:[space:PortalSpace];ask:[space:PortalSpace];retry:[]}>()
const {t}=useI18n()
const publicSpaces=computed(()=>{
  const lifecycleStages=new Set(props.stageKeys)
  return props.spaces.filter(space=>!space.stages.some(stage=>lifecycleStages.has(stage)))
})
</script>

<style scoped lang="less">
.public-knowledge{margin-top:20px;padding:22px 24px 25px;border:1px solid var(--portal-line);border-radius:14px;background:var(--td-bg-color-container);box-shadow:0 5px 18px rgba(15,23,42,.035)}header{display:flex;align-items:center;justify-content:space-between;gap:24px;margin-bottom:15px}header>div{min-width:0}.public-title{display:flex;align-items:center;gap:11px}.public-title__icon{width:34px;height:34px;display:grid;flex:none;place-items:center;border:1px solid color-mix(in srgb,var(--td-brand-color) 18%,var(--portal-line));border-radius:9px;background:var(--portal-brand-surface);color:var(--td-brand-color-active);font-size:18px}h2{margin:0;color:var(--portal-text-primary);font-size:var(--portal-section-title);font-weight:700;line-height:1.35}header p{margin:3px 0 0;color:var(--portal-text-muted);font-size:12.5px;line-height:18px}header>span{flex:none;padding:4px 8px;border-radius:6px;background:var(--portal-surface-soft);color:var(--portal-text-muted);font-size:12.5px;font-weight:550}.summary-loading{width:120px;flex:none}.public-grid{display:grid;grid-template-columns:repeat(4,minmax(240px,1fr));gap:14px}.public-empty{min-height:88px;display:grid;place-items:center;color:var(--portal-text-muted);font-size:13px}
@media(max-width:1499px){.public-grid{grid-template-columns:repeat(auto-fit,minmax(240px,1fr))}}
@media(max-width:900px){header{align-items:flex-start;flex-direction:column;gap:6px}.summary-loading{width:100%}.public-grid{grid-template-columns:1fr}}
</style>
