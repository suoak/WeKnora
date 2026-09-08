<template>
  <section v-if="config" class="editor">
    <div class="editor-head"><div><h2>{{ config.tenant_name }}</h2><p>Workspace #{{ config.tenant_id }}</p></div><t-tag :theme="statusTheme">{{ statusLabel }}</t-tag></div>
    <t-form label-align="top">
      <div class="form-grid">
        <t-form-item label="Portal display name" required><t-input v-model="form.display_name" :maxlength="128" /></t-form-item>
        <t-form-item label="Category（知识领域 / 组织分类）"><t-input v-model="form.category" placeholder="例如 ntos、public_rd" :maxlength="64" /></t-form-item>
      </div>
      <t-form-item label="Description"><t-textarea v-model="form.description" :maxlength="4000" :autosize="{minRows:3,maxRows:7}" /></t-form-item>
      <div class="form-grid">
        <t-form-item label="Responsible team"><t-input v-model="form.responsible_team" :maxlength="128" /></t-form-item>
        <t-form-item label="Contact"><t-input v-model="form.contact" :maxlength="256" /></t-form-item>
      </div>
      <t-form-item label="IPD stages"><t-checkbox-group v-model="form.stages" :options="stageOptions" /></t-form-item>
      <div class="form-grid">
        <t-form-item label="Display order"><t-input-number v-model="form.display_order" /></t-form-item>
        <t-form-item label="Interaction organization"><t-select v-model="form.interaction_organization_id" clearable placeholder="不关联交互空间"><t-option v-for="org in organizations" :key="org.id" :value="org.id" :label="org.name" /></t-select></t-form-item>
      </div>
      <div class="switches"><t-checkbox v-model="form.featured">Featured</t-checkbox><t-checkbox v-model="form.allow_access_request">允许提交 Viewer 访问申请</t-checkbox></div>
    </t-form>
    <div class="save-row"><t-button :loading="saving" @click="save">保存配置</t-button><span>保存配置不会修改发布状态。</span></div>
    <div class="status-actions"><strong>发布状态</strong><div><t-button v-if="config.status!=='published'" theme="success" variant="outline" :loading="transitioning" @click="$emit('status','publish')">发布</t-button><t-button v-if="config.status==='published'" variant="outline" :loading="transitioning" @click="$emit('status','unpublish')">取消发布</t-button><t-button v-if="config.status!=='archived'" theme="danger" variant="outline" :loading="transitioning" @click="$emit('status','archive')">归档</t-button></div></div>
  </section>
</template>
<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import type { PortalAdminSpace, PortalConfigPayload, PortalOrganizationOption, PortalStage } from '@/api/portal'
const props=defineProps<{config:PortalAdminSpace|null;stages:PortalStage[];organizations:PortalOrganizationOption[];saving:boolean;transitioning:boolean}>()
const emit=defineEmits<{save:[payload:PortalConfigPayload];status:[action:'publish'|'unpublish'|'archive']}>()
const form=reactive<PortalConfigPayload>({display_name:'',description:'',category:'',responsible_team:'',contact:'',stages:[],featured:false,display_order:0,allow_access_request:false,interaction_organization_id:null})
watch(()=>props.config,(config)=>{if(!config)return;Object.assign(form,{display_name:config.display_name||config.tenant_name,description:config.description||'',category:config.category||'',responsible_team:config.responsible_team||'',contact:config.contact||'',stages:[...(config.stages||[])],featured:Boolean(config.featured),display_order:Number(config.display_order||0),allow_access_request:Boolean(config.allow_access_request),interaction_organization_id:config.interaction_organization_id||null})},{immediate:true})
const stageOptions=computed(()=>props.stages.map(stage=>({label:stage.name||stage.key,value:stage.key})))
const statusLabel=computed(()=>({draft:'草稿',published:'已发布',archived:'已归档'}[props.config?.status||'draft']))
const statusTheme=computed(()=>props.config?.status==='published'?'success':props.config?.status==='archived'?'danger':'default')
function save(){if(!form.display_name.trim())return;emit('save',{...form,display_name:form.display_name.trim(),description:form.description.trim(),category:form.category.trim().toLowerCase(),responsible_team:form.responsible_team.trim(),contact:form.contact.trim(),stages:[...form.stages],interaction_organization_id:form.interaction_organization_id||null})}
</script>
<style scoped lang="less">
.editor{padding:24px;border:1px solid var(--td-component-border);border-radius:16px;background:var(--td-bg-color-container)}.editor-head{display:flex;justify-content:space-between;align-items:start;margin-bottom:22px}.editor-head h2{margin:0}.editor-head p{margin:5px 0 0;color:var(--td-text-color-secondary)}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:16px}.switches{display:flex;gap:24px;margin:5px 0 22px}.save-row,.status-actions{display:flex;align-items:center;gap:12px;border-top:1px solid var(--td-component-stroke);padding-top:18px}.save-row span{font-size:12px;color:var(--td-text-color-placeholder)}.status-actions{justify-content:space-between;margin-top:18px}.status-actions>div{display:flex;gap:8px}@media(max-width:720px){.form-grid{grid-template-columns:1fr}.status-actions{align-items:start;flex-direction:column}}
</style>
