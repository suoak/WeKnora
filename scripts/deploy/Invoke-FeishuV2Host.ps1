param(
    [Parameter(Mandatory=$true)][string]$CredentialPath,
    [Parameter(Mandatory=$true)][ValidateSet('feishu_v2_inventory.py','feishu_v2_prepare_build.py',
        'feishu_v2_build_candidate.py','feishu_v2_runtime_image.py','feishu_v2_remote_gateway_probe.py',
        'feishu_v2_prepare_test_site.py','feishu_v2_test_site_preflight.py','feishu_v2_network_plan.py',
        'feishu_v2_configure_gateway.py','feishu_v2_bootstrap_site.py','feishu_v2_entry.py',
        'feishu_v2_site_smoke.py','feishu_v2_private_http_probe.py','feishu_v2_upgrade_site.py')][string]$Script,
    [string[]]$ScriptArguments=@(),
    [string]$GatewayCredentialPath,
    [string]$AppIdPath,
    [string]$AppSecretPath
)

# DPAPI belongs to the current Windows user. Plaintext is streamed only to the
# selected helper's stdin and never passed as a process argument or environment.
Add-Type -AssemblyName System.Security
$taskExit=1
try {
    try {
        $taskCipher=[IO.File]::ReadAllBytes((Resolve-Path -LiteralPath $CredentialPath))
        $taskPlain=[Security.Cryptography.ProtectedData]::Unprotect(
            $taskCipher,$null,[Security.Cryptography.DataProtectionScope]::CurrentUser)
        $taskPassword=[Text.Encoding]::UTF8.GetString($taskPlain)
    } catch {
        $taskSecure=(Get-Content -Raw -LiteralPath $CredentialPath).Trim() | ConvertTo-SecureString
        $taskPointer=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($taskSecure)
        try { $taskPassword=[Runtime.InteropServices.Marshal]::PtrToStringBSTR($taskPointer) }
        finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($taskPointer) }
    }
    $taskProgram=Join-Path $PSScriptRoot $Script
    $taskPayload=@{host='172.30.232.42';user='aqyfzx';password=$taskPassword}
    if($GatewayCredentialPath){
        if($Script -ne 'feishu_v2_configure_gateway.py'){throw 'Gateway credential only allowed for gateway configuration.'}
        $taskGatewaySecure=(Get-Content -Raw -LiteralPath $GatewayCredentialPath).Trim() | ConvertTo-SecureString
        $taskGatewayPointer=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($taskGatewaySecure)
        try {$taskGatewayKey=[Runtime.InteropServices.Marshal]::PtrToStringBSTR($taskGatewayPointer)}
        finally {[Runtime.InteropServices.Marshal]::ZeroFreeBSTR($taskGatewayPointer)}
        $taskPayload.gateway=@{base_url='https://uniapi.ruijie.com.cn/v1';api_key=$taskGatewayKey}
    }
    if($AppIdPath -or $AppSecretPath){
        if($Script -ne 'feishu_v2_bootstrap_site.py' -or !$AppIdPath -or !$AppSecretPath){throw 'Test app paths required only for site bootstrap.'}
        $taskAppSecure=(Get-Content -Raw -LiteralPath $AppSecretPath).Trim() | ConvertTo-SecureString
        $taskAppPointer=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($taskAppSecure)
        try {$taskAppSecret=[Runtime.InteropServices.Marshal]::PtrToStringBSTR($taskAppPointer)}
        finally {[Runtime.InteropServices.Marshal]::ZeroFreeBSTR($taskAppPointer)}
        $taskPayload.app=@{app_id=(Get-Content -Raw -LiteralPath $AppIdPath).Trim();client_secret=$taskAppSecret}
    }
    $taskPayload | ConvertTo-Json -Compress -Depth 4 |
        & python $taskProgram @ScriptArguments
    $taskExit=$LASTEXITCODE
} finally {
    if($taskPlain){[Array]::Clear($taskPlain,0,$taskPlain.Length)}
    if($taskPayload){$taskPayload.Clear()}
    Remove-Variable taskPassword,taskSecure,taskGatewayKey,taskGatewaySecure,taskAppSecret,taskAppSecure -ErrorAction SilentlyContinue
}
exit $taskExit
