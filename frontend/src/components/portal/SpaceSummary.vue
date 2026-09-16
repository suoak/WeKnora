<template>
  <article v-if="loading" class="space-summary is-loading" :class="variant">
    <t-skeleton animation="gradient" :row-col="[{ width: '62%', height: '18px' }, { width: '88%', height: '14px' }, { width: '48%', height: '14px' }]" />
  </article>
  <article v-else-if="space" class="space-summary" :class="[variant, space.access_state, { current: isActiveSpace }]"
    role="button" tabindex="0" :aria-label="cardLabel" @click="activate" @keydown.enter.prevent="activate" @keydown.space.prevent="activate">
    <div class="summary-main">
      <div v-if="stageNumber && stageName" class="stage-identity" :class="{ 'is-redundant': repeatsSpaceName }"><b>{{ stageNumber }}</b><span aria-hidden="true">·</span>{{ displayStageName }}</div>
      <div class="space-title"><span class="space-icon"><t-icon :name="space.access_state === 'discoverable' ? 'lock-on' : 'folder'" /></span><strong :title="space.display_name">{{ space.display_name }}</strong><span v-show="isActiveSpace">{{ t('spaceSwitcher.current') }}</span></div>
      <p v-if="displayDescription">{{ displayDescription }}</p>
      <small>{{ t('portalMap.spaceInventory', { kb: numberFormatter.format(space.knowledge_base_count), files: numberFormatter.format(space.file_count) }) }}</small>
    </div>
    <div v-if="space.access_state === 'accessible'" class="accessible-actions" @click.stop @keydown.stop>
      <div class="icon-actions">
        <t-tooltip :content="actionLabel('ask')"><button type="button" :aria-label="actionLabel('ask')" @click="$emit('ask', space)"><t-icon name="chat" /></button></t-tooltip>
        <t-tooltip :content="actionLabel('search')"><button type="button" :aria-label="actionLabel('search')" @click="$emit('search', space)"><t-icon name="search" /></button></t-tooltip>
      </div>
      <t-tooltip :content="actionLabel('enter')"><button class="enter-action" type="button" :aria-label="actionLabel('enter')" @click="$emit('enter', space)"><span>{{ t('portalMap.enter') }}</span><t-icon name="chevron-right" /></button></t-tooltip>
    </div>
    <div v-else class="restricted-action"><strong>{{ restrictedAction }} <t-icon name="chevron-right" /></strong></div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PortalSpace } from '@/api/portal'

const props = withDefaults(defineProps<{
  space?: PortalSpace
  variant?: 'compact' | 'normal'
  loading?: boolean
  isActiveSpace?: boolean
  stageNumber?: string
  stageName?: string
  fallbackDescription?: string
}>(), { variant: 'normal', loading: false, isActiveSpace: false, stageNumber: '', stageName: '', fallbackDescription: '' })
const emit = defineEmits<{ enter: [space: PortalSpace]; restricted: [space: PortalSpace]; search: [space: PortalSpace]; ask: [space: PortalSpace] }>()
const { t } = useI18n()
const numberFormatter = new Intl.NumberFormat()
const displayDescription=computed(()=>props.space?.description?.trim()||props.fallbackDescription.trim())
const repeatsSpaceName=computed(()=>props.stageName.trim().toLocaleLowerCase()===props.space?.display_name?.trim().toLocaleLowerCase())
const displayStageName=computed(()=>repeatsSpaceName.value?t('portalMap.stageIdentity',{name:props.stageName}):props.stageName)
const cardLabel=computed(()=>props.space?`${props.space.display_name} · ${props.space.access_state==='accessible'?t('portalMap.enterSpace'):restrictedAction.value}`:'')
const restrictedAction=computed(()=>props.space?.access_request_pending?t('portal.pending'):props.space?.can_request_access?t('portal.requestAccess'):t('portalMap.getAccess'))
function actionLabel(action:'enter'|'search'|'ask'){
  if(!props.space)return ''
  const actionKey=action==='enter'?'portalMap.enterSpace':action==='search'?'portalExperience.quickActions.search':'portalMap.ask'
  return `${props.space.display_name} · ${t(actionKey)}`
}
function activate(){
  if(!props.space)return
  if(props.space.access_state === 'accessible')emit('enter',props.space)
  else emit('restricted',props.space)
}
</script>

<style scoped lang="less">
.space-summary{position:relative;display:flex;box-sizing:border-box;min-width:0;min-height:150px;flex-direction:column;padding:16px 17px 13px;border:1px solid var(--portal-line);border-radius:13px;background:color-mix(in srgb,var(--td-bg-color-container) 98%,transparent);color:var(--portal-text-primary);box-shadow:0 2px 8px rgba(15,23,42,.035);cursor:pointer;transition:border-color .16s ease,background-color .16s ease,box-shadow .16s ease}.space-summary:hover,.space-summary:focus-visible,.space-summary:focus-within{border-color:color-mix(in srgb,var(--td-brand-color) 42%,var(--portal-line-strong));box-shadow:0 6px 17px rgba(15,23,42,.065);outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.space-summary.current{border-color:color-mix(in srgb,var(--td-brand-color) 42%,var(--portal-line));background:color-mix(in srgb,var(--portal-brand-surface) 42%,var(--td-bg-color-container))}.space-summary.discoverable{background:color-mix(in srgb,var(--portal-surface-soft) 72%,var(--td-bg-color-container));color:var(--portal-text-secondary)}.summary-main{min-width:0;width:100%;flex:1}.stage-identity{display:flex;align-items:center;gap:5px;margin-bottom:5px;color:var(--portal-text-muted);font-size:12.5px;font-weight:600;line-height:18px}.stage-identity b{color:var(--td-brand-color-active);font-weight:700}.stage-identity.is-redundant{font-weight:500}.stage-identity.is-redundant b{color:var(--portal-text-muted)}.space-title{display:flex;min-width:0;align-items:center;gap:9px}.space-icon{width:27px;height:27px;display:grid!important;flex:none!important;place-items:center;padding:0!important;border:1px solid color-mix(in srgb,var(--td-brand-color) 14%,var(--portal-line));border-radius:8px!important;background:color-mix(in srgb,var(--portal-brand-surface) 72%,var(--td-bg-color-container))!important;color:var(--td-brand-color-active)!important;font-size:15px!important}.space-title strong{display:block;min-width:0;overflow:hidden;font-size:16px;font-weight:680;line-height:22px;text-overflow:ellipsis;white-space:nowrap}.space-title>span:last-child{flex:none;padding:2px 7px;border-radius:999px;background:var(--portal-brand-surface);color:var(--td-brand-color-active);font-size:12.5px;font-weight:650}.summary-main p{overflow:hidden;margin:7px 0 3px;color:var(--portal-text-secondary);font-size:13.5px;font-weight:400;line-height:20px;text-overflow:ellipsis;white-space:nowrap}.summary-main small{display:block;margin-top:4px;color:var(--portal-text-muted);font-size:12.5px;font-weight:500;line-height:18px}.accessible-actions{display:flex;align-items:center;justify-content:space-between;margin-top:10px;padding-top:8px;border-top:1px solid var(--portal-line)}.icon-actions{display:flex;gap:5px}.accessible-actions button{height:28px;display:flex;align-items:center;justify-content:center;border:0;border-radius:7px;background:transparent;color:var(--portal-text-secondary);cursor:pointer;font:13px var(--portal-font-family)}.icon-actions button{width:28px;padding:0;font-size:17px}.accessible-actions .enter-action{gap:4px;padding:0 7px;color:var(--td-brand-color-active);font-weight:650}.accessible-actions button:hover,.accessible-actions button:focus-visible{background:var(--portal-brand-surface);color:var(--td-brand-color-active);outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.restricted-action{display:flex;justify-content:flex-end;margin-top:10px;padding-top:8px;border-top:1px solid var(--portal-line)}.restricted-action strong{display:flex;height:28px;align-items:center;gap:3px;color:var(--portal-text-secondary);font-size:13px;font-weight:650}.space-summary.discoverable:hover .restricted-action strong,.space-summary.discoverable:focus-visible .restricted-action strong{color:var(--portal-text-primary)}.is-loading{cursor:default}
.space-summary.compact{min-height:138px;padding:13px 14px 11px;border-radius:10px}.space-summary.compact .space-title{align-items:flex-start}.space-summary.compact .space-title strong{display:-webkit-box;overflow:hidden;white-space:normal;text-overflow:clip;-webkit-box-orient:vertical;-webkit-line-clamp:2}.space-summary.compact .summary-main p{display:-webkit-box;overflow:hidden;margin-top:6px;white-space:normal;text-overflow:clip;-webkit-box-orient:vertical;-webkit-line-clamp:2}.space-summary.compact .accessible-actions,.space-summary.compact .restricted-action{margin-top:8px;padding-top:7px}
</style>
