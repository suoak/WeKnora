<template>
  <section v-if="config" class="editor">
    <div class="editor-head"><div><h2>{{ config.tenant_name }}</h2><p>{{ t('portal.admin.workspaceNumber', { id: config.tenant_id }) }}</p></div><t-tag :theme="statusTheme">{{ statusLabel }}</t-tag></div>
    <t-form label-align="top">
      <section class="config-group">
        <div class="group-heading"><h3>{{ t('portal.admin.groups.basicTitle') }}</h3><p>{{ t('portal.admin.groups.basicDescription') }}</p></div>
        <t-form-item :label="t('portal.admin.displayName')" required><t-input v-model="form.display_name" :maxlength="128" /></t-form-item>
        <t-form-item :label="t('portal.admin.descriptionLabel')"><t-textarea v-model="form.description" :maxlength="4000" :autosize="{minRows:3,maxRows:7}" /></t-form-item>
        <div class="form-grid">
          <t-form-item :label="t('portal.admin.responsibleTeam')"><t-input v-model="form.responsible_team" :maxlength="128" /></t-form-item>
          <t-form-item :label="t('portal.admin.contact')"><t-input v-model="form.contact" :maxlength="256" /></t-form-item>
        </div>
      </section>
      <section class="config-group">
        <div class="group-heading"><h3>{{ t('portal.admin.groups.positioningTitle') }}</h3><p>{{ t('portal.admin.groups.positioningDescription') }}</p></div>
        <t-form-item :label="t('portal.admin.category')">
          <t-select v-model="form.category" clearable filterable creatable :placeholder="t('portal.admin.categoryPlaceholder')">
            <t-option v-for="item in categoryOptions" :key="item.value" :value="item.value" :label="item.label" />
          </t-select>
          <small class="field-help">{{ t('portal.admin.categoryHelp') }}</small>
        </t-form-item>
        <t-form-item :label="t('portal.admin.ipdStages')"><t-checkbox-group v-model="form.stages" :options="stageOptions" /></t-form-item>
      </section>
      <section class="config-group">
        <div class="group-heading"><h3>{{ t('portal.admin.groups.displayTitle') }}</h3><p>{{ t('portal.admin.groups.displayDescription') }}</p></div>
        <div class="form-grid">
          <t-form-item :label="t('portal.admin.displayOrder')"><t-input-number v-model="form.display_order" /></t-form-item>
          <div class="switches"><t-checkbox v-model="form.featured">{{ t('portal.admin.featured') }}</t-checkbox></div>
        </div>
      </section>
      <section class="config-group">
        <div class="group-heading"><h3>{{ t('portal.admin.groups.accessTitle') }}</h3><p>{{ t('portal.admin.groups.accessDescription') }}</p></div>
        <t-form-item :label="t('portal.admin.interactionOrganization')"><t-select v-model="form.interaction_organization_id" clearable :placeholder="t('portal.admin.noInteractionOrganization')"><t-option v-for="org in organizations" :key="org.id" :value="org.id" :label="org.name" /></t-select></t-form-item>
        <div class="switches"><t-checkbox v-model="form.allow_access_request">{{ t('portal.admin.allowAccessRequest') }}</t-checkbox></div>
      </section>
    </t-form>
    <div class="save-row"><t-button :loading="saving" @click="save">{{ t('portal.admin.save') }}</t-button><span>{{ t('portal.admin.saveHint') }}</span></div>
    <div class="status-actions"><strong>{{ t('portal.admin.publishStatus') }}</strong><div><t-button v-if="config.status!=='published'" theme="success" variant="outline" :loading="transitioning" @click="$emit('status','publish')">{{ t('portal.admin.publish') }}</t-button><t-button v-if="config.status==='published'" variant="outline" :loading="transitioning" @click="$emit('status','unpublish')">{{ t('portal.admin.unpublish') }}</t-button><t-button v-if="config.status!=='archived'" theme="danger" variant="outline" :loading="transitioning" @click="$emit('status','archive')">{{ t('portal.admin.archive') }}</t-button></div></div>
  </section>
</template>
<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PortalAdminSpace, PortalConfigPayload, PortalOrganizationOption, PortalStage } from '@/api/portal'
const props=defineProps<{config:PortalAdminSpace|null;stages:PortalStage[];organizations:PortalOrganizationOption[];saving:boolean;transitioning:boolean}>()
const emit=defineEmits<{save:[payload:PortalConfigPayload];status:[action:'publish'|'unpublish'|'archive']}>()
const { t }=useI18n()
const form=reactive<PortalConfigPayload>({display_name:'',description:'',category:'',responsible_team:'',contact:'',stages:[],featured:false,display_order:0,allow_access_request:false,interaction_organization_id:null})
watch(()=>props.config,(config)=>{if(!config)return;Object.assign(form,{display_name:config.display_name||config.tenant_name,description:config.description||'',category:config.category||'',responsible_team:config.responsible_team||'',contact:config.contact||'',stages:[...(config.stages||[])],featured:Boolean(config.featured),display_order:Number(config.display_order||0),allow_access_request:Boolean(config.allow_access_request),interaction_organization_id:config.interaction_organization_id||null})},{immediate:true})
const stageOptions=computed(()=>props.stages.map(stage=>({label:t(`portal.stages.${stage.key}.name`),value:stage.key})))
const categoryOptions=computed(()=>['market_customer','requirements_management','architecture_design','development_assets','test_quality','operations_lifecycle','process_governance','public_knowledge'].map(value=>({value,label:t(`portal.categories.${value}`)})))
const statusLabel=computed(()=>t(`portal.admin.statuses.${props.config?.status||'draft'}`))
const statusTheme=computed(()=>props.config?.status==='published'?'success':props.config?.status==='archived'?'danger':'default')
function save(){if(!form.display_name.trim())return;emit('save',{...form,display_name:form.display_name.trim(),description:form.description.trim(),category:form.category.trim().toLowerCase(),responsible_team:form.responsible_team.trim(),contact:form.contact.trim(),stages:[...form.stages],interaction_organization_id:form.interaction_organization_id||null})}
</script>
<style scoped lang="less">
.editor{padding:24px;border:1px solid var(--td-component-border);border-radius:16px;background:var(--td-bg-color-container)}.editor-head{display:flex;justify-content:space-between;align-items:start;margin-bottom:22px}.editor-head h2{margin:0}.editor-head p{margin:5px 0 0;color:var(--td-text-color-secondary)}.config-group{margin-bottom:18px;padding:18px;border:1px solid var(--td-component-stroke);border-radius:12px;background:var(--td-bg-color-secondarycontainer)}.group-heading{margin-bottom:16px}.group-heading h3{margin:0;font-size:16px}.group-heading p{margin:5px 0 0;color:var(--td-text-color-secondary);font-size:13px}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:16px}.field-help{display:block;margin-top:6px;color:var(--td-text-color-placeholder);line-height:1.5}.switches{display:flex;align-items:center;gap:24px;min-height:32px;margin:5px 0 10px}.save-row,.status-actions{display:flex;align-items:center;gap:12px;border-top:1px solid var(--td-component-stroke);padding-top:18px}.save-row span{font-size:12px;color:var(--td-text-color-placeholder)}.status-actions{justify-content:space-between;margin-top:18px}.status-actions>div{display:flex;gap:8px}@media(max-width:720px){.form-grid{grid-template-columns:1fr}.status-actions{align-items:start;flex-direction:column}}
</style>
