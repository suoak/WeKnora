<template>
  <article v-if="loading" class="space-summary is-loading" :class="variant">
    <t-skeleton animation="gradient" :row-col="[{ width: '62%', height: '14px' }, { width: '88%', height: '12px' }]" />
  </article>
  <article v-else-if="space" class="space-summary" :class="[variant, space.access_state]"
    role="button" tabindex="0" @click="activate" @keydown.enter.prevent="activate">
    <div class="summary-main">
      <strong>{{ space.display_name }}</strong>
      <p v-if="variant === 'normal'">{{ space.description || t('portal.noDescription') }}</p>
      <small>{{ t('portalMap.spaceInventory', { kb: numberFormatter.format(space.knowledge_base_count), files: numberFormatter.format(space.file_count) }) }}</small>
      <span v-if="space.access_state === 'discoverable'" class="permission"><t-icon name="lock-on" />{{ t('portalMap.noAccess') }}</span>
    </div>
    <div v-if="space.access_state === 'accessible'" class="accessible-actions" @click.stop>
      <button type="button" :aria-label="t('portalMap.enterSpace')" @click="$emit('enter', space)"><t-icon name="chevron-right" /></button>
      <template v-if="isActiveSpace">
        <button type="button" :aria-label="t('portalExperience.quickActions.search')" @click="$emit('search', space)"><t-icon name="search" /></button>
        <button type="button" :aria-label="t('portalMap.ask')" @click="$emit('ask', space)"><t-icon name="chat" /></button>
      </template>
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
function activate(){
  if(!props.space)return
  if(props.space.access_state === 'accessible')emit('enter',props.space)
  else emit('restricted',props.space)
}
</script>

<style scoped lang="less">
.space-summary{position:relative;display:flex;min-width:0;align-items:flex-start;gap:8px;padding:12px;border:1px solid var(--td-component-stroke);border-radius:10px;background:var(--td-bg-color-container);color:var(--td-text-color-primary);cursor:pointer;transition:border-color .16s ease,background .16s ease}.space-summary:hover,.space-summary:focus-visible{border-color:var(--td-component-border);background:var(--td-bg-color-container-hover);outline:none}.space-summary.normal{min-height:132px;padding:16px}.space-summary.discoverable{color:var(--td-text-color-secondary)}.summary-main{min-width:0;flex:1}.summary-main strong{display:block;overflow:hidden;font-size:13px;font-weight:600;text-overflow:ellipsis;white-space:nowrap}.normal .summary-main strong{font-size:15px}.summary-main p{display:-webkit-box;min-height:38px;margin:7px 0 12px;overflow:hidden;color:var(--td-text-color-secondary);font-size:12px;line-height:1.55;-webkit-box-orient:vertical;-webkit-line-clamp:2}.summary-main small{display:block;margin-top:5px;color:var(--td-text-color-placeholder);font-size:11px;white-space:nowrap}.permission{display:flex;align-items:center;gap:4px;margin-top:6px;color:var(--td-text-color-placeholder);font-size:11px}.lock{flex:none;color:var(--td-text-color-placeholder)}.accessible-actions{display:flex;flex-direction:row-reverse;gap:2px}.accessible-actions button{width:25px;height:25px;display:grid;place-items:center;padding:0;border:0;border-radius:6px;background:transparent;color:var(--td-text-color-placeholder);cursor:pointer}.accessible-actions button:not(:first-child){display:none}.space-summary:hover .accessible-actions button,.space-summary:focus-within .accessible-actions button{display:grid}.accessible-actions button:hover{background:var(--td-brand-color-light);color:var(--td-brand-color)}.is-loading{cursor:default}
</style>
