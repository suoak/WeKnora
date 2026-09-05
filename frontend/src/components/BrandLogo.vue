<template>
  <span class="brand-logo" :class="{ 'brand-logo--inverse': inverse }" :aria-label="brandName">
    <span class="brand-logo__mark" aria-hidden="true">
      <img v-if="inverse" :src="branding.logoMarkInversePath" alt="">
      <template v-else>
        <img class="brand-logo__mark-light" :src="branding.logoMarkPath" alt="">
        <img class="brand-logo__mark-dark" :src="branding.logoMarkDarkPath" alt="">
      </template>
    </span>
    <span class="brand-logo__name">{{ brandName }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { branding } from '@/config/branding'

const { locale } = useI18n()
withDefaults(defineProps<{ inverse?: boolean }>(), { inverse: false })
const brandName = computed(() => locale.value === 'zh-CN'
  ? branding.productNameZh
  : branding.productName)
</script>

<style scoped>
.brand-logo {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: var(--td-text-color-primary, #101f38);
  font-size: 18px;
  font-weight: 700;
  line-height: 1;
  letter-spacing: -0.02em;
  white-space: nowrap;
}

.brand-logo__mark {
  display: inline-flex;
  width: 28px;
  height: 28px;
  flex: 0 0 28px;
}

.brand-logo__mark img {
  width: 100%;
  height: 100%;
  display: block;
}

.brand-logo__mark-dark {
  display: none !important;
}

:global(:root[theme-mode="dark"]) .brand-logo__mark-light {
  display: none !important;
}

:global(:root[theme-mode="dark"]) .brand-logo__mark-dark {
  display: block !important;
}

.brand-logo--inverse {
  color: #fff;
}
</style>
