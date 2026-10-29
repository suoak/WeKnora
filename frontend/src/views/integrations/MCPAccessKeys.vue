<template>
  <main class="mcp-access-center">
    <header class="page-header">
      <div><h1>MCP 接入</h1><p>将 KnowHub 企业知识连接到常用 AI 工具。</p></div>
      <t-button theme="primary" :disabled="loading || !!loadError" @click="openPersonalCreate"><template #icon><t-icon name="add" /></template>创建个人 Key</t-button>
    </header>
    <t-alert v-if="!publicUrl && !loading" theme="warning" class="url-warning">服务地址尚未配置，请联系管理员。你仍可管理已有凭证。</t-alert>
    <section class="credential-surface personal-surface" aria-labelledby="personal-title">
      <div class="section-heading"><div class="list-heading"><h2 id="personal-title">我的个人 MCP Key</h2><span v-if="!loading && !loadError" class="secondary-copy">{{ personalListState.summary }}</span></div><t-button variant="text" :loading="loading" @click="load"><t-icon name="refresh" /> 刷新</t-button></div>
      <div v-if="loading" class="state-panel"><t-loading />正在加载个人 Key…</div>
      <t-alert v-else-if="loadError" theme="error">{{ loadError }} <t-button variant="text" @click="load">重试</t-button></t-alert>
      <template v-else-if="personalKeys.length">
        <div v-if="personalListState.hasMany" class="list-filters"><t-tabs v-if="personalListState.multipleStates" v-model="personalStatusFilter" class="status-tabs" size="medium" aria-label="个人 Key 状态筛选"><t-tab-panel v-for="filter in personalListState.filters" :key="filter.value" :value="filter.value" :label="`${filter.label} ${filter.count}`" /></t-tabs><t-input v-model="personalSearch" clearable placeholder="搜索 Key 名称" aria-label="搜索个人 Key"><template #prefix-icon><t-icon name="search" /></template></t-input></div>
        <div v-if="!filteredPersonalKeys.length" class="state-panel">当前筛选下没有个人 Key</div>
        <div class="credential-list personal-list">
          <article v-for="key in pagedPersonalKeys" :key="key.id" class="credential-card personal-key-card" :data-key-id="key.id">
            <div class="identity"><span class="client-mark"><t-icon name="key" /></span><div><h3>{{ key.name }}</h3><t-tag :theme="lifecycleState(key) === 'active' ? 'success' : lifecycleState(key) === 'expired' ? 'warning' : 'default'" variant="light">{{ statusLabel(lifecycleState(key)) }}</t-tag></div></div>
            <div class="scope"><strong>{{ personalRangeLabel(key) }}</strong><span>{{ scopeSummary(key) }}</span></div>
            <dl class="meta"><div><dt>有效期</dt><dd>{{ key.expires_at ? formatTime(key.expires_at) : '永不过期' }}</dd></div><div><dt>最近使用</dt><dd>{{ relativeTime(key.last_used_at) }}</dd></div></dl>
            <div class="actions"><t-button variant="outline" @click="openDetails(key)">管理</t-button><t-button theme="primary" :disabled="lifecycleState(key) !== 'active' || !publicUrl" @click="openConnect(key)">连接 AI 工具</t-button></div>
          </article>
        </div>
        <t-pagination v-if="filteredPersonalKeys.length > credentialPageSize" v-model="personalPage" :page-size="credentialPageSize" :total="filteredPersonalKeys.length" :show-page-size="false" :show-jumper="false" />
      </template>
      <div v-else class="state-panel column"><t-icon name="key" /><strong>暂无个人 Key</strong><p>创建一次，即可用于多个 AI 客户端。</p><t-button theme="primary" @click="openPersonalCreate">创建个人 Key</t-button></div>
      <p class="secondary-copy scope-explanation">默认范围随本人当前授权更新；历史固定范围仅限原凭证中的授权，不会自动扩展。当前空间不等于个人 MCP 的完整授权范围。凭证有效不代表客户端已连接。</p>
    </section>
    <section v-if="historyKeys.length" class="credential-surface history-surface">
      <t-button variant="text" :aria-expanded="historyExpanded" aria-controls="mcp-history" @click="historyExpanded = !historyExpanded"><template #icon><t-icon :name="historyExpanded ? 'chevron-down' : 'chevron-right'" /></template>历史接入 · {{ historyKeys.length }} 个凭证</t-button>
      <div v-if="historyExpanded" id="mcp-history">
        <p class="secondary-copy">仅展示旧版客户端凭证，可继续查看、修改范围、轮换及停用。个人 Key 已在上方独立展示。</p>
        <t-tabs v-if="historyListState.multipleStates" v-model="statusFilter" class="status-tabs" size="medium" aria-label="凭证状态筛选"><t-tab-panel v-for="filter in historyListState.filters" :key="filter.value" :value="filter.value" :label="`${filter.label} ${filter.count}`" /></t-tabs>
        <div v-if="loadError" class="state-panel column"><strong>加载失败</strong><p>{{ loadError }}</p><t-button variant="outline" @click="load">重试</t-button></div>
        <div v-else-if="!filteredKeys.length" class="state-panel">当前筛选下没有接入凭证</div>
        <div v-else class="credential-list">
          <article v-for="key in pagedKeys" :key="key.id" class="credential-card">
            <div class="identity"><span class="client-mark"><t-icon :name="isUnifiedPersonalKey(key) ? 'key' : clientMeta(key.client_type).icon" /></span><div><h3>{{ key.name }}</h3><p>{{ isUnifiedPersonalKey(key) ? '个人 Key' : clientMeta(key.client_type).label }} · {{ statusLabel(lifecycleState(key)) }}</p></div></div>
            <div class="scope"><strong>{{ personalRangeLabel(key) }}</strong><span>{{ scopeSummary(key) }}</span></div>
            <dl class="meta"><div><dt>最近使用</dt><dd>{{ relativeTime(key.last_used_at) }}</dd></div><div><dt>有效期</dt><dd>{{ key.expires_at ? formatDate(key.expires_at) : '永不过期' }}</dd></div><div><dt>凭证</dt><dd class="token-hint">{{ key.token_hint || '未记录' }}</dd></div></dl>
            <div class="actions"><t-button v-if="lifecycleState(key) !== 'revoked'" variant="outline" :disabled="!publicUrl || lifecycleState(key) !== 'active'" @click="openHistoryConfig(key)">兼容配置</t-button><t-button variant="text" @click="openDetails(key)">管理</t-button></div>
          </article>
        </div>
        <t-pagination v-if="filteredKeys.length > credentialPageSize" v-model="historyPage" :page-size="credentialPageSize" :total="filteredKeys.length" :show-page-size="false" :show-jumper="false" />
      </div>
    </section>

    <t-dialog dialog-class-name="mcp-access-dialog" :dialog-style="dialogBounds" top="16px" v-model:visible="personalCreateVisible" :header="personalEditingID ? '修改指定范围' : '创建个人 Key'" width="min(640px, 94vw)" :footer="false">
      <p>填写名称、有效期和知识范围，保存后即可复制连接配置。</p>
      <div class="field"><label>接入名称</label><t-input v-model="name" maxlength="128" placeholder="例如：个人知识检索" :disabled="saving" /></div>
      <div class="field"><label>有效期</label><t-radio-group v-model="expiryMode" :disabled="saving"><t-radio value="90">90 天</t-radio><t-radio value="custom">指定日期</t-radio><t-radio value="never">永不过期</t-radio></t-radio-group><input v-if="expiryMode === 'custom'" v-model="customExpiry" class="native-datetime" type="datetime-local" :disabled="saving" /></div>
      <div class="field"><label>知识范围</label><t-radio-group v-model="personalRange" :disabled="saving || !!personalEditingID"><t-radio value="default">默认范围</t-radio><t-radio value="selected">指定范围</t-radio></t-radio-group></div>
      <p v-if="personalRange === 'default'">自动使用当前可用的知识范围。</p>
      <section v-else class="private-range-picker" aria-label="选择私人知识库"><p>仅检索所选且当前有权读取的私人知识。新增授权不会自动扩大此 Key 的指定范围。</p><section v-for="option in selectablePrivateOptions" :key="option.tenant_id" class="scope-card"><header><strong>{{ option.tenant_name }}</strong></header><t-checkbox-group v-model="privateSelections[option.tenant_id]" :disabled="saving"><t-checkbox v-for="kb in option.owned_knowledge_bases" :key="ownedRef(kb.id)" :value="ownedRef(kb.id)">{{ kb.name }}</t-checkbox><t-checkbox v-for="share in option.shared_knowledge_bases" :key="sharedRef(share.knowledge_base.id, share.share_id)" :value="sharedRef(share.knowledge_base.id, share.share_id)">{{ share.knowledge_base.name }} · 共享给此空间</t-checkbox></t-checkbox-group></section><t-button size="small" variant="text" :disabled="saving" @click="privateSelections = {}">清空选择</t-button><p v-if="!selectablePrivateOptions.length">当前没有可选的私人知识库。</p></section>
      <footer class="wizard-footer"><t-button variant="outline" :disabled="saving" @click="personalCreateVisible = false">取消</t-button><span /><t-button theme="primary" :loading="saving" @click="savePersonal">{{ personalEditingID ? '保存修改' : '创建个人 Key' }}</t-button></footer>
    </t-dialog>

    <t-dialog dialog-class-name="mcp-access-dialog" :dialog-style="dialogBounds" top="16px" v-model:visible="privateScopeVisible" header="更新私人空间范围" width="min(600px, 94vw)" :confirm-btn="{ content: '添加选中的私人空间', disabled: !scopeAdditions.length, loading: privateScopeSaving }" @confirm="savePrivateScope">
      <p>新加入的私人空间需要你主动添加；原 Key 的知识库限制和有效期保持不变。</p>
      <p v-if="scopeKey">当前私人范围：{{ scopeSummary(scopeKey) }}</p>
      <t-checkbox-group v-model="scopeAdditions"><t-checkbox v-for="option in newPrivateOptions" :key="option.tenant_id" :value="option.tenant_id">{{ option.tenant_name }}</t-checkbox></t-checkbox-group>
      <p v-if="!newPrivateOptions.length">没有可添加的新私人空间。</p>
    </t-dialog>

    <t-dialog dialog-class-name="mcp-access-dialog" :dialog-style="dialogBounds" top="16px" v-model:visible="editorVisible" header="历史接入设置" width="min(820px, 94vw)" :footer="false" @close="resetEditor">
      <div class="steps"><button v-for="(label, index) in wizardSteps" :key="label" type="button" :class="{ active: step === index, done: step > index }" @click="goToStep(index)"><b>{{ index + 1 }}</b>{{ label }}</button></div>
      <div class="wizard-body">
        <section v-if="step === 0" class="panel"><div class="field"><label>接入名称</label><t-input v-model="name" maxlength="128" placeholder="例如：Jerry - WorkBuddy" /></div><div class="field"><label>历史客户端</label><p>{{ clientMeta(clientType).label }}（保留原有兼容设置）</p></div></section>
        <section v-else-if="step === 1" class="panel"><div class="intro"><h3>选择空间</h3><p>只显示你当前拥有有效成员关系的空间。</p></div><div v-if="invalidTenantIds.length" class="invalid-scope"><strong>以下原授权空间已失效或无权限</strong><div v-for="tenantID in invalidTenantIds" :key="tenantID"><span>空间 #{{ tenantID }} · 已失效 / 无权限</span><t-button size="small" variant="text" @click="removeInvalidTenant(tenantID)">从凭证范围移除</t-button></div></div><div class="space-grid"><label v-for="option in options" :key="option.tenant_id" :class="{ selected: selectedTenantIds.includes(option.tenant_id) }"><t-checkbox :checked="selectedTenantIds.includes(option.tenant_id)" @change="toggleTenant(option.tenant_id, Boolean($event))" /><span><strong :title="option.tenant_name">{{ option.tenant_name }}</strong><small>{{ option.owned_knowledge_bases.length }} 个本空间知识库</small></span></label></div></section>
        <section v-else-if="step === 2" class="panel"><div class="intro"><h3>设置每个空间的知识库范围</h3><p>“动态全部”会包含未来新增且你有权访问的知识库。</p></div><div class="scope-list"><section v-for="option in selectedOptions" :key="option.tenant_id" class="scope-card"><header><strong :title="option.tenant_name">{{ option.tenant_name }}</strong><t-radio-group v-model="ensureDraft(option.tenant_id).mode" variant="default-filled"><t-radio-button value="all">动态全部</t-radio-button><t-radio-button value="selected">仅指定</t-radio-button></t-radio-group></header><div v-if="invalidRefs(option.tenant_id).length" class="invalid-scope"><strong>原授权知识库已失效或无权限</strong><div v-for="refValue in invalidRefs(option.tenant_id)" :key="refValue"><span :title="refValue">{{ invalidRefLabel(refValue) }}</span><t-button size="small" variant="text" @click="removeInvalidRef(option.tenant_id, refValue)">移除</t-button></div></div><div v-if="ensureDraft(option.tenant_id).mode === 'selected'" class="kb-groups"><div><h4>本空间知识库</h4><t-checkbox-group v-model="ensureDraft(option.tenant_id).selected"><t-checkbox v-for="kb in option.owned_knowledge_bases" :key="ownedRef(kb.id)" :value="ownedRef(kb.id)"><span :title="kb.name">{{ kb.name }}</span></t-checkbox></t-checkbox-group></div><div v-if="option.shared_knowledge_bases.length"><h4>共享知识库</h4><t-checkbox-group v-model="ensureDraft(option.tenant_id).selected"><t-checkbox v-for="share in option.shared_knowledge_bases" :key="sharedRef(share.knowledge_base.id, share.share_id)" :value="sharedRef(share.knowledge_base.id, share.share_id)"><span :title="`${share.knowledge_base.name} · ${share.org_name}`">{{ share.knowledge_base.name }} · {{ share.org_name }}</span></t-checkbox></t-checkbox-group></div></div></section></div></section>
        <section v-else-if="step === 3" class="panel"><div class="intro"><h3>选择能力</h3><p>默认仅启用知识检索和知识问答，不授予高权限。</p></div><div class="capability-options"><label v-for="capability in capabilityOptions" :key="capability.value" :class="{ selected: capabilities.includes(capability.value) }"><t-checkbox :checked="capabilities.includes(capability.value)" @change="toggleCapability(capability.value, Boolean($event))" /><span><strong>{{ capability.label }}</strong><small>{{ capability.description }}</small></span></label></div></section>
        <section v-else class="panel"><div class="intro"><h3>设置有效期</h3><p>到期后凭证立即失效；遗失 Secret 时请执行轮换。</p></div><div class="expiry-card"><t-radio-group v-model="expiryMode"><t-radio value="90">90 天</t-radio><t-radio value="custom">指定日期</t-radio><t-radio value="never">永不过期</t-radio></t-radio-group><input v-if="expiryMode === 'custom'" v-model="customExpiry" class="native-datetime" type="datetime-local" /></div><div class="review"><h4>即将{{ editingID ? '更新' : '创建' }}</h4><p><strong>{{ name || '未命名接入' }}</strong> · {{ clientMeta(clientType).label }}</p><p>{{ selectedTenantIds.length }} 空间 · {{ capabilities.map(capabilityLabel).join('、') }}</p></div></section>
      </div>
      <footer class="wizard-footer"><t-button variant="outline" @click="editorVisible = false">取消</t-button><span /><t-button v-if="step > 0" variant="text" @click="step--">上一步</t-button><t-button v-if="step < 4" theme="primary" @click="nextStep">下一步</t-button><t-button v-else theme="primary" :loading="saving" @click="save">{{ editingID ? '保存修改' : '创建接入' }}</t-button></footer>
    </t-dialog>

    <t-drawer v-model:visible="detailsVisible" header="接入详情" size="min(680px, 94vw)" :footer="false"><div v-if="selectedKey" class="details"><div class="identity"><span class="client-mark"><t-icon :name="clientMeta(selectedKey.client_type).icon" /></span><div><h3 :title="selectedKey.name">{{ selectedKey.name }}</h3><p>{{ clientMeta(selectedKey.client_type).label }} · {{ statusLabel(lifecycleState(selectedKey)) }}</p></div></div><dl><div><dt>授权范围</dt><dd>{{ scopeSummary(selectedKey) }}</dd></div><div v-if="!isUnifiedPersonalKey(selectedKey)"><dt>历史能力</dt><dd>{{ selectedKey.capabilities.map(capabilityLabel).join('、') }}</dd></div><div><dt>最近使用</dt><dd>{{ relativeTime(selectedKey.last_used_at) }}</dd></div><div><dt>有效期</dt><dd>{{ selectedKey.expires_at ? formatTime(selectedKey.expires_at) : '永不过期' }}</dd></div><div><dt>Token Hint</dt><dd>{{ selectedKey.token_hint || '未记录' }}</dd></div></dl><div class="detail-actions"><template v-if="lifecycleState(selectedKey) !== 'revoked'"><t-button variant="outline" @click="openEdit(selectedKey)">修改范围</t-button><t-button variant="outline" @click="requestRotate(selectedKey)">重新生成 Secret</t-button><t-popconfirm content="停用后旧 Secret 将立即且永久失效，历史记录仍会保留。确认停用？" @confirm="revoke(selectedKey.id)"><t-button theme="danger" variant="text">停用接入</t-button></t-popconfirm></template><t-button v-else theme="primary" @click="requestReconnect(selectedKey)">重新接入</t-button></div></div></t-drawer>

    <t-dialog dialog-class-name="mcp-access-dialog" :dialog-style="dialogBounds" top="16px" v-model:visible="reconnectVisible" header="重新接入" width="min(560px, 94vw)" :confirm-btn="{ content: '确认重新接入', loading: reconnecting }" @confirm="confirmReconnect">
      <p>将基于原接入配置创建一个全新凭证。原凭证保持停用，旧 Secret 仍永久失效。</p>
      <t-alert theme="warning">系统会按原空间、知识库和能力范围提交，并重新执行实时权限校验；如权限已变化，创建会失败且不会缩减授权范围。</t-alert>
      <p v-if="reconnectUsesDefaultExpiry" class="expiry-note">原有效期已过期或不可用，新凭证将采用当前默认有效期。</p>
    </t-dialog>

    <t-dialog dialog-class-name="mcp-access-dialog" :dialog-style="dialogBounds" top="16px" v-model:visible="rotateVisible" header="重新生成 Secret" width="min(560px, 94vw)" :confirm-btn="{ content: '确认重新生成', loading: rotating }" @confirm="confirmRotate">
      <t-alert theme="warning">重新生成后，旧 Secret 将立即失效。其他客户端已使用此 Key 时，需要同步更新为新 Secret。</t-alert>
      <div class="rotate-options"><strong>有效期策略</strong><t-radio-group v-model="rotateExpiryMode"><t-radio v-if="rotateKey && lifecycleState(rotateKey) !== 'expired'" value="preserve">保持当前有效期</t-radio><t-radio value="custom">设置新的有效期</t-radio><t-radio value="never">永不过期</t-radio></t-radio-group><input v-if="rotateExpiryMode === 'custom'" v-model="rotateCustomExpiry" class="native-datetime" type="datetime-local" /><p v-if="rotateKey && lifecycleState(rotateKey) === 'expired'">该凭证已经过期，必须明确选择未来有效期或永不过期。</p></div>
    </t-dialog>

    <t-dialog dialog-class-name="mcp-access-dialog" :dialog-style="dialogBounds" top="16px" v-model:visible="configVisible" :header="personalConfig ? (configStage === 'clients' ? '连接 AI 工具' : `${currentClient.label} 接入配置`) : '历史客户端兼容配置'" width="min(820px, 94vw)" :footer="false" @close="clearPlaintextToken">
      <div class="config-body">
        <template v-if="connectionKey">
          <div class="connection-identity" :data-key-id="connectionKey.id"><strong>{{ connectionKey.name }}</strong><t-tag :theme="lifecycleState(connectionKey) === 'active' ? 'success' : 'warning'" variant="light">{{ statusLabel(lifecycleState(connectionKey)) }}</t-tag><span>Key #{{ connectionKey.id }}</span></div>
          <p class="secondary-copy">授权范围：{{ personalRangeLabel(connectionKey) }} · {{ scopeSummary(connectionKey) }}</p>
          <template v-if="personalConfig && configStage === 'clients'">
            <p class="secondary-copy">选择客户端，使用此 Key 获取配置。同一个 Key 可连接多个 AI 工具，无需重复创建。</p>
            <div class="client-grid client-picker"><button v-for="client in mcpClients" :key="client.value" :aria-label="client.label" type="button" :disabled="!canConnect" @click="openClient(client.value)"><img v-if="clientIcons[client.value]" :src="clientIcons[client.value]" class="client-product-icon" :class="{ 'monochrome-icon': client.value === 'copilot' }" alt="" /><t-icon v-else :name="client.icon" /><strong>{{ client.label }}</strong><span>{{ client.description }}</span><t-icon name="chevron-right" class="client-arrow" /></button></div>
          </template>
          <template v-else>
          <t-button v-if="personalConfig" variant="text" class="change-client" :disabled="revealingSecret || testingPersonal" @click="configStage = 'clients'; secretVisible = false; clearProtocolState()"><t-icon name="chevron-left" />更换客户端</t-button>
          <t-loading v-if="revealingSecret" text="正在读取个人 Secret" />
          <t-alert v-else theme="info">{{ connectionKey.secret_available || hasConfigSecret ? 'Secret 默认遮蔽，主动显示或复制时才读取。服务端加密保存，仅当前凭证本人可在此查看。' : '此 Key 未保存可恢复的 Secret。请使用已保存的 Secret 设置客户端，或在管理中重新生成；占位模板不是完整配置。' }}</t-alert>
          <t-alert v-if="safeConfiguration" theme="info">安全模板不含密钥，无需替换模板中的变量引用。{{ connectionKey.secret_available || hasConfigSecret ? '请用“复制 Secret”获取此 Key，按下方步骤设置环境变量或在客户端安全输入框中粘贴。' : '请使用已有 Secret 设置环境变量或客户端安全输入；若已遗失，请在管理中重新生成。' }}</t-alert>
          <t-input v-if="!personalConfig && !connectionKey.secret_available && !revealingSecret && token === TOKEN_PLACEHOLDER" v-model="suppliedSecret" type="password" autocomplete="off" placeholder="填入已保存的 Secret（仅本次使用）" aria-label="已保存的 Secret" />
          <div v-if="connectionKey.secret_available || hasConfigSecret" class="token-row"><code>{{ secretVisible ? configToken : '••••••••••••••••' }}</code><t-button size="small" variant="outline" :disabled="!canConnect || revealingSecret" @click="toggleSecret">{{ secretVisible ? '隐藏 Secret' : '显示 Secret' }}</t-button><t-button size="small" variant="outline" :disabled="!canConnect || revealingSecret" @click="copySecret">复制 Secret</t-button></div>
          <div class="config-heading"><strong>{{ personalConfig ? currentClient.location : configPresetTitle }}</strong><t-tag theme="warning" variant="light">参考配置 · 客户端待验证</t-tag></div>
          <pre v-if="publicUrl">{{ displayedConfig }}</pre>
          <div class="detail-actions"><t-button theme="primary" :disabled="!canConnect || revealingSecret" @click="copyConfiguration">{{ configurationCopyLabel }}</t-button><t-button v-if="personalConfig" variant="outline" :disabled="!canConnect || (!hasConfigSecret && !connectionKey.secret_available) || revealingSecret" :loading="testingPersonal" @click="testPersonalConnection">验证 MCP 协议</t-button><t-button variant="text" @click="configVisible = false; openDetails(connectionKey)">管理 Key</t-button></div>
          <ol v-if="personalConfig" class="config-steps"><li v-for="instruction in currentClient.steps" :key="instruction">{{ instruction }}</li><li v-if="configPreset === 'copilot' || configPreset === 'vscode'">启动时在密码输入框中粘贴个人 Secret。</li><li v-else-if="!safeConfiguration && !connectionKey.secret_available && !hasConfigSecret">此占位模板需要你已有的 Secret 才能使用；若已遗失，请在管理中重新生成。</li><li>请求 AI 检索一条你有权读取的知识，核对返回来源。实际检索成功后，才算完成客户端验收。</li></ol>
          <p v-else class="secondary-copy">保留历史配置格式、地址与空间请求头。请按原客户端方式导入并验证检索。</p>
          <t-alert v-if="personalConnection" :theme="personalProtocolPassed ? 'success' : 'warning'">{{ personalConnection }}</t-alert>
          <t-alert theme="info">凭证有效、MCP 协议通过和客户端实际调用是不同状态。本页协议验证只检查鉴权与工具发现。401 请检查 Secret 和有效期；403 请检查个人授权；连接失败请检查网络和服务地址。</t-alert>
          </template>
          <t-alert v-if="!publicUrl" theme="warning">服务地址尚未配置，当前配置不可运行。</t-alert>
          <t-alert v-if="lifecycleState(connectionKey) !== 'active'" theme="warning">此凭证已过期或停用，请在管理中处理后重新连接。</t-alert>
        </template>
      </div>
    </t-dialog>
  </main>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import { createMCPAccessKey, getMCPAccessKeyScopeOptions, listMCPAccessKeys, revokeMCPAccessKey, revealMCPAccessKey, rotateMCPAccessKey, updateMCPAccessKey, type KBScopeMode, type MCPAccessKey, type MCPCapability, type MCPClientType, type MCPKBRef, type MCPScopeOption } from '@/api/mcpAccessKeys'
import { copyWithToast } from '@/utils/clipboard'
import { cssViewportSize } from '@/utils/zoom'
import { stringifyMCPConfig, type MCPConfigSpace } from './mcpConfig'
import { availableKBRefs, invalidCredentialScopes } from './mcpAccessScope'
import { credentialToEditDraft } from './mcpAccessEditor'
import { credentialListState, credentialPageSize, clampCredentialPage } from './mcpCredentialList'
import { isUnifiedPersonalKey, personalKnowledgeRangePayload, personalPrivateScopeUpdate, type PersonalKnowledgeRange } from './personalMcpPresentation'
import { createReconnectAndReveal, credentialLifecycleActions, credentialMatchesStatusFilter, credentialToReconnectPayload, getCredentialLifecycleState, type CredentialStatusFilter } from './mcpAccessLifecycle'

import { mcpClients, personalClientConfiguration, usesSafeCredentialInput, type MCPClientPreset } from './mcpClientPresets'
import claudeIcon from '@/assets/mcp-clients/claude.svg'
import copilotIcon from '@/assets/mcp-clients/copilot.svg'
import piIcon from '@/assets/mcp-clients/pi.svg'
import workbuddyIcon from '@/assets/mcp-clients/workbuddy.png'
import vscodeIcon from '@/assets/mcp-clients/vscode.png'
const clientIcons: Partial<Record<MCPClientPreset, string>> = { claude: claudeIcon, copilot: copilotIcon, vscode: vscodeIcon, pi: piIcon, workbuddy: workbuddyIcon }
const auth = useAuthStore()
const dialogBounds = ref({ maxHeight: 'calc(100dvh - 32px)', maxWidth: 'calc(100vw - 32px)' })
let rootStyleObserver: MutationObserver | undefined
function updateDialogBounds() {
  const viewport = cssViewportSize()
  dialogBounds.value = { maxHeight: `${Math.max(0, viewport.height - 32)}px`, maxWidth: `${Math.max(0, viewport.width - 32)}px` }
}
onMounted(() => {
  updateDialogBounds()
  window.addEventListener('resize', updateDialogBounds)
  rootStyleObserver = new MutationObserver(updateDialogBounds)
  rootStyleObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['style'] })
})
onBeforeUnmount(() => { rootStyleObserver?.disconnect(); window.removeEventListener('resize', updateDialogBounds) })
const TOKEN_PLACEHOLDER = '<YOUR_MCP_KEY>'
interface DraftScope { mode: KBScopeMode; selected: string[] }
const clients: Array<{ value: MCPClientType; label: string; icon: string; description: string }> = [
  { value: 'workbuddy', label: 'WorkBuddy', icon: 'service', description: '企业研发 AI 助手' }, { value: 'workmate', label: 'WorkMate', icon: 'user-talk', description: '团队智能工作伙伴' }, { value: 'cursor', label: 'Cursor', icon: 'code', description: 'AI 编程客户端' }, { value: 'codebuddy', label: 'CodeBuddy', icon: 'terminal-rectangle', description: '代码研发助手' }, { value: 'generic', label: 'Generic MCP', icon: 'link', description: '其它 MCP 客户端' },
]
import { personalMcpURL, testPersonalMcpConnection } from './personalMcpConfig'
const personalConfig = ref(false), testingPersonal = ref(false), personalConnection = ref('')
const connectionKey = ref<MCPAccessKey | null>(null), configStage = ref<'clients' | 'config'>('clients'), secretVisible = ref(false)
const personalCreateVisible = ref(false)
const personalRange = ref<PersonalKnowledgeRange>('default'), personalEditingID = ref<number | null>(null)
const privateSelections = ref<Record<number, string[]>>({})
const revealingSecret = ref(false)
let connectionRequest = 0
const configPreset = ref<MCPClientPreset>('codex'), suppliedSecret = ref('')
const currentClient = computed(() => mcpClients.find(client => client.value === configPreset.value)!)
const configToken = computed(() => suppliedSecret.value.trim() || token.value)
const hasConfigSecret = computed(() => Boolean(configToken.value && configToken.value !== TOKEN_PLACEHOLDER))
const personalProtocolPassed = ref(false)
let protocolRequest = 0
async function testPersonalConnection() {
  if (!canConnect.value) return
  const request = ++protocolRequest
  testingPersonal.value = true; personalConnection.value = ''; personalProtocolPassed.value = false
  try {
    const secret = await ensureConnectionSecret()
    if (request !== protocolRequest || secret === TOKEN_PLACEHOLDER) return
    await testPersonalMcpConnection(personalMcpURL(publicUrl.value), secret)
    if (request === protocolRequest && configVisible.value) { personalProtocolPassed.value = true; personalConnection.value = 'MCP 协议通过：服务鉴权与只读工具发现成功；尚未验证客户端会话及知识检索。' }
  } catch (error) { if (request === protocolRequest && configVisible.value) personalConnection.value = error instanceof Error ? error.message : '连接失败' }
  finally { if (request === protocolRequest) testingPersonal.value = false }
}
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
const personalKeys = computed(() => keys.value.filter(isUnifiedPersonalKey))
const historyKeys = computed(() => keys.value.filter(key => !isUnifiedPersonalKey(key)))
const personalStatusFilter = ref<CredentialStatusFilter>('all'), personalSearch = ref(''), personalPage = ref(1)
const personalListState = computed(() => credentialListState(personalKeys.value))
const historyListState = computed(() => credentialListState(historyKeys.value))
const filteredPersonalKeys = computed(() => personalKeys.value.filter(key => !personalListState.value.hasMany || (credentialMatchesStatusFilter(key, personalStatusFilter.value) && key.name.toLocaleLowerCase().includes(personalSearch.value.trim().toLocaleLowerCase()))).sort((a, b) => Number(lifecycleState(b) === 'active') - Number(lifecycleState(a) === 'active')))
const pagedPersonalKeys = computed(() => filteredPersonalKeys.value.slice((personalPage.value - 1) * credentialPageSize, personalPage.value * credentialPageSize))
const filteredKeys = computed(() => historyKeys.value.filter(key => credentialMatchesStatusFilter(key, statusFilter.value)))
const historyExpanded = ref(false), historyPage = ref(1)
const pagedKeys = computed(() => filteredKeys.value.slice((historyPage.value - 1) * credentialPageSize, historyPage.value * credentialPageSize))
watch([statusFilter, () => keys.value], () => { historyPage.value = 1 })
watch([personalStatusFilter, personalSearch, () => keys.value], () => { personalPage.value = 1 })
watch(personalListState, state => { if (!state.hasMany) personalSearch.value = ''; if (!state.hasMany || !state.multipleStates || !state.filters.some(filter => filter.value === personalStatusFilter.value)) personalStatusFilter.value = 'all' })
watch(historyListState, state => { if (!state.multipleStates || !state.filters.some(filter => filter.value === statusFilter.value)) statusFilter.value = 'all'; if (!historyKeys.value.length) historyExpanded.value = false })
watch([personalPage, () => filteredPersonalKeys.value.length], () => { personalPage.value = clampCredentialPage(personalPage.value, filteredPersonalKeys.value.length) })
watch([historyPage, () => filteredKeys.value.length], () => { historyPage.value = clampCredentialPage(historyPage.value, filteredKeys.value.length) })
const invalidTenantIds = computed(() => selectedTenantIds.value.filter(id => !options.value.some(option => option.tenant_id === id)))
const configPresetTitle = computed(() => `${clientMeta(configClientType.value).label} · 历史兼容模板`)
const canConnect = computed(() => Boolean(publicUrl.value && connectionKey.value && lifecycleState(connectionKey.value) === 'active'))
const safeConfiguration = computed(() => personalConfig.value && usesSafeCredentialInput(configPreset.value))
const configurationCopyLabel = computed(() => safeConfiguration.value ? '复制安全模板' : connectionKey.value?.secret_available || hasConfigSecret.value ? '复制完整配置（含密钥）' : '复制占位模板（需已有密钥）')
function configuration(secret: string) { return !publicUrl.value ? '' : personalConfig.value ? personalClientConfiguration(configPreset.value, publicUrl.value, secret) : stringifyMCPConfig(publicUrl.value, secret, configSpaces.value, configClientType.value) }
const jsonConfig = computed(() => configuration(configToken.value))
const displayedConfig = computed(() => configuration(secretVisible.value ? configToken.value : TOKEN_PLACEHOLDER))
function openConnect(key: MCPAccessKey) {
  if (lifecycleState(key) !== 'active' || !publicUrl.value || !isUnifiedPersonalKey(key)) return
  clearPlaintextToken(); connectionKey.value = key; personalConfig.value = true; configStage.value = 'clients'; token.value = TOKEN_PLACEHOLDER; configVisible.value = true
}
function openClient(client: MCPClientPreset) {
  if (!canConnect.value) return
  configPreset.value = client; configStage.value = 'config'; secretVisible.value = false; clearProtocolState()
}
function openHistoryConfig(key: MCPAccessKey) { configPreset.value = 'generic'; void showTemplate(key) }
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
function showSecret(data: MCPAccessKey & { token: string; mcp_public_url?: string }) { clearPlaintextToken(); personalConfig.value = isUnifiedPersonalKey(data); configStage.value = personalConfig.value ? 'clients' : 'config'; connectionKey.value = data; token.value = data.token; publicUrl.value = data.mcp_public_url || publicUrl.value; configSpaces.value = spacesForScopes(data.tenant_scopes); configClientType.value = data.client_type; configVisible.value = true }
function openDetails(key: MCPAccessKey) { selectedKey.value = key; detailsVisible.value = true }
async function showTemplate(key: MCPAccessKey) {
  clearPlaintextToken(); personalConfig.value = isUnifiedPersonalKey(key); connectionKey.value = key; token.value = TOKEN_PLACEHOLDER; configSpaces.value = spacesForScopes(key.tenant_scopes); configClientType.value = key.client_type; configStage.value = 'config'; configVisible.value = true
}
async function ensureConnectionSecret(): Promise<string> {
  const key = connectionKey.value
  if (!key || !configVisible.value || !publicUrl.value || lifecycleState(key) !== 'active') throw new Error('凭证或服务地址当前不可用，请刷新后重试')
  if (hasConfigSecret.value) return configToken.value
  if (!key.secret_available) return TOKEN_PLACEHOLDER
  const request = ++connectionRequest
  revealingSecret.value = true
  try {
    const response = await revealMCPAccessKey(key.id)
    if (request !== connectionRequest || !configVisible.value || connectionKey.value?.id !== key.id) throw new Error('凭证选择已改变，请重新操作')
    if (!response.data?.token) throw new Error('未能读取 Secret')
    token.value = response.data.token
    return token.value
  } catch (error) { if (request === connectionRequest && connectionKey.value?.id === key.id) { token.value = TOKEN_PLACEHOLDER; suppliedSecret.value = ''; secretVisible.value = false }; throw error }
  finally { if (request === connectionRequest) revealingSecret.value = false }
}
async function secretAction(action: () => Promise<void>) { const id = connectionKey.value?.id; try { await ensureConnectionSecret(); if (configVisible.value && connectionKey.value?.id === id) await action() } catch (error) { if (configVisible.value && connectionKey.value?.id === id) MessagePlugin.error(error instanceof Error ? error.message : '读取 Secret 失败') } }
async function toggleSecret() { if (secretVisible.value) { secretVisible.value = false; return }; await secretAction(async () => { secretVisible.value = true }) }
async function copySecret() { await secretAction(async () => { if (hasConfigSecret.value) await copySensitive(configToken.value) }) }
async function copyConfiguration() { if (!canConnect.value) return; if (safeConfiguration.value || (!connectionKey.value?.secret_available && !hasConfigSecret.value)) { await copySensitive(displayedConfig.value); return }; await secretAction(async () => { await copySensitive(jsonConfig.value) }) }
function clearProtocolState() { protocolRequest++; testingPersonal.value = false; personalConnection.value = ''; personalProtocolPassed.value = false }
watch([configPreset, suppliedSecret], clearProtocolState)
function clearPlaintextToken() { clearProtocolState(); suppliedSecret.value = ''; connectionRequest++; revealingSecret.value = false; secretVisible.value = false; token.value = ''; configSpaces.value = []; connectionKey.value = null }
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
async function copySensitive(value: string) { const copied = await copyWithToast(value, 'common.copied'); if (copied && hasConfigSecret.value && value.includes(configToken.value)) MessagePlugin.warning('复制内容包含密钥：仅保存到可信的个人配置位置，禁止提交代码仓库') }

watch(configVisible, visible => { if (!visible) clearPlaintextToken() }); onBeforeUnmount(clearPlaintextToken); onMounted(load)
</script>

<style scoped lang="less">
@import "./mcp-access.less";
</style>
