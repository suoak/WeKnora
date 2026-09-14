<template>
  <article v-if="loading" class="space-summary is-loading" :class="variant">
    <t-skeleton animation="gradient" :row-col="[{ width: '62%', height: '14px' }, { width: '88%', height: '12px' }]" />
  </article>
  <article v-else-if="space" class="space-summary" :class="[variant, space.access_state]"
    role="button" tabindex="0" @click="activate" @keydown.enter.prevent="activate" @keydown.space.prevent="activate">
    <div class="summary-main">
      <strong :title="space.display_name">{{ space.display_name }}</strong>
      <p v-if="variant === 'normal'">{{ space.description || t('portal.noDescription') }}</p>
      <small>{{ t(variant === 'compact' ? 'portalMap.spaceInventoryCompact' : 'portalMap.spaceInventory', { kb: numberFormatter.format(space.knowledge_base_count), files: numberFormatter.format(space.file_count) }) }}</small>
      <span v-if="space.access_state === 'discoverable'" class="permission"><t-icon name="lock-on" />{{ t('portalMap.noAccess') }}</span>
    </div>
    <div v-if="space.access_state === 'accessible'" class="accessible-actions" @click.stop @keydown.stop>
      <t-tooltip :content="actionLabel('enter')"><button type="button" :aria-label="actionLabel('enter')" @click="$emit('enter', space)"><t-icon name="chevron-right" /></button></t-tooltip>
      <t-tooltip :content="actionLabel('search')"><button class="space-action--secondary" type="button" :aria-label="actionLabel('search')" @click="$emit('search', space)"><t-icon name="search" /></button></t-tooltip>
      <t-tooltip :content="actionLabel('ask')"><button class="space-action--secondary" type="button" :aria-label="actionLabel('ask')" @click="$emit('ask', space)"><t-icon name="chat" /></button></t-tooltip>
    </div>
    <t-icon v-else class="lock" name="lock-on" />
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
.space-summary{position:relative;display:flex;min-width:0;align-items:flex-start;gap:8px;padding:9px;border:1px solid var(--td-component-stroke);border-radius:8px;background:var(--td-bg-color-container);color:var(--td-text-color-primary);cursor:pointer;transition:border-color .16s ease,background .16s ease}.space-summary:hover,.space-summary:focus-visible{border-color:var(--td-component-border);background:var(--td-bg-color-container-hover);outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.space-summary.normal{min-height:122px;padding:14px;border-radius:10px}.space-summary.discoverable{color:var(--td-text-color-secondary)}.summary-main{min-width:0;flex:1;padding-right:22px}.summary-main strong{display:block;overflow:hidden;font-size:12px;font-weight:600;text-overflow:ellipsis;white-space:nowrap}.normal .summary-main strong{font-size:14px}.summary-main p{display:-webkit-box;min-height:36px;margin:6px 0 10px;overflow:hidden;color:var(--td-text-color-secondary);font-size:12px;line-height:1.5;-webkit-box-orient:vertical;-webkit-line-clamp:2}.summary-main small{display:block;max-width:100%;margin-top:4px;overflow:hidden;color:var(--td-text-color-placeholder);font-size:10px;text-overflow:ellipsis;white-space:nowrap}.normal .summary-main small{font-size:11px}.permission{display:flex;align-items:center;gap:4px;margin-top:5px;color:var(--td-text-color-placeholder);font-size:10px}.lock{flex:none;color:var(--td-text-color-placeholder)}.accessible-actions{position:absolute;top:6px;right:6px;display:flex;flex-direction:row-reverse;gap:1px;border-radius:7px;background:linear-gradient(90deg,transparent,var(--td-bg-color-container) 16%)}.accessible-actions button{width:24px;height:24px;display:grid;place-items:center;padding:0;border:0;border-radius:6px;background:transparent;color:var(--td-text-color-placeholder);cursor:pointer}.accessible-actions .space-action--secondary{display:none}.space-summary:hover .space-action--secondary,.space-summary:focus-within .space-action--secondary{display:grid}.accessible-actions button:hover,.accessible-actions button:focus-visible{background:var(--td-brand-color-light);color:var(--td-brand-color);outline:2px solid var(--td-brand-color-focus);outline-offset:1px}.is-loading{cursor:default}.is-loading .summary-main{padding-right:0}
</style>
