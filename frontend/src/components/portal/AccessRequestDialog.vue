<template>
  <t-dialog :visible="visible" header="申请只读访问" :confirm-btn="{content:'提交申请',loading:submitting,disabled:!valid}" @confirm="submit" @close="close">
    <div v-if="space" class="dialog-body"><p><strong>{{ space.display_name }}</strong></p><div class="permission">权限固定：Viewer / 只读访问</div><t-textarea v-model="reason" placeholder="请说明您需要访问该空间的原因" :maxlength="1000" :autosize="{minRows:4,maxRows:8}" /><small>{{ [...reason.trim()].length }}/1000</small></div>
  </t-dialog>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { PortalSpace } from '@/api/portal'
import { validAccessReason } from '@/stores/portalState'
const props=defineProps<{visible:boolean;space:PortalSpace|null;submitting:boolean}>()
const emit=defineEmits<{close:[];submit:[reason:string]}>()
const reason=ref('')
const valid=computed(()=>validAccessReason(reason.value))
watch(()=>props.visible,(open)=>{if(open)reason.value=''})
const close=()=>emit('close')
const submit=()=>{if(valid.value)emit('submit',reason.value.trim())}
</script>
<style scoped>.dialog-body{display:flex;flex-direction:column;gap:12px}.dialog-body p{margin:0}.permission{padding:10px 12px;border-radius:8px;background:var(--td-brand-color-light);color:var(--td-brand-color)}small{text-align:right;color:var(--td-text-color-placeholder)}</style>
