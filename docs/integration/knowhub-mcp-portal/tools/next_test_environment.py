"""Public-name fixtures for mocked transport/SSRF unit tests, NOT live DNS.

Containers retain --network none. No cloud API is contacted and no SSRF
assertion is disabled. These are the public endpoints named in failed tests.
"""
PUBLIC_TEST_HOSTS = [
 'example.com','api.e2b.app','oss-cn-hangzhou.aliyuncs.com','open.feishu.cn',
 'api-inference.modelscope.cn','api.anthropic.com','api.deepseek.com',
 'api.hunyuan.cloud.tencent.com','api.lkeap.cloud.tencent.com',
 'api.longcat.chat','api.minimaxi.com','api.moonshot.ai','api.notion.com',
 'api.novita.ai','api.openai.com','api.qnaigc.com','api.siliconflow.cn',
 'api.xiaomimimo.com','ark.cn-beijing.volces.com','company.yuque.com',
 'dashscope.aliyuncs.com','generativelanguage.googleapis.com',
 'integrate.api.nvidia.com','open.bigmodel.cn','openrouter.ai',
 'qianfan.baidubce.com','router.requesty.ai','weknora.weixin.qq.com',
]

def docker_hosts():
 return [part for host in PUBLIC_TEST_HOSTS for part in ('--add-host',host+':8.8.8.8')]
