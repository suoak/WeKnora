<template>
  <span class="brand-logo" :class="{ 'brand-logo--inverse': inverse, 'brand-logo--lockup': portalLockup }" :aria-label="ariaName">
    <span class="brand-logo__mark" aria-hidden="true">
      <img v-if="inverse" :src="branding.logoMarkInversePath" alt="">
      <template v-else>
        <img class="brand-logo__mark-light" :src="branding.logoMarkPath" alt="">
        <img class="brand-logo__mark-dark" :src="branding.logoMarkDarkPath" alt="">
      </template>
    </span>
    <span class="brand-logo__copy">
      <span class="brand-logo__name">{{ brandName }}</span>
      <span v-if="brandSubtitle" class="brand-logo__subtitle">{{ brandSubtitle }}</span>
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { branding } from '@/config/branding'

const { locale } = useI18n()
const props=withDefaults(defineProps<{ inverse?: boolean; portalLockup?: boolean }>(), { inverse: false, portalLockup: false })
const brandName = computed(() => props.portalLockup ? branding.productName : locale.value === 'zh-CN' ? `${branding.productName} · ${branding.productNameZh}` : branding.productName)
const brandSubtitle = computed(() => props.portalLockup && locale.value === 'zh-CN' ? branding.productNameZh : '')
const ariaName = computed(() => brandSubtitle.value ? `${brandName.value} · ${brandSubtitle.value}` : brandName.value)
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

.brand-logo__copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.brand-logo__subtitle {
  color: var(--td-text-color-secondary, #526071);
  font-size: 11.5px;
  font-weight: 500;
  letter-spacing: 0;
  line-height: 1.1;
}

.brand-logo--lockup .brand-logo__name {
  font-size: 14px;
  font-weight: 650;
  letter-spacing: .01em;
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

.brand-logo--inverse .brand-logo__subtitle {
  color: rgba(255, 255, 255, 0.7);
}
</style>
