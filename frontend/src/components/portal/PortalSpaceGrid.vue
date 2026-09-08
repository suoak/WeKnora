<template>
  <div v-if="loading" class="loading"><t-loading :text="t('portal.loadingSpaces')" /></div>
  <div v-else-if="!spaces.length" class="empty">{{ emptyText }}</div>
  <div v-else class="grid"><PortalSpaceCard v-for="space in spaces" :key="space.tenant_id" :space="space" :stage-labels="stageLabels" @request="$emit('request',$event)" @enter="$emit('enter',$event)" @interaction="$emit('interaction',$event)" /></div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PortalSpace } from '@/api/portal'
import PortalSpaceCard from './PortalSpaceCard.vue'
defineProps<{spaces:PortalSpace[];loading:boolean;emptyText:string;stageLabels:Record<string,string>}>()
defineEmits<{request:[space:PortalSpace];enter:[space:PortalSpace];interaction:[space:PortalSpace]}>()
const { t }=useI18n()
</script>
<style scoped>.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(290px,1fr));gap:16px}.loading,.empty{min-height:180px;display:grid;place-items:center;border:1px dashed var(--td-component-border);border-radius:16px;color:var(--td-text-color-secondary);background:var(--td-bg-color-secondarycontainer)}</style>
