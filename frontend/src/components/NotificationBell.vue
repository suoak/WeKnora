<template>
  <t-badge :count="notifications.unreadCount" :max-count="99">
    <button class="notification-bell" type="button" :aria-label="t('notifications.title')" :title="t('notifications.title')" @click="open">
      <t-icon name="notification" size="20px" />
      <span v-if="auth.pendingInvitationCount" class="invitation-dot" :title="t('notifications.invitationPending')" />
    </button>
  </t-badge>
  <t-drawer v-model:visible="visible" :header="t('notifications.title')" :footer="false" :close-btn="true" :z-index="2500" size="min(560px, 100vw)" @close="visible = false">
    <div class="inbox">
      <t-button variant="outline" @click="openInvitations">{{ t('notifications.invitations') }} ({{ auth.pendingInvitationCount }})</t-button>
      <t-tabs v-model="tab" @change="load">
        <t-tab-panel value="notifications" :label="t('notifications.messages')" />
        <t-tab-panel value="requests" :label="t('notifications.myRequests')" />
      </t-tabs>
      <div v-if="tab === 'notifications'" class="inbox-actions">
        <t-checkbox v-model="unreadOnly" @change="filterChanged">{{ t('notifications.unreadOnly') }}</t-checkbox>
        <t-button variant="text" :disabled="!notifications.unreadCount || busy" @click="markAll">{{ t('notifications.readAll') }}</t-button>
      </div>
      <t-loading v-if="loading" />
      <t-alert v-if="error" theme="error" :message="error"><template #operation><t-button size="small" @click="load">{{ t('notifications.retry') }}</t-button></template></t-alert>
      <template v-else-if="tab === 'notifications'">
        <p v-if="!loading && !rows.length">{{ t('notifications.empty') }}</p>
        <article v-for="row in rows" :key="row.id" class="inbox-card" :class="{ unread: !row.is_read }">
          <button class="message-link" type="button" @click="visit(row)"><strong>{{ row.title }}</strong><p>{{ row.content }}</p></button>
          <div class="message-meta"><time>{{ formatDate(row.created_at) }}</time><t-button variant="text" size="small" :disabled="row.is_read || busy" @click="mark(row)">{{ t(row.is_read ? 'notifications.read' : 'notifications.markRead') }}</t-button></div>
        </article>
        <t-pagination v-if="total > 20" v-model="page" :page-size="20" :total="total" :show-page-size="false" size="small" @change="load" />
      </template>
      <template v-else>
        <p v-if="!loading && !requests.length">{{ t('notifications.noRequests') }}</p>
        <article v-for="request in requests" :key="request.id" class="inbox-card">
          <strong>{{ request.tenant_name || `#${request.tenant_id}` }}</strong>
          <t-tag size="small">{{ t(`notifications.status.${request.status}`) }}</t-tag>
          <time>{{ formatDate(request.created_at) }}</time><p>{{ request.reason }}</p>
          <p v-if="request.reviewed_by">{{ t('notifications.reviewer') }}: {{ request.reviewer_name || request.reviewed_by }} · {{ formatDate(request.reviewed_at) }}</p>
          <p v-if="request.review_note">{{ t('notifications.reviewNote') }}: {{ request.review_note }}</p>
          <t-button v-if="request.status === 'pending'" size="small" variant="outline" :disabled="busy" @click="cancel(request.id)">{{ t('notifications.cancelRequest') }}</t-button>
        </article>
      </template>
    </div>
  </t-drawer>
  <MyInvitationsDialog v-model:visible="invitationsVisible" />
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { listNotifications, readAllNotifications, readNotification, type UserNotification } from '@/api/notifications'
import { listMyPortalAccessRequests, cancelMyPortalAccessRequest, listMyPortalSpaces, type TenantAccessRequest } from '@/api/portal'
import { useNotificationsStore } from '@/stores/notifications'
import { useAuthStore } from '@/stores/auth'
import { usePortalStore } from '@/stores/portal'
import { switchWorkspaceAndNavigate } from '@/utils/tenantSwitch'
import { notificationTarget } from '@/utils/notificationTarget'
import MyInvitationsDialog from '@/components/MyInvitationsDialog.vue'

const { t, locale } = useI18n()
const router = useRouter()
const notifications = useNotificationsStore(), auth = useAuthStore(), portal = usePortalStore()
const visible = ref(false), invitationsVisible = ref(false), tab = ref('notifications'), unreadOnly = ref(false)
const page = ref(1), total = ref(0), rows = ref<UserNotification[]>([]), requests = ref<TenantAccessRequest[]>([])
const loading = ref(false), busy = ref(false), error = ref('')
let loadVersion = 0
const formatDate = (value?: string) => value ? new Date(value).toLocaleString(locale.value) : ''

async function load() {
  const version = ++loadVersion
  const userID = auth.user?.id
  loading.value = true; error.value = ''
  try {
    if (tab.value === 'notifications') {
      const response = await listNotifications(page.value, unreadOnly.value)
      if (version === loadVersion && userID === auth.user?.id) {
        total.value = response.total
        const lastPage = Math.max(1, Math.ceil(response.total / 20))
        if (page.value > lastPage) { page.value = lastPage; void load(); return }
        rows.value = response.data
      }
    } else {
      const response = await listMyPortalAccessRequests()
      if (version === loadVersion && userID === auth.user?.id) requests.value = response.data || []
    }
  } catch (e: any) { if (version === loadVersion) error.value = e?.message || t('notifications.loadFailed') }
  finally { if (version === loadVersion) loading.value = false }
}
function open() { visible.value = true; void notifications.refresh(); void auth.fetchPendingInvitationCount(); void load() }
function openInvitations() { visible.value = false; invitationsVisible.value = true }
function filterChanged() { page.value = 1; void load() }
async function act(action: () => Promise<unknown>) {
  busy.value = true
  try { await action(); notifications.changed(); await load() }
  catch (e: any) { MessagePlugin.error(e?.message || t('notifications.loadFailed')) }
  finally { busy.value = false }
}
const mark = (row: UserNotification) => act(() => readNotification(row.id))
const markAll = () => act(async () => { await readAllNotifications(); page.value = 1 })
const cancel = (id: string) => act(async () => { await cancelMyPortalAccessRequest(id); portal.invalidate() })
async function visit(row: UserNotification) {
  if (!row.is_read) await mark(row)
  const target = notificationTarget(row)
  if (!target) return
  try {
    // Re-check current membership: an old notification must not imply access.
    const response = await listMyPortalSpaces()
    const space = response.data.find(item => item.tenant_id === row.tenant_id)
    if (!space || (row.notification_type === 'access_request_submitted' && space.role !== 'owner')) {
      MessagePlugin.warning(t('notifications.accessChanged')); return
    }
    visible.value = false
    if (Number(auth.effectiveTenantId) === row.tenant_id) await router.push(target)
    else switchWorkspaceAndNavigate({ tenantId: row.tenant_id, tenantName: space.tenant_name, role: space.role, targetPath: target })
  } catch (e: any) { MessagePlugin.error(e?.message || t('notifications.loadFailed')) }
}
watch(() => notifications.revision, () => { if (visible.value && auth.isLoggedIn) void load() })
watch(() => auth.user?.id, () => { loadVersion++; rows.value = []; requests.value = []; visible.value = false; invitationsVisible.value = false })
</script>

<style scoped>
.notification-bell{position:relative;display:grid;place-items:center;width:36px;height:36px;border:1px solid var(--td-component-border);border-radius:9px;background:var(--td-bg-color-container);color:var(--td-text-color-primary);cursor:pointer}
.invitation-dot{position:absolute;bottom:3px;right:3px;width:6px;height:6px;border-radius:50%;background:var(--td-warning-color)}
.inbox{display:flex;flex-direction:column;gap:14px;color:var(--td-text-color-primary)}.inbox-actions,.message-meta{display:flex;align-items:center;justify-content:space-between;gap:12px}.inbox-card{padding:14px;border:1px solid var(--td-component-border);border-radius:10px;background:var(--td-bg-color-container);overflow-wrap:anywhere}.inbox-card.unread{border-left:3px solid var(--td-brand-color)}.inbox-card time{display:block;color:var(--td-text-color-secondary);font-size:12px}.inbox-card p{white-space:pre-wrap;line-height:1.6;margin:8px 0}.message-link{width:100%;text-align:left;border:0;padding:0;background:transparent;color:inherit;cursor:pointer;font:inherit}.inbox-card .t-tag{margin-left:8px}
</style>
