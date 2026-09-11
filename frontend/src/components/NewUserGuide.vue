<template>
  <SpotlightGuide v-model:active="active" :steps="steps" step-i18n-prefix="newUserGuide.steps"
    labels-prefix="newUserGuide" @finish="onFinish" />
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import SpotlightGuide from '@/components/SpotlightGuide.vue'
import { GLOBAL_USER_GUIDE_KEY, OPEN_NEW_USER_GUIDE_EVENT } from '@/config/contextualGuides'
import { useUIStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import { buildUserGuideFlow } from '@/config/userGuideFlow'
import type { SpotlightGuideStep } from '@/types/spotlightGuide'

const uiStore = useUIStore()
const authStore = useAuthStore()
const capabilities = useDeploymentCapabilitiesStore()

const steps = computed<SpotlightGuideStep[]>(() => buildUserGuideFlow({
  role: authStore.currentTenantRole,
  isSystemAdmin: authStore.isSystemAdmin,
  agentsSupported: capabilities.isSupported('agents'),
  mcpSupported: capabilities.isSupported('settings.mcp'),
}).map((step) => ({
  ...step,
  placement: step.target?.includes('nav-') || step.key === 'space' ? 'right' : 'bottom',
  before: step.target?.includes('nav-') || step.key === 'space' ? () => {
    uiStore.expandSidebar()
    window.dispatchEvent(new Event('weknora:open-mobile-navigation'))
  } : undefined,
})))

const active = ref(false)

const onFinish = () => {
  localStorage.setItem(GLOBAL_USER_GUIDE_KEY, '1')
}

const open = () => {
  active.value = true
}

const handleOpenEvent = () => {
  if (active.value) return
  open()
}

onMounted(() => {
  window.addEventListener(OPEN_NEW_USER_GUIDE_EVENT, handleOpenEvent)
  if (localStorage.getItem(GLOBAL_USER_GUIDE_KEY) !== '1') {
    window.setTimeout(() => {
      if (localStorage.getItem(GLOBAL_USER_GUIDE_KEY) !== '1') {
        open()
      }
    }, 700)
  }
})

onBeforeUnmount(() => {
  window.removeEventListener(OPEN_NEW_USER_GUIDE_EVENT, handleOpenEvent)
})
</script>
