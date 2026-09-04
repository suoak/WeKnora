<template>
  <span class="brand-logo" :aria-label="brandName">
    {{ brandName }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { branding } from '@/config/branding'

const { locale } = useI18n()
const brandName = computed(() => locale.value === 'zh-CN'
  ? branding.productNameZh
  : branding.productName)
</script>

<style scoped>
.brand-logo {
  display: inline-flex;
  align-items: baseline;
  color: var(--td-text-color-primary, #101f38);
  font-size: 18px;
  font-weight: 700;
  line-height: 1;
  letter-spacing: -0.02em;
  white-space: nowrap;
}
</style>
