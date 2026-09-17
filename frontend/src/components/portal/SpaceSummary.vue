<template>
  <article v-if="loading" class="space-summary is-loading" :class="variant">
    <t-skeleton animation="gradient" :row-col="[{ width: '62%', height: '18px' }, { width: '88%', height: '14px' }, { width: '48%', height: '14px' }]" />
  </article>
  <article v-else-if="space" class="space-summary" :class="[variant, space.access_state, { current: isActiveSpace }]"
    role="button" tabindex="0" :aria-label="cardLabel" @click="activate" @keydown.enter.prevent="activate" @keydown.space.prevent="activate">
    <div class="summary-main">
      <div v-if="stageNumber && stageName" class="stage-identity" :class="{ 'is-redundant': repeatsSpaceName }"><b>{{ stageNumber }}</b><span aria-hidden="true">·</span>{{ displayStageName }}</div>
      <div class="space-title" :class="{ 'is-title-hidden': hideTitle }"><span v-if="variant !== 'flat'" class="space-icon"><t-icon :name="visualIcon" /><i v-if="space.access_state === 'discoverable'"><t-icon name="lock-on" /></i></span><strong v-if="!hideTitle" :title="space.display_name">{{ space.display_name }}</strong><span v-show="isActiveSpace">{{ t('spaceSwitcher.current') }}</span></div>
      <p :title="displayDescription">{{ displayDescription }}</p>
      <t-tooltip v-if="variant === 'compact'" :content="responsibleLabel"><span class="space-responsible is-compact"><t-icon name="user" />{{ displayResponsible }}</span></t-tooltip>
      <span v-else class="space-responsible"><t-icon name="user" />{{ responsibleLabel }}</span>
      <small>{{ t('portalMap.spaceInventory', { kb: formatCount(space.knowledge_base_count), files: formatCount(space.file_count) }) }}</small>
    </div>
    <div v-if="space.access_state === 'accessible'" class="accessible-actions" @click.stop @keydown.stop>
      <div class="icon-actions">
        <t-tooltip :content="actionLabel('ask')"><button type="button" :title="actionLabel('ask')" :aria-label="actionLabel('ask')" @click="$emit('ask', space)"><t-icon name="chat" /></button></t-tooltip>
        <t-tooltip :content="actionLabel('search')"><button type="button" :title="actionLabel('search')" :aria-label="actionLabel('search')" @click="$emit('search', space)"><t-icon name="search" /></button></t-tooltip>
      </div>
      <t-tooltip :content="actionLabel('enter')"><button class="enter-action" type="button" :title="actionLabel('enter')" :aria-label="actionLabel('enter')" @click="$emit('enter', space)"><span>{{ t('portalMap.enter') }}</span><t-icon name="chevron-right" /></button></t-tooltip>
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
  variant?: 'compact' | 'normal' | 'flat'
  loading?: boolean
  isActiveSpace?: boolean
  stageNumber?: string
  stageName?: string
  fallbackDescription?: string
  visualIcon?: string
  hideTitle?: boolean
  suppressDescription?: boolean
}>(), { variant: 'normal', loading: false, isActiveSpace: false, stageNumber: '', stageName: '', fallbackDescription: '', visualIcon: 'folder', hideTitle: false, suppressDescription: false })
const emit = defineEmits<{ enter: [space: PortalSpace]; restricted: [space: PortalSpace]; search: [space: PortalSpace]; ask: [space: PortalSpace] }>()
const { t } = useI18n()
const numberFormatter = new Intl.NumberFormat()
const formatCount=(value:unknown)=>numberFormatter.format(typeof value==='number'&&Number.isFinite(value)&&value>0?value:0)
const displayDescription=computed(()=>props.suppressDescription?props.fallbackDescription.trim()||t('portalCard.noSpaceDescription'):props.space?.description?.trim()||props.fallbackDescription.trim()||t('portalCard.noSpaceDescription'))
const displayResponsible=computed(()=>props.space?.responsible_team?.trim()||t('portalCard.unconfigured'))
const responsibleLabel=computed(()=>t('portalCard.responsible',{name:displayResponsible.value}))
const repeatsSpaceName=computed(()=>props.stageName.trim().toLocaleLowerCase()===props.space?.display_name?.trim().toLocaleLowerCase())
const displayStageName=computed(()=>repeatsSpaceName.value?t('portalMap.stageIdentity',{name:props.stageName}):props.stageName)
const cardLabel=computed(()=>props.space?`${props.space.display_name} · ${props.space.access_state==='accessible'?t('portalMap.enterSpace'):restrictedAction.value}`:'')
const restrictedAction=computed(()=>props.space?.access_request_pending?t('portal.pending'):props.space?.can_request_access?t('portal.requestAccess'):t('portalMap.getAccess'))
function actionLabel(action:'enter'|'search'|'ask'){
  if(!props.space)return ''
  const actionKey=action==='enter'?'portalMap.enterSpace':action==='search'?'portalCard.search':'portalCard.ask'
  return action==='enter'?`${props.space.display_name} · ${t(actionKey)}`:t(actionKey)
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
.space-icon{position:relative}.space-icon i{position:absolute;right:-5px;bottom:-5px;width:16px;height:16px;display:grid;place-items:center;border:1px solid var(--portal-line);border-radius:50%;background:var(--td-bg-color-container);color:var(--portal-text-secondary);font-size:12px;font-style:normal}.space-summary.normal .icon-actions button{width:auto;gap:5px;padding:0 7px;font-size:13px}.space-summary.normal .icon-actions button>.t-icon{font-size:16px}
.space-summary.flat{min-height:112px;padding:0;border:0;border-radius:0;background:transparent;box-shadow:none}.space-summary.flat:hover,.space-summary.flat:focus-within{border-color:transparent;background:transparent;box-shadow:none}.space-summary.flat:focus-visible{border-color:transparent;box-shadow:none}.space-summary.flat .space-title.is-title-hidden{justify-content:flex-end}.space-summary.flat .icon-actions button{width:auto;gap:5px;padding:0 7px;font-size:13px}.space-summary.flat .icon-actions button>.t-icon{font-size:16px}
.space-summary{min-height:166px}.summary-main p{display:-webkit-box;overflow:hidden;white-space:normal;text-overflow:clip;-webkit-box-orient:vertical;-webkit-line-clamp:2}.space-responsible{display:flex;min-width:0;align-items:center;gap:5px;margin-top:4px;overflow:hidden;color:var(--portal-text-muted);font-size:12.5px;line-height:18px;text-overflow:ellipsis;white-space:nowrap}.space-responsible>.t-icon{flex:none;font-size:14px}.summary-main small{margin-top:3px}.accessible-actions,.space-summary.compact .accessible-actions,.restricted-action,.space-summary.compact .restricted-action{margin-top:auto}.icon-actions{opacity:0;pointer-events:none;transition:opacity .15s ease}.space-summary:hover .icon-actions,.space-summary:focus-within .icon-actions{opacity:1;pointer-events:auto}.space-summary.normal .icon-actions button,.space-summary.flat .icon-actions button{width:28px;gap:0;padding:0;font-size:16px}.space-summary.flat{height:100%;min-height:130px}
@media(pointer:coarse){.icon-actions{opacity:1;pointer-events:auto}}
</style>
