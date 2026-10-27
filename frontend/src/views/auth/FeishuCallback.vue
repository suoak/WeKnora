<template>
  <main class="callback-page">
    <section class="callback-card" aria-live="polite" :aria-busy="phase === 'connecting'">
      <t-loading v-if="phase === 'connecting'" size="40px" />
      <check-circle-filled-icon v-else-if="phase === 'success'" class="state-icon success" aria-hidden="true" />
      <error-circle-filled-icon v-else class="state-icon failure" aria-hidden="true" />
      <h1>{{ title }}</h1>
      <p v-if="detail" role="status">{{ detail }}</p>
      <div v-if="phase !== 'connecting'" class="actions">
        <t-button v-if="phase === 'success'" @click="back">继续导入文档</t-button>
        <t-button v-else-if="sessionExpired" @click="login">登录 KnowHub</t-button>
        <t-button v-else :loading="busy" @click="reconnect">重新连接飞书</t-button>
        <t-button v-if="phase !== 'success'" variant="outline" @click="back">返回导入页面</t-button>
        <t-button v-if="uncertain" variant="text" :loading="busy" @click="verify">查看连接状态</t-button>
      </div>
    </section>
  </main>
</template>
<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { CheckCircleFilledIcon, ErrorCircleFilledIcon } from 'tdesign-icons-vue-next'
import { authorizeFeishu, completeFeishu, callbackFeishuStatus } from '@/api/feishuPersonal'
import { FEISHU_PENDING } from '@/i18n/feishuPersonal'
import { FEISHU_CALLBACK, safeImportPath, readFlow, saveFeishuDraft, callbackFailure, currentOwner, type CallbackAttempt, type FeishuDraft } from '@/utils/feishuOAuthFlow'

const router = useRouter(), phase = ref<'connecting' | 'success' | 'failed'>('connecting')
const detail = ref(''), sessionExpired = ref(false), uncertain = ref(false), busy = ref(false)
const title = computed(() => phase.value === 'connecting' ? '正在确认飞书授权，请稍候…' : phase.value === 'success' ? '飞书账号连接成功' : '飞书账号未连接完成')
let path = '/platform/knowledge-bases', mounted = true, timer: ReturnType<typeof setTimeout> | undefined
let attempt: CallbackAttempt | undefined
onUnmounted(() => { mounted = false; clearTimeout(timer) })
function fail(error: any) {
  if (!mounted) return
  phase.value = 'failed'; detail.value = callbackFailure(error || {})
  sessionExpired.value = error?.status === 401
  uncertain.value = ['timeout', 'network'].includes(error?.category)
}
function success(revision?: string) {
  if (!mounted) return
  if (attempt) { attempt.confirmed = revision; try { sessionStorage.setItem(FEISHU_CALLBACK, JSON.stringify(attempt)) } catch { /* Server already confirmed. */ } }
  phase.value = 'success'; detail.value = '现在可以继续导入飞书文档。'; uncertain.value = false
  timer = setTimeout(back, 1200)
}
async function verify() {
  busy.value = true
  try {
    const status = await callbackFeishuStatus()
    // An old valid grant is not proof that supplemental consent completed.
    if (status.connected && status.authorization_revision && attempt && (attempt.confirmed === status.authorization_revision || status.authorization_revision !== attempt.before)) success(status.authorization_revision)
    else fail({ category: 'network' })
  } catch (error) { fail(error) } finally { busy.value = false }
}
async function back() {
  clearTimeout(timer)
  try {
    const result = await router.replace(path)
    if (result || router.currentRoute.value.path === '/feishu/callback') detail.value = '自动返回未完成，请点击“继续导入文档”。'
  } catch { detail.value = '自动返回未完成，请点击“继续导入文档”。' }
}
function login() { void router.replace({ path: '/login', query: { returnUrl: path } }) }
async function reconnect() {
  busy.value = true
  try {
    const status = await callbackFeishuStatus(), draft = readFlow<FeishuDraft>(FEISHU_PENDING)
    saveFeishuDraft(draft?.kbId || '', draft?.url || '', path, status.authorization_revision || '')
    const result = await authorizeFeishu(), url = new URL(result.authorization_url)
    if (url.origin !== 'https://accounts.feishu.cn' || url.username || url.password) throw { error: 'feishu_app_unavailable' }
    window.location.assign(url.href)
  } catch (error) { fail(error) } finally { busy.value = false }
}
onMounted(async () => {
  const query = new URLSearchParams(window.location.search)
  const code = query.get('code') || '', state = query.get('state') || '', cancelled = query.has('error')
  try {
    window.history.replaceState(window.history.state, '', '/feishu/callback')
    const draft = readFlow<FeishuDraft>(FEISHU_PENDING)
    path = safeImportPath(draft?.path)
    attempt = readFlow<CallbackAttempt>(FEISHU_CALLBACK)
    if (cancelled) { fail({ category: 'cancelled' }); return }
    if (!code || !state || attempt?.started) { await verify(); if (!attempt) fail({ error: 'invalid_oauth_callback' }); return }
    if (!attempt) {
      const baseline = await callbackFeishuStatus()
      attempt = { started: false, before: baseline.authorization_revision || '', createdAt: Date.now(), owner: currentOwner() }
    }
    attempt.started = true
    // Store no OAuth parameters. Refresh/back only verifies; never replays.
    sessionStorage.setItem(FEISHU_CALLBACK, JSON.stringify(attempt))
    try {
      const result = await completeFeishu(state, code)
      if (!result.connected) throw { error: 'feishu_operation_failed' }
      const status = await callbackFeishuStatus()
      if (!status.connected) throw { error: 'feishu_operation_failed' }
      success(status.authorization_revision)
    } catch (error: any) {
      if (['timeout', 'network'].includes(error?.category)) await verify()
      else fail(error)
    }
  } catch { fail({ category: 'network' }) }
})
</script>
<style scoped>
.callback-page { min-height:100dvh; display:grid; place-items:center; padding:24px; box-sizing:border-box; background:var(--td-bg-color-page); }
.callback-card { width:100%; max-width:440px; box-sizing:border-box; padding:40px 28px; border-radius:16px; background:var(--td-bg-color-container); text-align:center; box-shadow:var(--td-shadow-1); }
.state-icon { font-size:48px; }
.success { color:var(--td-success-color); }
.failure { color:var(--td-warning-color); }
h1 { font-size:20px; line-height:1.5; margin:20px 0 12px; color:var(--td-text-color-primary); }
p { font-size:14px; line-height:1.7; color:var(--td-text-color-secondary); overflow-wrap:anywhere; }
.actions { display:flex; flex-wrap:wrap; justify-content:center; gap:12px; margin-top:24px; }
@media(max-width:480px) { .callback-card { padding:32px 20px; } .actions > * { width:100%; } }
</style>
