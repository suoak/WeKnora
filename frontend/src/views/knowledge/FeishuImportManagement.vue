<template><main class="management"><h2>飞书导入历史与设置</h2><p><router-link to="/platform/knowledge-bases">返回知识库</router-link></p><p>任务在后台持续执行。需要处理的文档可在这里查看详情；不会自动扩大同步范围。</p><FeishuAccessManagement v-if="status && kb" :kb-id="kb" :status="status" @connect="connect" /><p v-else>请从知识库的飞书导入入口打开对应历史。</p></main></template>
<script setup lang="ts">
import {ref,onMounted} from 'vue'
import {useRoute} from 'vue-router'
import {feishuStatus,authorizeFeishu,type FeishuStatus} from '@/api/feishuPersonal'
import {FEISHU_PENDING} from '@/i18n/feishuPersonal'
import FeishuAccessManagement from './components/FeishuAccessManagement.vue'
const route=useRoute(), kb=typeof route.query.kb === 'string' ? route.query.kb : '', status=ref<FeishuStatus>()
onMounted(async()=>{status.value=await feishuStatus()})
async function connect(links:string) { sessionStorage.setItem(FEISHU_PENDING,JSON.stringify({kbId:kb,url:links,path:location.pathname+location.search})); const result=await authorizeFeishu(), target=new URL(result.authorization_url); if(target.origin==='https://accounts.feishu.cn') location.assign(target.href) }
</script>
<style scoped>.management {max-width:960px;margin:24px auto;padding:0 20px;overflow-wrap:anywhere;}</style>
