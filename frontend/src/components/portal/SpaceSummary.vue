<template>
  <article v-if="loading" class="space-summary is-loading" :class="variant">
    <t-skeleton animation="gradient" :row-col="[{ width: '62%', height: '14px' }, { width: '88%', height: '12px' }]" />
  </article>
  <article v-else-if="space" class="space-summary" :class="[variant, space.access_state]"
    role="button" tabindex="0" @click="activate" @keydown.enter.prevent="activate" @keydown.space.prevent="activate">
    <div class="summary-main">
      <div class="space-title"><t-icon v-if="space.access_state === 'discoverable'" name="lock-on" /><strong :title="space.display_name">{{ space.display_name }}</strong></div>
      <p v-if="variant === 'normal'">{{ space.description || t('portal.noDescription') }}</p>
      <small>{{ t(variant === 'compact' ? 'portalMap.spaceInventoryCompact' : 'portalMap.spaceInventory', { kb: numberFormatter.format(space.knowledge_base_count), files: numberFormatter.format(space.file_count) }) }}</small>
    </div>
    <div v-if="space.access_state === 'accessible'" class="accessible-actions" @click.stop @keydown.stop>
      <t-tooltip :content="actionLabel('ask')"><button type="button" :aria-label="actionLabel('ask')" @click="$emit('ask', space)"><t-icon name="chat" /><span>{{ t('portalMap.ask') }}</span></button></t-tooltip>
      <t-tooltip :content="actionLabel('search')"><button type="button" :aria-label="actionLabel('search')" @click="$emit('search', space)"><t-icon name="search" /><span>{{ t('portalMap.search') }}</span></button></t-tooltip>
      <t-tooltip :content="actionLabel('enter')"><button class="enter-action" type="button" :aria-label="actionLabel('enter')" @click="$emit('enter', space)"><span>{{ t('portalMap.enter') }}</span><t-icon name="chevron-right" /></button></t-tooltip>
    </div>
    <div v-else class="restricted-action"><span>{{ space.access_request_pending ? t('portalMap.accessDialog.pending') : t('portalMap.permissionRequired') }}</span><strong>{{ t(space.can_request_access ? 'portalMap.getAccess' : 'portalMap.learnAccess') }} <t-icon name="chevron-right" /></strong></div>
  </article>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PortalSpace } from '@/api/portal'

const props = withDefaults(defineProps<{
  space?: PortalSpace
  variant?: 'compact' | 'normal'
  loading?: boolean
  isActiveSpace?: boolean
}>(), { variant: 'normal', loading: false, isActiveSpace: false })
const emit = defineEmits<{ enter: [space: PortalSpace]; restricted: [space: PortalSpace]; search: [space: PortalSpace]; ask: [space: PortalSpace] }>()
const { t } = useI18n()
const numberFormatter = new Intl.NumberFormat()
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
.space-summary{position:relative;display:flex;min-width:0;flex-direction:column;gap:8px;padding:9px;border:1px solid var(--td-component-stroke);border-radius:8px;background:var(--td-bg-color-container);color:var(--td-text-color-primary);cursor:pointer;transition:border-color .16s ease,background .16s ease}.space-summary:hover,.space-summary:focus-visible,.space-summary:focus-within{border-color:color-mix(in srgb,var(--td-brand-color) 38%,var(--td-component-border));background:var(--td-bg-color-container-hover);outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.space-summary.normal{min-height:132px;padding:14px;border-radius:10px}.space-summary.discoverable{background:color-mix(in srgb,var(--td-bg-color-secondarycontainer) 56%,var(--td-bg-color-container));color:var(--td-text-color-secondary)}.summary-main{min-width:0;width:100%;flex:1}.space-title{display:flex;min-width:0;align-items:center;gap:5px}.space-title>.t-icon{flex:none;color:var(--td-text-color-placeholder)}.summary-main strong{display:block;overflow:hidden;font-size:12px;font-weight:650;text-overflow:ellipsis;white-space:nowrap}.normal .summary-main strong{font-size:14px}.summary-main p{display:-webkit-box;min-height:36px;margin:5px 0 9px;overflow:hidden;color:var(--td-text-color-secondary);font-size:11px;line-height:1.5;-webkit-box-orient:vertical;-webkit-line-clamp:2}.summary-main small{display:block;max-width:100%;margin-top:4px;overflow:hidden;color:var(--td-text-color-placeholder);font-size:9px;text-overflow:ellipsis;white-space:nowrap}.normal .summary-main small{font-size:11px}.accessible-actions{display:flex;min-width:0;align-items:center;gap:2px;padding-top:6px;border-top:1px solid var(--td-component-stroke)}.accessible-actions button{height:24px;display:flex;min-width:0;align-items:center;justify-content:center;gap:3px;padding:0 4px;border:0;border-radius:5px;background:transparent;color:var(--td-text-color-secondary);cursor:pointer;font:9px var(--td-font-family);white-space:nowrap}.accessible-actions .enter-action{margin-left:auto;color:var(--td-brand-color);font-weight:600}.normal .accessible-actions{gap:7px}.normal .accessible-actions button{padding:0 6px;font-size:11px}.accessible-actions button:hover,.accessible-actions button:focus-visible{background:var(--td-brand-color-light);color:var(--td-brand-color);outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.restricted-action{display:flex;align-items:center;justify-content:space-between;gap:5px;padding-top:6px;border-top:1px solid var(--td-component-stroke);color:var(--td-text-color-placeholder);font-size:9px}.restricted-action strong{display:flex;align-items:center;color:var(--td-text-color-secondary);font-weight:600;white-space:nowrap}.normal .restricted-action{font-size:11px}.is-loading{cursor:default}
@media(max-width:900px){.accessible-actions button{min-height:28px}.normal .accessible-actions{justify-content:space-between}.normal .accessible-actions .enter-action{margin-left:0}}
</style>
