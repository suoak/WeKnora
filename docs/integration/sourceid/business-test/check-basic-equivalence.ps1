# Read-only, offline check. Never invokes SID, SSH, a browser, or a deployment.
# Only the child process accesses the protected value; stdout is a boolean.
param(
    [string]$ProtectedPath,
    [switch]$SyntheticOnly
)
$ErrorActionPreference = 'Stop'
$VerbosePreference = 'SilentlyContinue'
$DebugPreference = 'SilentlyContinue'
Set-PSDebug -Off
try {
    Add-Type -ReferencedAssemblies 'System.Security.dll' -TypeDefinition @'
using System;
using System.IO;
using System.Security.Cryptography;
using System.Text;
public static class SourceIDBasicEquivalence {
    private static string Form(string value) {
        byte[] bytes = Encoding.UTF8.GetBytes(value);
        try {
            var result = new StringBuilder();
            foreach (byte b in bytes) {
                if ((b >= 65 && b <= 90) || (b >= 97 && b <= 122) ||
                    (b >= 48 && b <= 57) || b == 45 || b == 46 || b == 95 || b == 126)
                    result.Append((char)b);
                else if (b == 32) result.Append('+');
                else result.Append('%').Append(b.ToString("X2"));
            }
            return result.ToString();
        } finally { Array.Clear(bytes, 0, bytes.Length); }
    }
    private static bool Compare(string id, string secret) {
        byte[] raw = null, form = null;
        string rawHeader = null, formHeader = null;
        try {
            raw = Encoding.UTF8.GetBytes(id + ":" + secret);
            form = Encoding.UTF8.GetBytes(Form(id) + ":" + Form(secret));
            rawHeader = "Basic " + Convert.ToBase64String(raw);
            formHeader = "Basic " + Convert.ToBase64String(form);
            return String.Equals(rawHeader, formHeader, StringComparison.Ordinal);
        } finally {
            if (raw != null) Array.Clear(raw, 0, raw.Length);
            if (form != null) Array.Clear(form, 0, form.Length);
            rawHeader = null; formHeader = null;
        }
    }
    public static bool CheckProtectedFile(string path) {
        byte[] encrypted = null, decrypted = null;
        string plaintext = null;
        try {
            encrypted = File.ReadAllBytes(path);
            decrypted = ProtectedData.Unprotect(encrypted, null, DataProtectionScope.CurrentUser);
            plaintext = new UTF8Encoding(false, true).GetString(decrypted);
            if (String.IsNullOrEmpty(plaintext)) throw new InvalidOperationException();
            return Compare("knowhub", plaintext);
        } finally {
            plaintext = null;
            if (encrypted != null) Array.Clear(encrypted, 0, encrypted.Length);
            if (decrypted != null) Array.Clear(decrypted, 0, decrypted.Length);
        }
    }
    public static bool SelfTest() {
        string[] values = { "Synthetic-AZ_09.~", "Synthetic+Only", "Synthetic/Only",
            "Synthetic=Only", "Synthetic%Only", "Synthetic:Only", "Synthetic Only",
            "Synthetic-\u6d4b\u8bd5", "Synthetic%2BOnly" };
        string[] escaped = { "Synthetic-AZ_09.~", "Synthetic%2BOnly", "Synthetic%2FOnly",
            "Synthetic%3DOnly", "Synthetic%25Only", "Synthetic%3AOnly", "Synthetic+Only",
            "Synthetic-%E6%B5%8B%E8%AF%95", "Synthetic%252BOnly" };
        for (int i = 0; i < values.Length; i++) {
            if (Form(values[i]) != escaped[i]) return false;
            if (Compare("knowhub", values[i]) != (i == 0)) return false;
        }
        return !Compare("synthetic+client", "SyntheticOnly");
    }
}
'@ | Out-Null
    if (-not [SourceIDBasicEquivalence]::SelfTest()) { throw 'checker self-test failed' }
    if ($SyntheticOnly) {
        if ($ProtectedPath) { throw 'ambiguous invocation' }
        Write-Output 'SELF_TEST=PASS'
    } else {
        if (-not $ProtectedPath) { throw 'protected path required' }
        # Matches the existing storage writer: binary DPAPI, CurrentUser,
        # null entropy, UTF-8. Plaintext exists only inside the C# runtime.
        $equivalent = [SourceIDBasicEquivalence]::CheckProtectedFile($ProtectedPath)
        if ($equivalent) { Write-Output 'EQUIVALENT=true' }
        else { Write-Output 'EQUIVALENT=false' }
    }
} catch {
    Write-Output 'EQUIVALENT=UNKNOWN'
    exit 2
}
# Managed strings cannot be reliably erased by .NET. Run in a short-lived
# process under the vault owner; do not enable transcripts, dumps or tracing.
