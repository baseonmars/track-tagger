$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
foreach ($name in @('AZURE_CLIENT_ID', 'AZURE_TENANT_ID', 'AZURE_SUBSCRIPTION_ID', 'ARTIFACT_SIGNING_ENDPOINT', 'ARTIFACT_SIGNING_ACCOUNT', 'ARTIFACT_SIGNING_PROFILE', 'ARTIFACT_SIGNING_PUBLISHER')) {
    if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name))) { throw "Missing production signing configuration: $name" }
}
if ($env:ARTIFACT_SIGNING_ENDPOINT.TrimEnd('/') -ne 'https://eus.codesigning.azure.net') { throw 'Expected the East US signing endpoint' }
if (-not (Test-Path -LiteralPath "$env:WINDIR/System32/vcruntime140.dll")) { throw 'The x64 Visual C++ runtime is required by the signing client' }
$root = Join-Path $env:RUNNER_TEMP 'artifact-signing'
New-Item -ItemType Directory -Force $root | Out-Null
$packages = @(
    @{ Name = 'microsoft.artifactsigning.client'; Version = '1.0.146'; Hash = '7761E5D8F31E54B764DA21D33CD8B0A2940E1B05526C7DFCB5E94B0431426F70' },
    @{ Name = 'microsoft.windows.sdk.buildtools'; Version = '10.0.26100.7627'; Hash = '34480A26E87C9A127B36997E1AB3C7D93812384DCE484877E11816D3A6B1CCCE' }
)
foreach ($package in $packages) {
    $name = $package.Name
    $version = $package.Version
    $archive = Join-Path $root "$name.zip"
    Invoke-WebRequest "https://api.nuget.org/v3-flatcontainer/$name/$version/$name.$version.nupkg" -OutFile $archive
    if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash -ne $package.Hash) { throw "Signing dependency hash mismatch: $name" }
    Expand-Archive -LiteralPath $archive -DestinationPath (Join-Path $root $name)
}
$metadata = Join-Path $root 'metadata.json'
@{
    Endpoint = $env:ARTIFACT_SIGNING_ENDPOINT
    CodeSigningAccountName = $env:ARTIFACT_SIGNING_ACCOUNT
    CertificateProfileName = $env:ARTIFACT_SIGNING_PROFILE
    CorrelationId = "$env:GITHUB_RUN_ID-$env:GITHUB_RUN_ATTEMPT"
    # azure/login obtains an Azure CLI session through GitHub OIDC. No other
    # credential source may silently substitute a different signing identity.
    ExcludeCredentials = @('EnvironmentCredential', 'WorkloadIdentityCredential', 'ManagedIdentityCredential', 'SharedTokenCacheCredential', 'VisualStudioCredential', 'VisualStudioCodeCredential', 'AzurePowerShellCredential', 'AzureDeveloperCliCredential', 'InteractiveBrowserCredential', 'BrokerCredential')
} | ConvertTo-Json | Set-Content -LiteralPath $metadata -Encoding utf8
"SIGNTOOL_PATH=$(Join-Path $root 'microsoft.windows.sdk.buildtools/bin/10.0.26100.0/x64/signtool.exe')" >> $env:GITHUB_ENV
"SIGNING_DLIB_PATH=$(Join-Path $root 'microsoft.artifactsigning.client/bin/x64/Azure.CodeSigning.Dlib.dll')" >> $env:GITHUB_ENV
"SIGNING_METADATA_PATH=$metadata" >> $env:GITHUB_ENV
