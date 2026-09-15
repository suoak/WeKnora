<template>
  <article v-if="loading" class="space-summary is-loading" :class="variant">
    <t-skeleton animation="gradient" :row-col="[{ width: '62%', height: '18px' }, { width: '88%', height: '14px' }, { width: '48%', height: '14px' }]" />
  </article>
  <article v-else-if="space" class="space-summary" :class="[variant, space.access_state, { current: isActiveSpace }]"
    role="button" tabindex="0" :aria-label="cardLabel" @click="activate" @keydown.enter.prevent="activate" @keydown.space.prevent="activate">
    <div class="summary-main">
      <div v-if="stageNumber && stageName" class="stage-identity"><b>{{ stageNumber }}</b><span aria-hidden="true">·</span>{{ stageName }}</div>
      <div class="space-title"><t-icon v-if="space.access_state === 'discoverable'" name="lock-on" /><strong :title="space.display_name">{{ space.display_name }}</strong><span v-show="isActiveSpace">{{ t('spaceSwitcher.current') }}</span></div>
      <p>{{ space.description || t('portal.noDescription') }}</p>
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
}>(), { variant: 'normal', loading: false, isActiveSpace: false, stageNumber: '', stageName: '' })
const emit = defineEmits<{ enter: [space: PortalSpace]; restricted: [space: PortalSpace]; search: [space: PortalSpace]; ask: [space: PortalSpace] }>()
const { t } = useI18n()
const numberFormatter = new Intl.NumberFormat()
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
.space-summary{position:relative;display:flex;box-sizing:border-box;min-width:0;min-height:146px;flex-direction:column;padding:13px 14px;border:1px solid var(--portal-line);border-radius:11px;background:var(--td-bg-color-container);color:var(--portal-text-primary);box-shadow:0 1px 2px rgba(15,23,42,.025);cursor:pointer;transition:border-color .16s ease,background-color .16s ease,box-shadow .16s ease}.space-summary:hover,.space-summary:focus-visible,.space-summary:focus-within{border-color:color-mix(in srgb,var(--td-brand-color) 38%,var(--portal-line-strong));box-shadow:0 3px 10px rgba(15,23,42,.045);outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.space-summary.current{border-color:color-mix(in srgb,var(--td-brand-color) 45%,var(--portal-line))}.space-summary.discoverable{background:color-mix(in srgb,var(--portal-surface-soft) 72%,var(--td-bg-color-container));color:var(--portal-text-secondary)}.summary-main{min-width:0;width:100%;flex:1}.stage-identity{display:flex;align-items:center;gap:5px;margin-bottom:2px;color:var(--portal-text-muted);font-size:12px;font-weight:600;line-height:18px}.stage-identity b{color:var(--td-brand-color-active);font-weight:700}.space-title{display:flex;min-width:0;align-items:center;gap:7px}.space-title>.t-icon{flex:none;color:var(--portal-text-muted);font-size:16px}.space-title strong{display:block;min-width:0;overflow:hidden;font-size:15px;font-weight:650;line-height:21px;text-overflow:ellipsis;white-space:nowrap}.space-title span{flex:none;padding:2px 6px;border-radius:999px;background:var(--portal-brand-surface);color:var(--td-brand-color-active);font-size:12px;font-weight:600}.summary-main p{overflow:hidden;margin:4px 0 3px;color:var(--portal-text-secondary);font-size:13px;font-weight:400;line-height:20px;text-overflow:ellipsis;white-space:nowrap}.summary-main small{display:block;color:var(--portal-text-muted);font-size:12px;font-weight:500;line-height:18px}.accessible-actions{display:flex;align-items:center;justify-content:space-between;margin-top:6px}.icon-actions{display:flex;gap:5px}.accessible-actions button{height:28px;display:flex;align-items:center;justify-content:center;border:0;border-radius:7px;background:transparent;color:var(--portal-text-secondary);cursor:pointer;font:13px var(--portal-font-family)}.icon-actions button{width:28px;padding:0;font-size:17px}.accessible-actions .enter-action{gap:4px;padding:0 7px;color:var(--td-brand-color-active);font-weight:650}.accessible-actions button:hover,.accessible-actions button:focus-visible{background:var(--portal-brand-surface);color:var(--td-brand-color-active);outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.restricted-action{display:flex;justify-content:flex-end;margin-top:6px}.restricted-action strong{display:flex;height:28px;align-items:center;gap:3px;color:var(--portal-text-secondary);font-size:13px;font-weight:650}.space-summary.discoverable:hover .restricted-action strong,.space-summary.discoverable:focus-visible .restricted-action strong{color:var(--portal-text-primary)}.is-loading{cursor:default}
</style>
