<template>
  <t-tooltip :content="ownerName">
    <span class="portal-space-owner" :title="ownerName">
      <t-icon name="user" />
      <span>{{ ownerName }}</span>
    </span>
  </t-tooltip>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PortalSpace } from '@/api/portal'
import { resolvePortalResponsibleTeam } from '@/config/portalSpacePresentation'

const props = defineProps<{ space?: Pick<PortalSpace, 'tenant_id' | 'responsible_team'> }>()
const { t } = useI18n()
const ownerName = computed(() => resolvePortalResponsibleTeam(props.space, t('portalCard.unconfigured')))
</script>

<style scoped lang="less">
.portal-space-owner {
  display: inline-flex;
  min-width: 0;
  max-width: min(44%, 180px);
  flex: 0 1 auto;
  align-items: center;
  gap: 5px;
  color: var(--portal-text-metadata);
  font-size: 12.5px;
  font-weight: 550;
  line-height: 22px;
  white-space: nowrap;
}

.portal-space-owner > .t-icon {
  flex: none;
  font-size: 14px;
}

.portal-space-owner > span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
