<template>
  <FeishuAccessV2 :kb-id="kbId" :status="status" :initial-url="initialUrl" @connect="links => emit('connect', links)" @imported="emit('imported')" />
</template>
<script setup lang="ts">
// Compatibility wrapper for older entry points. Governance APIs and stored
// records remain available; ordinary imports always use the compact interface.
import FeishuAccessV2 from './FeishuAccessV2.vue'
import type { FeishuStatus } from '@/api/feishuPersonal'
defineProps<{ kbId: string; status: FeishuStatus; initialUrl?: string }>()
const emit = defineEmits<{ connect: [links: string]; imported: [] }>()
</script>