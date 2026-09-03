<template>
  <span class="brand-logo" :aria-label="branding.productName">
    {{ branding.productName }}
  </span>
</template>

<script setup lang="ts">
import { branding } from '@/config/branding'
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
