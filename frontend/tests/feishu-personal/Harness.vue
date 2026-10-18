<script setup lang="ts">
import { ref } from 'vue'
import Browser from '../../src/views/knowledge/components/FeishuDriveBrowser.vue'
const selected = ref(''), disabled = ref(false), mounted = ref(true)
</script>
<template>
  <button id="disable" @click="disabled = !disabled">Toggle disabled</button>
  <button id="unmount" @click="mounted = !mounted">Toggle mounted</button>
  <Browser v-if="mounted" :disabled="disabled" @select="selected = $event" />
  <output id="selected">{{ selected }}</output>
</template>
