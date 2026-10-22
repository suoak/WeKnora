<template>
  <main class="mcp-access-center">
    <header class="page-header">
      <div><div class="eyebrow"><t-icon name="link" /> KNOWHUB MCP</div><h1>MCP 接入</h1><p>将 KnowHub 企业知识连接到常用 AI 工具。</p></div>
      <t-button theme="primary" size="large" :disabled="loading || !!loadError" @click="openPersonalCreate"><template #icon><t-icon name="add" /></template>创建个人 Key</t-button>
    </header>
    <section class="security-note service-summary"><span><t-icon name="secured" /></span><div><strong>内置 Go MCP Server <small>Streamable HTTP</small></strong><p>使用个人 Key，将可用知识连接到常用 AI 工具。</p><div class="service-url"><code>{{ unifiedURL || '/api/v1/mcp/personal' }}</code><t-button size="small" variant="outline" :disabled="!unifiedURL" @click="copyURL">复制地址</t-button></div><p class="service-footnote">只读检索与阅读，不提供上传、修改、删除或管理操作。凭证状态不代表客户端连接状态。</p></div></section>
    <div class="connect-steps" aria-label="连接步骤"><span><b>1</b> 创建个人 Key</span><span><b>2</b> 复制客户端配置</span><span><b>3</b> 在 AI 工具中检索知识</span></div>
    <t-alert v-if="!publicUrl && !loading" theme="warning" class="url-warning">服务地址尚未配置，请联系管理员。你仍可管理已有凭证。</t-alert>

    <section class="credential-surface" aria-live="polite">
      <div class="section-heading"><div><h2>我的个人 MCP Key</h2><span>{{ personalKeys.length }} 个个人凭证</span></div><t-button variant="text" :loading="loading" @click="load"><t-icon name="refresh" /> 刷新</t-button></div>
      <div v-if="keys.length" class="credential-filters" aria-label="凭证状态筛选"><button v-for="filter in statusFilters" :key="filter.value" type="button" :class="{ active: statusFilter === filter.value }" @click="statusFilter = filter.value">{{ filter.label }}<span>{{ filter.count }}</span></button></div>
      <div v-if="loading && !keys.length" class="state-panel"><t-loading size="medium" />正在加载接入配置…</div>
      <div v-else-if="loadError" class="state-panel column"><t-icon name="error-circle" /><strong>加载失败</strong><span>{{ loadError }}</span><t-button variant="outline" @click="load">重试</t-button></div>
      <div v-else-if="!keys.length" class="state-panel column"><span class="empty-mark"><t-icon name="link" /></span><h3>连接你的企业知识</h3><p>创建个人 Key，复制配置到 AI 工具，即可开始检索知识。</p><t-button theme="primary" @click="openPersonalCreate">创建个人 Key</t-button></div>
      <div v-else-if="!filteredKeys.length" class="state-panel column compact"><t-icon name="filter" /><strong>当前筛选下没有接入配置</strong><button type="button" class="clear-filter" @click="statusFilter = 'all'">查看全部</button></div>
      <div v-else class="credential-list">
        <p v-if="!personalKeys.length" class="personal-empty">还没有当前筛选状态的个人 Key。历史接入保留在下方，可继续管理。</p>
        <article v-for="key in personalKeys" :key="key.id" class="credential-card personal-card">
          <div class="identity"><span class="client-mark"><t-icon name="key" /></span><div><h3 :title="key.name">{{ key.name }}</h3><p><i :class="`is-${lifecycleState(key)}`" /> {{ statusLabel(lifecycleState(key)) }} · {{ personalRangeLabel(key) }}</p></div></div>
          <dl class="meta"><div><dt>最近使用</dt><dd>{{ relativeTime(key.last_used_at) }}</dd></div><div><dt>有效期</dt><dd>{{ key.expires_at ? formatDate(key.expires_at) : '永不过期' }}</dd></div><div><dt>凭证</dt><dd class="token-hint">{{ key.token_hint || '未记录' }}</dd></div></dl>
          <div class="actions"><t-button v-if="lifecycleState(key) !== 'revoked'" variant="outline" :disabled="!publicUrl" @click="showTemplate(key)">连接配置</t-button><t-button variant="text" @click="openDetails(key)">管理</t-button></div>
        </article>
        <details v-if="legacyKeys.length" class="legacy-access"><summary>历史接入 · {{ legacyKeys.length }} 个凭证</summary><p>保留原有客户端、授权范围和能力。客户端名称仅是历史配置标记。</p><article v-for="key in legacyKeys" :key="key.id" class="credential-card personal-card"><div class="identity"><span class="client-mark"><t-icon :name="clientMeta(key.client_type).icon" /></span><div><h3>{{ key.name }}</h3><p>{{ clientMeta(key.client_type).label }} · {{ statusLabel(lifecycleState(key)) }}</p></div></div><dl class="meta"><div><dt>最近使用</dt><dd>{{ relativeTime(key.last_used_at) }}</dd></div><div><dt>有效期</dt><dd>{{ key.expires_at ? formatDate(key.expires_at) : '永不过期' }}</dd></div><div><dt>Token Hint</dt><dd>{{ key.token_hint || '未记录' }}</dd></div></dl><div class="actions"><t-button v-if="lifecycleState(key) !== 'revoked'" variant="outline" :disabled="!publicUrl" @click="showTemplate(key)">兼容配置</t-button><t-button variant="text" @click="openDetails(key)">管理</t-button></div></article></details>
      </div>
    </section>

    <t-dialog v-model:visible="personalCreateVisible" :header="personalEditingID ? '修改指定范围' : '创建个人 Key'" width="min(640px, 94vw)" :footer="false">
      <p>填写名称、有效期和知识范围，保存后即可复制连接配置。</p>
      <div class="field"><label>接入名称</label><t-input v-model="name" maxlength="128" placeholder="例如：我的 Codex" :disabled="saving" /></div>
      <div class="field"><label>有效期</label><t-radio-group v-model="expiryMode" :disabled="saving"><t-radio value="90">90 天</t-radio><t-radio value="custom">指定日期</t-radio><t-radio value="never">永不过期</t-radio></t-radio-group><input v-if="expiryMode === 'custom'" v-model="customExpiry" class="native-datetime" type="datetime-local" :disabled="saving" /></div>
      <div class="field"><label>知识范围</label><t-radio-group v-model="personalRange" :disabled="saving || !!personalEditingID"><t-radio value="default">默认范围</t-radio><t-radio value="selected">指定范围</t-radio></t-radio-group></div>
      <p v-if="personalRange === 'default'">自动使用当前可用的知识范围。</p>
      <section v-else class="private-range-picker" aria-label="选择私人知识库"><p>仅检索所选且当前有权读取的私人知识。新增授权不会自动扩大此 Key 的指定范围。</p><section v-for="option in selectablePrivateOptions" :key="option.tenant_id" class="scope-card"><header><strong>{{ option.tenant_name }}</strong></header><t-checkbox-group v-model="privateSelections[option.tenant_id]" :disabled="saving"><t-checkbox v-for="kb in option.owned_knowledge_bases" :key="ownedRef(kb.id)" :value="ownedRef(kb.id)">{{ kb.name }}</t-checkbox><t-checkbox v-for="share in option.shared_knowledge_bases" :key="sharedRef(share.knowledge_base.id, share.share_id)" :value="sharedRef(share.knowledge_base.id, share.share_id)">{{ share.knowledge_base.name }} · 共享给此空间</t-checkbox></t-checkbox-group></section><t-button size="small" variant="text" :disabled="saving" @click="privateSelections = {}">清空选择</t-button><p v-if="!selectablePrivateOptions.length">当前没有可选的私人知识库。</p></section>
      <footer class="wizard-footer"><t-button variant="outline" :disabled="saving" @click="personalCreateVisible = false">取消</t-button><span /><t-button theme="primary" :loading="saving" @click="savePersonal">{{ personalEditingID ? '保存修改' : '创建个人 Key' }}</t-button></footer>
    </t-dialog>

    <t-dialog v-model:visible="privateScopeVisible" header="更新私人空间范围" width="min(600px, 94vw)" :confirm-btn="{ content: '添加选中的私人空间', disabled: !scopeAdditions.length, loading: privateScopeSaving }" @confirm="savePrivateScope">
      <p>新加入的私人空间需要你主动添加；原 Key 的知识库限制和有效期保持不变。</p>
      <p v-if="scopeKey">当前私人范围：{{ scopeSummary(scopeKey) }}</p>
      <t-checkbox-group v-model="scopeAdditions"><t-checkbox v-for="option in newPrivateOptions" :key="option.tenant_id" :value="option.tenant_id">{{ option.tenant_name }}</t-checkbox></t-checkbox-group>
      <p v-if="!newPrivateOptions.length">没有可添加的新私人空间。</p>
    </t-dialog>

    <t-dialog v-model:visible="editorVisible" header="历史接入设置" width="min(820px, 94vw)" :footer="false" @close="resetEditor">
      <div class="steps"><button v-for="(label, index) in wizardSteps" :key="label" type="button" :class="{ active: step === index, done: step > index }" @click="goToStep(index)"><b>{{ index + 1 }}</b>{{ label }}</button></div>
      <div class="wizard-body">
        <section v-if="step === 0" class="panel"><div class="field"><label>接入名称</label><t-input v-model="name" maxlength="128" placeholder="例如：Jerry - WorkBuddy" /></div><div class="field"><label>AI 客户端</label><div class="client-grid"><button v-for="client in clients" :key="client.value" type="button" :class="{ selected: clientType === client.value }" @click="clientType = client.value"><t-icon :name="client.icon" /><strong>{{ client.label }}</strong><span>{{ client.description }}</span></button></div></div></section>
        <section v-else-if="step === 1" class="panel"><div class="intro"><h3>选择空间</h3><p>只显示你当前拥有有效成员关系的空间。</p></div><div v-if="invalidTenantIds.length" class="invalid-scope"><strong>以下原授权空间已失效或无权限</strong><div v-for="tenantID in invalidTenantIds" :key="tenantID"><span>空间 #{{ tenantID }} · 已失效 / 无权限</span><t-button size="small" variant="text" @click="removeInvalidTenant(tenantID)">从凭证范围移除</t-button></div></div><div class="space-grid"><label v-for="option in options" :key="option.tenant_id" :class="{ selected: selectedTenantIds.includes(option.tenant_id) }"><t-checkbox :checked="selectedTenantIds.includes(option.tenant_id)" @change="toggleTenant(option.tenant_id, Boolean($event))" /><span><strong :title="option.tenant_name">{{ option.tenant_name }}</strong><small>{{ option.owned_knowledge_bases.length }} 个本空间知识库</small></span></label></div></section>
        <section v-else-if="step === 2" class="panel"><div class="intro"><h3>设置每个空间的知识库范围</h3><p>“动态全部”会包含未来新增且你有权访问的知识库。</p></div><div class="scope-list"><section v-for="option in selectedOptions" :key="option.tenant_id" class="scope-card"><header><strong :title="option.tenant_name">{{ option.tenant_name }}</strong><t-radio-group v-model="ensureDraft(option.tenant_id).mode" variant="default-filled"><t-radio-button value="all">动态全部</t-radio-button><t-radio-button value="selected">仅指定</t-radio-button></t-radio-group></header><div v-if="invalidRefs(option.tenant_id).length" class="invalid-scope"><strong>原授权知识库已失效或无权限</strong><div v-for="refValue in invalidRefs(option.tenant_id)" :key="refValue"><span :title="refValue">{{ invalidRefLabel(refValue) }}</span><t-button size="small" variant="text" @click="removeInvalidRef(option.tenant_id, refValue)">移除</t-button></div></div><div v-if="ensureDraft(option.tenant_id).mode === 'selected'" class="kb-groups"><div><h4>本空间知识库</h4><t-checkbox-group v-model="ensureDraft(option.tenant_id).selected"><t-checkbox v-for="kb in option.owned_knowledge_bases" :key="ownedRef(kb.id)" :value="ownedRef(kb.id)"><span :title="kb.name">{{ kb.name }}</span></t-checkbox></t-checkbox-group></div><div v-if="option.shared_knowledge_bases.length"><h4>共享知识库</h4><t-checkbox-group v-model="ensureDraft(option.tenant_id).selected"><t-checkbox v-for="share in option.shared_knowledge_bases" :key="sharedRef(share.knowledge_base.id, share.share_id)" :value="sharedRef(share.knowledge_base.id, share.share_id)"><span :title="`${share.knowledge_base.name} · ${share.org_name}`">{{ share.knowledge_base.name }} · {{ share.org_name }}</span></t-checkbox></t-checkbox-group></div></div></section></div></section>
        <section v-else-if="step === 3" class="panel"><div class="intro"><h3>选择能力</h3><p>默认仅启用知识检索和知识问答，不授予高权限。</p></div><div class="capability-options"><label v-for="capability in capabilityOptions" :key="capability.value" :class="{ selected: capabilities.includes(capability.value) }"><t-checkbox :checked="capabilities.includes(capability.value)" @change="toggleCapability(capability.value, Boolean($event))" /><span><strong>{{ capability.label }}</strong><small>{{ capability.description }}</small></span></label></div></section>
        <section v-else class="panel"><div class="intro"><h3>设置有效期</h3><p>到期后凭证立即失效；遗失 Secret 时请执行轮换。</p></div><div class="expiry-card"><t-radio-group v-model="expiryMode"><t-radio value="90">90 天</t-radio><t-radio value="custom">指定日期</t-radio><t-radio value="never">永不过期</t-radio></t-radio-group><input v-if="expiryMode === 'custom'" v-model="customExpiry" class="native-datetime" type="datetime-local" /></div><div class="review"><h4>即将{{ editingID ? '更新' : '创建' }}</h4><p><strong>{{ name || '未命名接入' }}</strong> · {{ clientMeta(clientType).label }}</p><p>{{ selectedTenantIds.length }} 空间 · {{ capabilities.map(capabilityLabel).join('、') }}</p></div></section>
      </div>
      <footer class="wizard-footer"><t-button variant="outline" @click="editorVisible = false">取消</t-button><span /><t-button v-if="step > 0" variant="text" @click="step--">上一步</t-button><t-button v-if="step < 4" theme="primary" @click="nextStep">下一步</t-button><t-button v-else theme="primary" :loading="saving" @click="save">{{ editingID ? '保存修改' : '创建接入' }}</t-button></footer>
    </t-dialog>

    <t-drawer v-model:visible="detailsVisible" header="接入详情" size="min(680px, 94vw)" :footer="false"><div v-if="selectedKey" class="details"><div class="identity"><span class="client-mark"><t-icon :name="clientMeta(selectedKey.client_type).icon" /></span><div><h3 :title="selectedKey.name">{{ selectedKey.name }}</h3><p>{{ clientMeta(selectedKey.client_type).label }} · {{ statusLabel(lifecycleState(selectedKey)) }}</p></div></div><dl><div><dt>授权范围</dt><dd>{{ scopeSummary(selectedKey) }}</dd></div><div v-if="!isUnifiedPersonalKey(selectedKey)"><dt>历史能力</dt><dd>{{ selectedKey.capabilities.map(capabilityLabel).join('、') }}</dd></div><div><dt>最近使用</dt><dd>{{ relativeTime(selectedKey.last_used_at) }}</dd></div><div><dt>有效期</dt><dd>{{ selectedKey.expires_at ? formatTime(selectedKey.expires_at) : '永不过期' }}</dd></div><div><dt>Token Hint</dt><dd>{{ selectedKey.token_hint || '未记录' }}</dd></div></dl><div class="detail-actions"><template v-if="lifecycleState(selectedKey) !== 'revoked'"><t-button variant="outline" @click="openEdit(selectedKey)">修改范围</t-button><t-button variant="outline" @click="requestRotate(selectedKey)">重新生成 Secret</t-button><t-popconfirm content="停用后旧 Secret 将立即且永久失效，历史记录仍会保留。确认停用？" @confirm="revoke(selectedKey.id)"><t-button theme="danger" variant="text">停用接入</t-button></t-popconfirm></template><t-button v-else theme="primary" @click="requestReconnect(selectedKey)">重新接入</t-button></div></div></t-drawer>

    <t-dialog v-model:visible="reconnectVisible" header="重新接入" width="560px" :confirm-btn="{ content: '确认重新接入', loading: reconnecting }" @confirm="confirmReconnect">
      <p>将基于原接入配置创建一个全新凭证。原凭证保持停用，旧 Secret 仍永久失效。</p>
      <t-alert theme="warning">系统会按原空间、知识库和能力范围提交，并重新执行实时权限校验；如权限已变化，创建会失败且不会缩减授权范围。</t-alert>
      <p v-if="reconnectUsesDefaultExpiry" class="expiry-note">原有效期已过期或不可用，新凭证将采用当前默认有效期。</p>
    </t-dialog>

    <t-dialog v-model:visible="rotateVisible" header="重新生成 Secret" width="560px" :confirm-btn="{ content: '确认重新生成', loading: rotating }" @confirm="confirmRotate">
      <t-alert theme="warning">重新生成后，旧 Secret 将立即失效。已配置 WorkBuddy、WorkMate、Cursor 等客户端时，需要同步更新为新 Secret。</t-alert>
      <div class="rotate-options"><strong>有效期策略</strong><t-radio-group v-model="rotateExpiryMode"><t-radio v-if="rotateKey && lifecycleState(rotateKey) !== 'expired'" value="preserve">保持当前有效期</t-radio><t-radio value="custom">设置新的有效期</t-radio><t-radio value="never">永不过期</t-radio></t-radio-group><input v-if="rotateExpiryMode === 'custom'" v-model="rotateCustomExpiry" class="native-datetime" type="datetime-local" /><p v-if="rotateKey && lifecycleState(rotateKey) === 'expired'">该凭证已经过期，必须明确选择未来有效期或永不过期。</p></div>
    </t-dialog>

    <t-dialog v-model:visible="configVisible" header="Secret 与客户端配置" width="860px" @close="clearPlaintextToken"><t-alert :theme="token === TOKEN_PLACEHOLDER ? 'info' : 'warning'">{{ token === TOKEN_PLACEHOLDER ? '此历史凭证未保存可恢复的 Secret。如需长期查看，请主动重新生成 Secret；原凭证的日期和权限不会自动改变。' : 'Secret 可长期查看和复制，直至过期、被撤销或重新生成。服务端加密保存，仅当前凭证本人可在此查看。' }}</t-alert><t-alert v-if="revealingSecret" theme="info">正在读取你的 Secret…</t-alert><t-alert v-if="token !== TOKEN_PLACEHOLDER && selectedKey?.secret_available === false" theme="warning">当前服务未配置安全密钥保存，关闭后无法重新查看，请联系管理员。</t-alert><div class="token-row"><code>{{ token }}</code><t-button size="small" variant="outline" :disabled="token === TOKEN_PLACEHOLDER" @click="copySensitive(token)">复制 Secret</t-button></div><div class="config-heading"><strong>{{ configPresetTitle }}</strong><span>Streamable HTTP · 多空间隔离</span></div><t-alert v-if="!configPresetVerified" theme="info">尚未完成该客户端的真实导入验收，以下仅提供配置模板，不标记为一键可用配置。</t-alert><t-radio-group v-if="personalConfig" v-model="personalConfigFormat" variant="default-filled"><t-radio-button value="codex">Codex CLI</t-radio-button><t-radio-button value="generic">Generic MCP JSON</t-radio-button></t-radio-group><p v-if="personalConfig && personalConfigFormat === 'codex'">将下面配置添加到 Codex 的 config.toml，并将上方 Secret 设置为 Codex 进程的 KNOWHUB_MCP_KEY 环境变量。</p><p v-if="!personalConfig && configClientType === 'workmate'">WorkMate 仅初始化及工具发现通过，实际会话知识调用尚未验收。</p><pre>{{ jsonConfig }}</pre><t-button v-if="personalConfig" variant="outline" :loading="testingPersonal" :disabled="!publicUrl || token === TOKEN_PLACEHOLDER" @click="testPersonalConnection">测试连接</t-button><p v-if="personalConnection" role="status">{{ personalConnection }}</p><div v-if="!personalConfig" class="space-copy"><span>按空间复制：</span><t-button v-for="space in configSpaces" :key="space.tenantId" size="small" variant="outline" :disabled="!publicUrl || token === TOKEN_PLACEHOLDER" @click="copySpace(space)">{{ space.tenantName }}</t-button></div><template #footer><t-button variant="outline" @click="configVisible = false">关闭</t-button><t-button v-if="token === TOKEN_PLACEHOLDER" variant="outline" :disabled="!publicUrl" @click="copySensitive(jsonConfig)">复制配置模板</t-button><t-button v-if="token === TOKEN_PLACEHOLDER && selectedKey" theme="primary" @click="configVisible = false; requestRotate(selectedKey)">重新生成 Secret</t-button><t-button v-if="token !== TOKEN_PLACEHOLDER" theme="primary" :disabled="!publicUrl" @click="copySensitive(jsonConfig)">复制全部配置</t-button></template></t-dialog>
  </main>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import { createMCPAccessKey, getMCPAccessKeyScopeOptions, listMCPAccessKeys, revokeMCPAccessKey, revealMCPAccessKey, rotateMCPAccessKey, updateMCPAccessKey, type KBScopeMode, type MCPAccessKey, type MCPCapability, type MCPClientType, type MCPKBRef, type MCPScopeOption } from '@/api/mcpAccessKeys'
import { copyWithToast } from '@/utils/clipboard'
import { buildMCPServers, stringifyMCPConfig, verifiedConfigPreset, type MCPConfigSpace } from './mcpConfig'
import { availableKBRefs, invalidCredentialScopes } from './mcpAccessScope'
import { credentialToEditDraft } from './mcpAccessEditor'
import { isUnifiedPersonalKey, personalKnowledgeRangePayload, personalPrivateScopeUpdate, type PersonalKnowledgeRange } from './personalMcpPresentation'
import { createReconnectAndReveal, credentialLifecycleActions, credentialMatchesStatusFilter, credentialToReconnectPayload, getCredentialLifecycleState, type CredentialStatusFilter } from './mcpAccessLifecycle'

const auth = useAuthStore()
const TOKEN_PLACEHOLDER = '<YOUR_MCP_KEY>'
interface DraftScope { mode: KBScopeMode; selected: string[] }
const clients: Array<{ value: MCPClientType; label: string; icon: string; description: string }> = [
  { value: 'workbuddy', label: 'WorkBuddy', icon: 'service', description: '企业研发 AI 助手' }, { value: 'workmate', label: 'WorkMate', icon: 'user-talk', description: '团队智能工作伙伴' }, { value: 'cursor', label: 'Cursor', icon: 'code', description: 'AI 编程客户端' }, { value: 'codebuddy', label: 'CodeBuddy', icon: 'terminal-rectangle', description: '代码研发助手' }, { value: 'generic', label: 'Generic MCP', icon: 'link', description: '其它 MCP 客户端' },
]
import { personalCodexConfig, personalMcpConfig, personalMcpURL, testPersonalMcpConnection } from './personalMcpConfig'
const personalConfig = ref(false), testingPersonal = ref(false), personalConnection = ref('')
const personalCreateVisible = ref(false)
const personalRange = ref<PersonalKnowledgeRange>('default'), personalEditingID = ref<number | null>(null)
const privateSelections = ref<Record<number, string[]>>({})
const revealingSecret = ref(false)
let connectionRequest = 0
const personalConfigFormat = ref<'generic' | 'codex'>('codex')
async function testPersonalConnection() { testingPersonal.value = true; personalConnection.value = ''; try { await testPersonalMcpConnection(personalMcpURL(publicUrl.value), token.value); personalConnection.value = '服务鉴权与只读工具发现通过；尚未验证客户端会话调用。' } catch (e) { personalConnection.value = e instanceof Error ? e.message : '连接失败' } finally { testingPersonal.value = false } }
const capabilityOptions: Array<{ value: MCPCapability; label: string; description: string }> = [
  { value: 'retrieve', label: '知识检索', description: '检索授权知识库中的内容' }, { value: 'chat', label: '知识问答', description: '基于授权知识进行问答' }, { value: 'read_agents', label: '智能体访问', description: '读取并使用有权访问的智能体' },
]
const wizardSteps = ['客户端', '空间', '知识库', '能力', '有效期']
const keys = ref<MCPAccessKey[]>([]), options = ref<MCPScopeOption[]>([]), loading = ref(false), saving = ref(false)
const publicUrl = ref(''), loadError = ref(''), editorVisible = ref(false), detailsVisible = ref(false), configVisible = ref(false)
const unifiedURL = computed(() => publicUrl.value ? personalMcpURL(publicUrl.value) : '')
const privateScopeVisible = ref(false), privateScopeSaving = ref(false), scopeAdditions = ref<number[]>([])
const scopeKey = ref<MCPAccessKey | null>(null)
const newPrivateOptions = computed(() => options.value.filter(option => !scopeKey.value?.tenant_scopes.some(scope => scope.tenant_id === option.tenant_id)))
const selectablePrivateOptions = computed(() => options.value.filter(option => option.owned_knowledge_bases.length || option.shared_knowledge_bases.length))
const rotateVisible = ref(false), rotating = ref(false), rotateKey = ref<MCPAccessKey | null>(null)
const reconnectVisible = ref(false), reconnecting = ref(false), reconnectKey = ref<MCPAccessKey | null>(null)
const rotateExpiryMode = ref<'preserve' | 'custom' | 'never'>('preserve'), rotateCustomExpiry = ref('')
const editingID = ref<number | null>(null), selectedKey = ref<MCPAccessKey | null>(null), step = ref(0), name = ref('')
const clientType = ref<MCPClientType>('workbuddy'), capabilities = ref<MCPCapability[]>(['retrieve', 'chat'])
const selectedTenantIds = ref<number[]>([]), draftScopes = ref<Record<number, DraftScope>>({})
const expiryMode = ref<'90' | 'custom' | 'never'>('never'), customExpiry = ref(''), token = ref('')
const configSpaces = ref<MCPConfigSpace[]>([]), configClientType = ref<MCPClientType>('generic')
const selectedOptions = computed(() => options.value.filter(option => selectedTenantIds.value.includes(option.tenant_id)))
const statusFilter = ref<CredentialStatusFilter>('all')
const lifecycleState = (key: MCPAccessKey) => getCredentialLifecycleState(key)
const filteredKeys = computed(() => keys.value.filter(key => credentialMatchesStatusFilter(key, statusFilter.value)))
const personalKeys = computed(() => filteredKeys.value.filter(isUnifiedPersonalKey))
const legacyKeys = computed(() => filteredKeys.value.filter(key => !isUnifiedPersonalKey(key)))
const statusFilters = computed(() => ([
  { value: 'all' as const, label: '全部', count: keys.value.length },
  { value: 'active' as const, label: '已启用', count: keys.value.filter(key => lifecycleState(key) === 'active').length },
  { value: 'revoked' as const, label: '已停用', count: keys.value.filter(key => lifecycleState(key) === 'revoked').length },
]))
const invalidTenantIds = computed(() => selectedTenantIds.value.filter(id => !options.value.some(option => option.tenant_id === id)))
const configPresetVerified = computed(() => personalConfig.value || verifiedConfigPreset(configClientType.value))
const effectiveConfigClient = computed(() => configPresetVerified.value ? configClientType.value : 'generic')
const configPresetTitle = computed(() => personalConfig.value ? (personalConfigFormat.value === 'codex' ? 'Codex CLI' : 'Generic MCP JSON') : configPresetVerified.value ? `${clientMeta(configClientType.value).label} 配置` : `${clientMeta(configClientType.value).label} · Generic MCP 模板`)
const jsonConfig = computed(() => personalConfig.value ? (personalConfigFormat.value === 'codex' ? personalCodexConfig(publicUrl.value) : personalMcpConfig(publicUrl.value, token.value)) : stringifyMCPConfig(publicUrl.value, token.value, configSpaces.value, effectiveConfigClient.value))
const clientMeta = (value: MCPClientType) => clients.find(client => client.value === value) || clients[4]
const capabilityLabel = (value: string) => capabilityOptions.find(item => item.value === value)?.label || value
const statusLabel = (value: ReturnType<typeof getCredentialLifecycleState>) => ({ active: '已启用', expired: '已过期', revoked: '已停用' }[value])
const ownedRef = (id: string) => `owned:${id}`, sharedRef = (id: string, shareID: string) => `shared:${id}:${shareID}`
function ensureDraft(id: number) { return draftScopes.value[id] ||= { mode: 'all', selected: [] } }
function toggleTenant(id: number, checked: boolean) { ensureDraft(id); selectedTenantIds.value = checked ? [...new Set([...selectedTenantIds.value, id])] : selectedTenantIds.value.filter(item => item !== id) }
function toggleCapability(value: MCPCapability, checked: boolean) { capabilities.value = checked ? [...new Set([...capabilities.value, value])] : capabilities.value.filter(item => item !== value) }
function invalidRefs(tenantID: number) { const option = options.value.find(item => item.tenant_id === tenantID); const available = availableKBRefs(option); return ensureDraft(tenantID).selected.filter(value => { const [source, id, shareID = ''] = value.split(':'); return !available.has(`${source}:${id}:${shareID}`) }) }
function invalidRefLabel(value: string) { const [, id] = value.split(':'); return `知识库 #${id} · 已失效 / 无权限` }
function removeInvalidTenant(tenantID: number) { selectedTenantIds.value = selectedTenantIds.value.filter(id => id !== tenantID); delete draftScopes.value[tenantID] }
function removeInvalidRef(tenantID: number, value: string) { ensureDraft(tenantID).selected = ensureDraft(tenantID).selected.filter(item => item !== value) }
function decodeRef(value: string): MCPKBRef { const [source, id, shareID] = value.split(':'); return source === 'shared' ? { knowledge_base_id: id, source_type: 'shared', kb_share_id: shareID } : { knowledge_base_id: id, source_type: 'owned' } }
function payloadScopes() { return selectedTenantIds.value.map(tenant_id => ({ tenant_id, kb_scope_mode: ensureDraft(tenant_id).mode, knowledge_bases: ensureDraft(tenant_id).mode === 'selected' ? ensureDraft(tenant_id).selected.map(decodeRef) : [] })) }
function spacesForScopes(scopes: MCPAccessKey['tenant_scopes']) { return scopes.map(scope => ({ tenantId: scope.tenant_id, tenantName: options.value.find(item => item.tenant_id === scope.tenant_id)?.tenant_name || String(scope.tenant_id) })) }
function resetEditor() { editingID.value = null; step.value = 0; name.value = ''; clientType.value = 'workbuddy'; capabilities.value = ['retrieve', 'chat']; selectedTenantIds.value = []; draftScopes.value = {}; expiryMode.value = 'never'; customExpiry.value = '' }
function openPersonalCreate() {
  resetEditor()
  personalEditingID.value = null
  personalRange.value = 'default'
  privateSelections.value = Object.fromEntries(options.value.map(option => [option.tenant_id, []]))
  const home = Number(auth.user?.tenant_id)
  if (!home || !options.value.some(option => option.tenant_id === home)) return void MessagePlugin.warning('请先确认当前账号具有有效的个人空间')
  capabilities.value = ['retrieve', 'enterprise_retrieve']
  clientType.value = 'generic'
  selectedTenantIds.value = options.value.map(option => option.tenant_id)
  for (const tenant of selectedTenantIds.value) ensureDraft(tenant).mode = 'all'
  personalCreateVisible.value = true
}
async function savePersonal() {
  if (saving.value) return
  const error = validation(0) || validation(4)
  if (error) return void MessagePlugin.warning(error)
  let range: ReturnType<typeof personalKnowledgeRangePayload>
  try { range = personalKnowledgeRangePayload(personalRange.value, privateSelections.value, options.value) }
  catch (error) { return void MessagePlugin.warning(error instanceof Error ? error.message : '请选择有效的知识范围') }
  saving.value = true
  try {
    if (personalEditingID.value) {
      await updateMCPAccessKey(personalEditingID.value, { name: name.value.trim(), tenant_scopes: range.tenant_scopes, ...expiryPayload() })
      personalCreateVisible.value = false
      await load()
      MessagePlugin.success('指定范围已更新')
      return
    }
    const response = await createMCPAccessKey({ ...range, name: name.value.trim(), client_type: 'generic', ...expiryPayload() })
    if (!response.data?.token) throw new Error('服务端未返回 Secret')
    personalCreateVisible.value = false
    showSecret(response.data)
    await load()
  } catch (error) { MessagePlugin.error(error instanceof Error ? error.message : '创建失败') }
  finally { saving.value = false }
}
function personalRangeLabel(key: MCPAccessKey) { return key.personal_scope_mode === 'dynamic_all' ? '默认范围' : key.capabilities.includes('enterprise_retrieve') ? '历史固定范围' : '指定范围' }
function openEdit(key: MCPAccessKey) {
  if (key.personal_scope_mode === 'dynamic_all') return void MessagePlugin.info('默认范围随当前授权自动更新，无需配置空间')
  if (isUnifiedPersonalKey(key) && !key.capabilities.includes('enterprise_retrieve')) {
    resetEditor(); personalEditingID.value = key.id; personalRange.value = 'selected'; name.value = key.name
    privateSelections.value = Object.fromEntries(options.value.map(option => [option.tenant_id, []]))
    for (const scope of key.tenant_scopes) {
      const option = options.value.find(item => item.tenant_id === scope.tenant_id)
      privateSelections.value[scope.tenant_id] = scope.kb_scope_mode === 'all'
        ? option ? [...option.owned_knowledge_bases.map(kb => ownedRef(kb.id)), ...option.shared_knowledge_bases.map(share => sharedRef(share.knowledge_base.id, share.share_id))] : []
        : scope.knowledge_bases.map(kb => kb.source_type === 'owned' ? ownedRef(kb.knowledge_base_id) : sharedRef(kb.knowledge_base_id, kb.kb_share_id || ''))
    }
    expiryMode.value = key.expires_at ? 'custom' : 'never'; customExpiry.value = key.expires_at ? toDatetimeLocal(key.expires_at) : ''
    detailsVisible.value = false; personalCreateVisible.value = true; return
  }
  if (isUnifiedPersonalKey(key)) { scopeKey.value = key; scopeAdditions.value = []; detailsVisible.value = false; privateScopeVisible.value = true; return }
  const draft = credentialToEditDraft(key); resetEditor(); editingID.value = draft.id; name.value = draft.name; clientType.value = draft.clientType; capabilities.value = draft.capabilities; selectedTenantIds.value = draft.selectedTenantIds; draftScopes.value = draft.scopes; expiryMode.value = draft.expiryMode; customExpiry.value = draft.customExpiry; detailsVisible.value = false; editorVisible.value = true
}
async function savePrivateScope() {
  if (!scopeKey.value || !scopeAdditions.value.length) return
  privateScopeSaving.value = true
  try {
    await updateMCPAccessKey(scopeKey.value.id, { tenant_scopes: personalPrivateScopeUpdate(scopeKey.value.tenant_scopes, scopeAdditions.value, options.value) })
    privateScopeVisible.value = false
    await load()
    MessagePlugin.success('私人空间范围已更新，原有知识库限制保持不变')
  } catch (error) { MessagePlugin.error(error instanceof Error ? error.message : '更新失败') }
  finally { privateScopeSaving.value = false }
}
function validation(index: number) { if (index === 0 && !name.value.trim()) return '请填写接入名称'; if (index === 1 && !selectedTenantIds.value.length) return '请至少选择一个空间'; if (index === 1 && invalidTenantIds.value.length) return '请先移除已失效或无权限的空间'; if (index === 2 && selectedTenantIds.value.some(id => ensureDraft(id).mode === 'selected' && !ensureDraft(id).selected.length)) return '“仅指定”的空间必须至少选择一个知识库'; if (index === 2 && selectedTenantIds.value.some(id => invalidRefs(id).length)) return '请先移除已失效或无权限的知识库'; if (index === 3 && !capabilities.value.length) return '请至少选择一项能力'; if (index === 4 && expiryMode.value === 'custom' && (!customExpiry.value || new Date(customExpiry.value).getTime() <= Date.now())) return '请选择未来的有效期'; return '' }
function nextStep() { const error = validation(step.value); if (error) MessagePlugin.warning(error); else step.value++ }
function goToStep(index: number) { if (index <= step.value) step.value = index }
function expiryPayload() { if (expiryMode.value === 'never') return { never_expires: true as const }; if (expiryMode.value === 'custom') return { expires_at_unix: Math.floor(new Date(customExpiry.value).getTime() / 1000) }; return { expires_at_unix: Math.floor(Date.now() / 1000) + 90 * 86400 } }
async function save() { const error = validation(4); if (error) return void MessagePlugin.warning(error); if (!editingID.value) return; saving.value = true; try { const payload = { name: name.value.trim(), client_type: clientType.value, capabilities: capabilities.value, tenant_scopes: payloadScopes(), ...expiryPayload() }; await updateMCPAccessKey(editingID.value, payload); editorVisible.value = false; await load(); MessagePlugin.success('接入配置已更新') } catch (error) { MessagePlugin.error(error instanceof Error ? error.message : '保存失败') } finally { saving.value = false } }
function showSecret(data: MCPAccessKey & { token: string; mcp_public_url?: string }) { connectionRequest++; revealingSecret.value = false; personalConfig.value = isUnifiedPersonalKey(data); selectedKey.value = data; personalConnection.value = ''; token.value = data.token; publicUrl.value = data.mcp_public_url || publicUrl.value; configSpaces.value = spacesForScopes(data.tenant_scopes); configClientType.value = data.client_type; configVisible.value = true }
function openDetails(key: MCPAccessKey) { selectedKey.value = key; detailsVisible.value = true }
async function showTemplate(key: MCPAccessKey) {
  const request = ++connectionRequest
  personalConfig.value = isUnifiedPersonalKey(key); personalConnection.value = ''; selectedKey.value = key; token.value = TOKEN_PLACEHOLDER; configSpaces.value = spacesForScopes(key.tenant_scopes); configClientType.value = key.client_type; configVisible.value = true
  if (!key.secret_available) return
  revealingSecret.value = true
  try {
    const response = await revealMCPAccessKey(key.id)
    if (request === connectionRequest && configVisible.value) {
      if (!response.data?.token) throw new Error('未能读取 Secret')
      token.value = response.data.token
    }
  } catch (error) { if (request === connectionRequest) MessagePlugin.error(error instanceof Error ? error.message : '密钥当前不可查看，请刷新凭证状态后重试') }
  finally { if (request === connectionRequest) revealingSecret.value = false }
}
function clearPlaintextToken() { connectionRequest++; revealingSecret.value = false; token.value = ''; configSpaces.value = [] }
function requestRotate(key: MCPAccessKey) { rotateKey.value = key; rotateExpiryMode.value = lifecycleState(key) === 'expired' ? 'custom' : 'preserve'; rotateCustomExpiry.value = ''; rotateVisible.value = true }
async function confirmRotate() { if (!rotateKey.value) return; if (rotateExpiryMode.value === 'custom' && (!rotateCustomExpiry.value || new Date(rotateCustomExpiry.value).getTime() <= Date.now())) return void MessagePlugin.warning('请选择未来的有效期'); const payload = rotateExpiryMode.value === 'never' ? { never_expires: true as const } : rotateExpiryMode.value === 'custom' ? { expires_at_unix: Math.floor(new Date(rotateCustomExpiry.value).getTime() / 1000) } : {}; rotating.value = true; try { const response = await rotateMCPAccessKey(rotateKey.value.id, payload); if (!response.data?.token) throw new Error('服务端未返回一次性 Secret'); showSecret(response.data); rotateVisible.value = false; detailsVisible.value = false; await load(); MessagePlugin.success('Secret 已重新生成，旧 Secret 已立即失效') } catch (error) { MessagePlugin.error(error instanceof Error ? error.message : '重新生成 Secret 失败') } finally { rotating.value = false } }
async function revoke(id: number) { try { await revokeMCPAccessKey(id); detailsVisible.value = false; await load(); MessagePlugin.success('接入已停用，旧 Secret 已永久失效') } catch { MessagePlugin.error('停用失败') } }
const reconnectUsesDefaultExpiry = computed(() => { const value = reconnectKey.value?.expires_at; if (value === undefined) return false; if (typeof value !== 'string') return true; const expiry = new Date(value).getTime(); return !Number.isFinite(expiry) || expiry <= Date.now() })
function requestReconnect(key: MCPAccessKey) { reconnectKey.value = key; reconnectVisible.value = true; detailsVisible.value = false }
async function confirmReconnect() { if (!reconnectKey.value) return; reconnecting.value = true; try { const payload = credentialToReconnectPayload(reconnectKey.value); await createReconnectAndReveal(payload, createMCPAccessKey, data => { showSecret(data); reconnectVisible.value = false; reconnectKey.value = null }, load); MessagePlugin.success('已创建新的接入凭证，请立即保存 Secret') } catch (error) { MessagePlugin.error(error instanceof Error ? error.message : '重新接入失败，请进入创建流程重新选择授权范围') } finally { reconnecting.value = false } }
async function load() { loading.value = true; loadError.value = ''; try { const [keyResponse, optionResponse] = await Promise.all([listMCPAccessKeys(), getMCPAccessKeyScopeOptions()]); keys.value = keyResponse.data || []; options.value = optionResponse.data || []; publicUrl.value = optionResponse.mcp_public_url || keyResponse.mcp_public_url || '' } catch (error) { loadError.value = error instanceof Error ? error.message : '请检查网络后重试' } finally { loading.value = false } }
function actionOptions(key: MCPAccessKey) { return credentialLifecycleActions(key).map(action => action === 'edit' ? { content: '修改范围', value: action } : action === 'rotate' ? { content: '重新生成 Secret', value: action } : action === 'reconnect' ? { content: '重新接入', value: action } : { content: '停用接入', value: action, theme: 'error' }) }
function handleAction(event: { value: string }, key: MCPAccessKey) { if (event.value === 'edit') openEdit(key); else if (event.value === 'rotate') requestRotate(key); else if (event.value === 'reconnect') requestReconnect(key); else if (event.value === 'revoke') openDetails(key) }
function knowledgeBaseCount(key: MCPAccessKey) { return key.tenant_scopes.reduce((sum, scope) => { if (scope.kb_scope_mode === 'selected') return sum + scope.knowledge_bases.length; const option = options.value.find(item => item.tenant_id === scope.tenant_id); return sum + (option ? option.owned_knowledge_bases.length + option.shared_knowledge_bases.length : 0) }, 0) }
function scopeSummary(key: MCPAccessKey) { if (key.personal_scope_mode === 'dynamic_all') return '默认范围（随当前授权更新）'; const invalid = invalidCredentialScopes(key, options.value); return key.tenant_scopes.map(scope => { const option = options.value.find(item => item.tenant_id === scope.tenant_id); if (invalid.invalidTenantIds.includes(scope.tenant_id)) return `空间 #${scope.tenant_id}（已失效 / 无权限）`; const invalidCount = invalid.invalidKBRefs[scope.tenant_id]?.length || 0; const range = scope.kb_scope_mode === 'all' ? '全部已授权知识库' : `${scope.knowledge_bases.length} 个知识库${invalidCount ? `，${invalidCount} 个已失效` : ''}`; return `${option?.tenant_name || scope.tenant_id}（${range}）` }).join('、') }
function formatTime(value: string) { return new Date(value).toLocaleString() } function formatDate(value: string) { return new Date(value).toLocaleDateString() }
function relativeTime(value?: string) { if (!value) return '尚未使用'; const delta = Date.now() - new Date(value).getTime(); if (delta < 60_000) return '刚刚'; if (delta < 86400_000) return `${Math.max(1, Math.floor(delta / 3600_000))} 小时前`; return formatDate(value) }
function toDatetimeLocal(value: string) { const date = new Date(value); return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16) }
async function copySensitive(value: string) { await copyWithToast(value, 'common.copied'); if (token.value && token.value !== TOKEN_PLACEHOLDER && value.includes(token.value)) MessagePlugin.warning('配置包含敏感凭证，请仅粘贴到可信 AI 工具中') }
async function copyURL() { if (unifiedURL.value) await copyWithToast(unifiedURL.value, 'common.copied') }
async function copySpace(space: MCPConfigSpace) { await copySensitive(JSON.stringify(buildMCPServers(publicUrl.value, token.value, [space], effectiveConfigClient.value), null, 2)) }
watch(configVisible, visible => { if (!visible) clearPlaintextToken() }); onBeforeUnmount(clearPlaintextToken); onMounted(load)
</script>

<style scoped lang="less">
.mcp-access-center{min-height:100%;padding:34px clamp(24px,4vw,64px) 56px;background:linear-gradient(180deg,#f6faf8,#fff 260px);color:var(--td-text-color-primary)}.page-header{display:flex;align-items:flex-start;justify-content:space-between;gap:24px;max-width:1320px;margin:0 auto 22px}.page-header h1{margin:4px 0 8px;font-size:30px}.page-header p,.intro p{margin:0;color:#5e6e68}.eyebrow{display:flex;align-items:center;gap:7px;color:#087f5b;font-size:var(--app-text-sm);font-weight:700;letter-spacing:.1em}.security-note,.credential-surface{max-width:1320px;margin-inline:auto;border:1px solid #e1eae7;background:#fff;box-shadow:0 10px 28px rgba(15,23,42,.045)}.security-note{display:flex;align-items:center;gap:12px;margin-bottom:16px;padding:14px 18px;border-radius:var(--app-radius-xl)}.security-note>span,.empty-mark{display:grid;width:36px;height:36px;place-items:center;border-radius:var(--app-radius-lg);background:#e9f7f1;color:#087f5b;font-size:19px}.security-note p{margin:2px 0 0;color:#66756f;font-size:var(--app-text-md)}.url-warning{max-width:1320px;margin:0 auto 16px}.credential-surface{padding:22px;border-radius:16px}.section-heading,.section-heading>div{display:flex;align-items:center;justify-content:space-between;gap:10px}.section-heading{margin-bottom:18px}.section-heading h2{margin:0;font-size:var(--app-text-2xl)}.section-heading span{padding:3px 8px;border-radius:999px;background:#f0f5f3;color:#60716b;font-size:var(--app-text-sm)}.credential-list{display:grid;gap:12px}.credential-card{display:grid;grid-template-columns:minmax(210px,1.2fr) minmax(210px,1.3fr) minmax(160px,.9fr) minmax(240px,1.2fr) auto;align-items:center;gap:18px;padding:18px;border:1px solid #e3e9e7;border-radius:13px;background:#fff;box-shadow:0 3px 12px rgba(15,23,42,.035);transition:.18s}.credential-card:hover{border-color:#cbd8d3;background:#fcfefd;box-shadow:0 8px 22px rgba(15,23,42,.065)}.identity{display:flex;align-items:center;gap:11px;min-width:0}.client-mark{display:grid;flex:0 0 42px;width:42px;height:42px;place-items:center;border-radius:11px;background:linear-gradient(145deg,#e8f7f1,#f3faf7);color:#087f5b;font-size:21px}.identity>div{min-width:0}.identity h3{overflow:hidden;margin:0 0 4px;text-overflow:ellipsis;white-space:nowrap;font-size:var(--app-text-lg)}.identity p{display:flex;align-items:center;gap:6px;margin:0;color:#65736e;font-size:var(--app-text-sm)}.identity i{width:6px;height:6px;border-radius:50%;background:#94a3b8}.identity i.is-active{background:#10b981}.identity i.is-expired{background:#f59e0b}.scope{display:flex;min-width:0;flex-direction:column;gap:5px}.scope strong{font-size:var(--app-text-base)}.scope span{overflow:hidden;color:#66756f;font-size:var(--app-text-sm);text-overflow:ellipsis;white-space:nowrap}.capabilities{display:flex;flex-wrap:wrap;gap:6px}.capabilities span{padding:4px 8px;border:1px solid #d9e6e1;border-radius:var(--app-radius-sm);background:#f6faf8;color:#315c4d;font-size:var(--app-text-sm)}.meta{display:grid;grid-template-columns:repeat(3,minmax(70px,1fr));gap:10px;margin:0}.meta dt{margin-bottom:3px;color:#82908b;font-size:var(--app-text-xs)}.meta dd{overflow:hidden;margin:0;color:#34423d;font-size:var(--app-text-sm);text-overflow:ellipsis;white-space:nowrap}.token-hint{font-family:ui-monospace,monospace}.actions{display:flex;align-items:center;justify-content:flex-end;white-space:nowrap}.state-panel{display:flex;min-height:260px;align-items:center;justify-content:center;gap:10px;color:#66756f}.state-panel.column{flex-direction:column;text-align:center}.state-panel h3,.state-panel p{margin:0}.state-panel p{max-width:460px}.empty-mark{width:54px;height:54px;border-radius:16px;font-size:25px}.steps{display:grid;grid-template-columns:repeat(5,1fr);margin:-4px 0 24px;border-bottom:1px solid #e8eeec}.steps button{display:flex;align-items:center;justify-content:center;gap:6px;padding:0 4px 13px;border:0;border-bottom:2px solid transparent;background:transparent;color:#7b8883;font:12px inherit;cursor:pointer}.steps b{display:grid;width:20px;height:20px;place-items:center;border-radius:50%;background:#eef2f1;font-size:var(--app-text-xs)}.steps .active{border-color:#0f9f72;color:#086c50;font-weight:600}.steps .active b,.steps .done b{background:#daf3e9;color:#087f5b}.wizard-body{min-height:390px}.panel,.field{display:grid;gap:18px}.field{gap:8px}.field>label{font-size:var(--app-text-md);font-weight:600}.client-grid{display:grid;grid-template-columns:repeat(5,1fr);gap:10px}.client-grid button{display:flex;min-height:112px;flex-direction:column;align-items:flex-start;gap:6px;padding:14px;border:1px solid #dfe7e4;border-radius:11px;background:#fff;color:inherit;text-align:left;cursor:pointer}.client-grid button>.t-icon{color:#43806b;font-size:22px}.client-grid button span{color:#7b8883;font-size:var(--app-text-xs)}.client-grid button.selected{border-color:#51b793;background:#f0faf6;box-shadow:0 0 0 1px #51b793}.intro h3{margin:0 0 4px;font-size:var(--app-text-xl)}.intro p{font-size:var(--app-text-md)}.space-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:10px}.space-grid label,.capability-options label{display:flex;align-items:flex-start;gap:10px;padding:13px;border:1px solid #e0e8e5;border-radius:var(--app-radius-lg)}.space-grid label.selected,.capability-options label.selected{border-color:#89cbb4;background:#f5fbf8}.space-grid label>span,.capability-options label>span{display:flex;flex-direction:column;gap:3px}.space-grid small,.capability-options small{color:#74827d}.scope-list{display:grid;gap:12px;max-height:380px;overflow:auto}.scope-card{padding:14px;border:1px solid #e0e8e5;border-radius:11px}.scope-card header{display:flex;align-items:center;justify-content:space-between}.kb-groups{display:grid;grid-template-columns:1fr 1fr;gap:18px;margin-top:15px;padding-top:13px;border-top:1px solid #edf1f0}.kb-groups h4{margin:0 0 9px;font-size:var(--app-text-sm)}.kb-groups :deep(.t-checkbox-group){display:flex;flex-direction:column;gap:8px}.capability-options{display:grid;gap:10px}.expiry-card{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:20px;border:1px solid #dfe8e4;border-radius:var(--app-radius-xl);background:#fbfdfc}.native-datetime{height:36px;padding:0 11px;border:1px solid #cbd7d3;border-radius:7px;background:#fff;color:#34423d}.review{padding:16px;border-radius:var(--app-radius-lg);background:#f3f8f6}.review h4,.review p{margin:0 0 7px}.review p:last-child{margin:0;color:#66756f;font-size:var(--app-text-md)}.wizard-footer{display:flex;align-items:center;gap:8px;margin-top:24px;padding-top:16px;border-top:1px solid #e8eeec}.wizard-footer span{flex:1}.details{display:grid;gap:22px}.details dl{display:grid;margin:0;border:1px solid #e3e9e7;border-radius:var(--app-radius-lg)}.details dl div{display:grid;grid-template-columns:110px 1fr;gap:14px;padding:12px 14px;border-bottom:1px solid #edf1f0}.details dl div:last-child{border:0}.details dt{color:#75837e}.details dd{margin:0}.detail-actions{display:flex;justify-content:flex-end;gap:8px}.token-row{display:flex;align-items:center;justify-content:space-between;gap:12px;margin:14px 0;padding:12px;border:1px solid #dce6e2;border-radius:9px;background:#f8fbfa}.config-heading{display:flex;justify-content:space-between;margin:18px 0 8px}.config-heading span{color:#74827d;font-size:var(--app-text-sm)}pre{max-height:340px;overflow:auto;padding:16px;border:1px solid #e1e8e5;border-radius:9px;background:#f6f9f8;white-space:pre-wrap}.space-copy{display:flex;align-items:center;flex-wrap:wrap;gap:8px;margin-top:13px}@media(max-width:1180px){.credential-card{grid-template-columns:1fr 1fr 1fr}.meta{grid-column:1/3}.actions{grid-column:3;grid-row:2}}@media(max-width:760px){.mcp-access-center{padding:22px 14px 40px}.page-header{flex-direction:column}.credential-surface{padding:14px}.credential-card{grid-template-columns:1fr}.meta,.actions{grid-column:auto;grid-row:auto}.actions{justify-content:flex-start}.client-grid{grid-template-columns:repeat(2,1fr)}.space-grid,.kb-groups{grid-template-columns:1fr}.steps button{font-size:0}.steps b{font-size:var(--app-text-xs)}.expiry-card{align-items:flex-start;flex-direction:column}}
.space-grid label>span,.capability-options label>span,.scope-card,.kb-groups>div{min-width:0}.space-grid label strong,.kb-groups label span,.scope-card header>strong{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.scope-card header{gap:12px}.invalid-scope{display:grid;gap:7px;padding:11px 13px;border:1px solid #f2cf9b;border-radius:9px;background:#fffaf0;color:#7c4a03;font-size:var(--app-text-sm)}.invalid-scope>div{display:flex;align-items:center;justify-content:space-between;gap:10px}.invalid-scope span{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.details dl div{grid-template-columns:110px minmax(0,1fr)}.details dd{overflow-wrap:anywhere}.rotate-options{display:grid;gap:13px;margin-top:18px}.rotate-options :deep(.t-radio-group){display:grid;gap:9px}.rotate-options p{margin:0;color:#a15c00;font-size:var(--app-text-sm)}
.mcp-access-center{box-sizing:border-box;width:100%;overflow-x:clip}.credential-surface{box-sizing:border-box}.credential-card>*{min-width:0}
.credential-filters{display:flex;gap:6px;margin:-4px 0 16px;padding:4px;border-radius:var(--app-radius-lg);background:#f3f7f5;width:max-content}.credential-filters button{display:flex;align-items:center;gap:7px;min-height:32px;padding:0 12px;border:0;border-radius:7px;background:transparent;color:#64736e;cursor:pointer}.credential-filters button span{color:#8a9893;font-size:var(--app-text-xs)}.credential-filters button.active{background:#fff;color:#176f55;box-shadow:0 1px 4px rgba(15,23,42,.08);font-weight:600}.state-panel.compact{min-height:180px}.clear-filter{border:0;background:transparent;color:#087f5b;cursor:pointer}.expiry-note{margin:14px 0 0;color:#8a5700;font-size:var(--app-text-md)}
@media(max-width:1480px){.mcp-access-center{padding-inline:clamp(20px,3vw,40px)}.credential-card{grid-template-columns:minmax(210px,1.15fr) minmax(220px,1.3fr) minmax(160px,.85fr);grid-template-areas:"identity scope capabilities" "meta meta actions";gap:16px 20px}.identity{grid-area:identity}.scope{grid-area:scope}.capabilities{grid-area:capabilities}.meta{grid-area:meta}.actions{grid-area:actions}.meta{grid-column:auto}.actions{grid-column:auto;grid-row:auto}}
@media(max-width:980px){.credential-card{grid-template-columns:minmax(0,1fr) minmax(0,1fr);grid-template-areas:"identity scope" "capabilities capabilities" "meta meta" "actions actions"}.actions{justify-content:flex-start}.client-grid{grid-template-columns:repeat(3,minmax(0,1fr))}}
@media(max-width:760px){.credential-card{grid-template-areas:"identity" "scope" "capabilities" "meta" "actions"}.meta{grid-template-columns:1fr}.client-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}
.private-range-picker{display:grid;gap:12px;max-height:360px;overflow:auto;margin-top:16px}.private-range-picker :deep(.t-checkbox-group){display:flex;flex-direction:column;align-items:flex-start;gap:8px;margin-top:12px}.private-range-picker :deep(.t-checkbox__label){white-space:normal;overflow-wrap:anywhere}
</style>

<style scoped>
.mcp-access-center{background:var(--td-bg-color-page)}
.service-summary,.credential-surface{background:var(--td-bg-color-container);border-color:var(--td-component-border)}
.service-summary>div{flex:1;min-width:0}.service-summary strong{display:flex;align-items:center;flex-wrap:wrap;gap:12px}.service-summary small{color:var(--td-text-color-secondary);font-size:var(--app-text-sm);font-weight:400}
.service-url{display:flex;align-items:center;gap:12px;margin-top:14px}.service-url code{flex:1;min-width:0;overflow-wrap:anywhere;padding:12px;border:1px solid var(--td-component-border);border-radius:var(--app-radius-md);background:var(--td-bg-color-secondarycontainer);font-size:var(--app-text-md)}.service-footnote{font-size:var(--app-text-sm)!important}
.connect-steps{display:flex;gap:28px;max-width:1320px;margin:20px auto;color:var(--td-text-color-secondary);font-size:var(--app-text-md)}.connect-steps span{display:flex;align-items:center;gap:8px}.connect-steps b{display:grid;place-items:center;width:24px;height:24px;border-radius:50%;background:var(--td-brand-color-light);color:var(--td-brand-color)}
.credential-card.personal-card{grid-template-columns:minmax(180px,1fr) minmax(250px,1.3fr) auto;grid-template-areas:"identity meta actions";background:var(--td-bg-color-container)}.personal-card .identity{grid-area:identity}.personal-card .meta{grid-area:meta}.personal-card .actions{grid-area:actions}
.legacy-access{margin-top:20px;border-top:1px solid var(--td-component-border);padding-top:18px}.legacy-access summary{cursor:pointer;font-weight:600}.legacy-access>p,.personal-empty{color:var(--td-text-color-secondary);font-size:var(--app-text-md)}.legacy-access article{margin-top:12px}
@media(max-width:980px){.credential-card.personal-card{grid-template-columns:1fr;grid-template-areas:"identity" "meta" "actions"}.personal-card .actions{justify-content:flex-start;flex-wrap:wrap}.connect-steps{gap:14px;flex-wrap:wrap}}
@media(max-width:760px){.service-url{align-items:stretch;flex-direction:column}.service-summary{align-items:flex-start}.token-row{align-items:stretch;flex-direction:column}.token-row code{overflow-wrap:anywhere}.connect-steps{font-size:var(--app-text-sm);gap:10px}}
.private-range-picker{display:grid;gap:12px;max-height:360px;overflow:auto;margin-top:16px}.private-range-picker :deep(.t-checkbox-group){display:flex;flex-direction:column;align-items:flex-start;gap:8px;margin-top:12px}.private-range-picker :deep(.t-checkbox__label){white-space:normal;overflow-wrap:anywhere}
</style>
