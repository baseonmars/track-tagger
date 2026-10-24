import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const workflow = readFileSync(new URL('../../.github/workflows/ci.yml', import.meta.url), 'utf8').replaceAll('\r', '');
const jobs = Object.fromEntries([...workflow.matchAll(/^  ([a-z][a-z-]+):\n([\s\S]*?)(?=^  [a-z][a-z-]+:|$(?![\s\S]))/gm)].map((match) => [match[1], match[2]]));
function needs(name) {
  return jobs[name].match(/^    needs: \[([^\]]+)\]/m)[1].split(',').map((s) => s.trim());
}

test('production signing is downstream of the protected approval and unsigned checks', () => {
  assert.deepEqual(needs('sign-windows'), ['authorize-release']);
  assert.match(jobs['authorize-release'], /environment: github-release/);
  assert.match(jobs['authorize-release'], /github.event_name == 'push'/);
  assert.match(jobs['authorize-release'], /needs\.plan-release\.outputs\.release == 'true'/);
  assert.match(jobs['authorize-release'], /startsWith\(github.ref, 'refs\/tags\/v'\)/);
  assert.deepEqual(needs('authorize-release'), ['test', 'plan-release', 'binaries', 'windows-installers']);
  assert.match(jobs['sign-windows'], /always\(\).*needs\.authorize-release\.result == 'success'/);
  assert.match(jobs['sign-windows'], /environment: github-release/);
  assert.match(jobs['sign-windows'], /id-token: write/);
  assert.match(jobs['sign-windows'], /uses: azure\/login@/);
  assert.doesNotMatch(jobs['sign-windows'], /secrets\./);
});

test('every publisher waits for signing and downloads only release assets', () => {
  for (const name of ['publish-main-container', 'publish-manual-container', 'publish-main-release', 'release']) {
    assert.ok(needs(name).includes('sign-windows'), name);
    if (jobs[name].includes('always()')) assert.match(jobs[name], /needs\.sign-windows\.result == 'success'/);
  }
  for (const name of ['publish-main-release', 'release']) {
    const pattern = jobs[name].match(/pattern: (.+)/)[1].trim();
    const regex = new RegExp(`^${pattern.replaceAll('*', '.*')}$`);
    for (const artifact of ['release-linux-amd64', 'release-darwin-arm64', 'release-windows-amd64']) assert.ok(regex.test(artifact));
    for (const artifact of ['release-plan', 'unsigned-windows-amd64', 'unsigned-windows-arm64-installer']) assert.ok(!regex.test(artifact));
  }
});

test('Windows ZIP uses signed payload and Inno signs its generated uninstaller', () => {
  const job = jobs['sign-windows'];
  assert.ok(job.indexOf('sign-windows.ps1 -File $file') < job.indexOf('Compress-Archive'));
  assert.match(job, /\/DProductionSigning/);
  assert.match(job, /\/Sazure=\$signCommand/);
  assert.match(job, /-VerifyOnly -File .*setup.exe/);
  assert.match(job, /-VerifyOnly -File \$env:SIGNING_UNINSTALLER_EVIDENCE/);
  const installer = readFileSync(new URL('../../packaging/windows/track-tagger.iss', import.meta.url), 'utf8').replaceAll('\r', '');
  assert.match(installer, /#ifdef ProductionSigning\nSignTool=azure\nSignedUninstaller=yes/);
});

test('production configuration is mandatory and Azure CLI is the sole credential source', () => {
  const script = readFileSync(new URL('./prepare-signing.ps1', import.meta.url), 'utf8').replaceAll('\r', '');
  for (const name of ['AZURE_CLIENT_ID', 'AZURE_TENANT_ID', 'AZURE_SUBSCRIPTION_ID', 'ARTIFACT_SIGNING_ENDPOINT', 'ARTIFACT_SIGNING_ACCOUNT', 'ARTIFACT_SIGNING_PROFILE', 'ARTIFACT_SIGNING_PUBLISHER']) {
    assert.match(script, new RegExp(`'${name}'`));
    assert.match(jobs['sign-windows'], new RegExp(`${name}: \\$\\{\\{ vars\\.${name} \\}\\}`));
  }
  assert.match(script, /throw "Missing production signing configuration/);
  const excluded = script.match(/ExcludeCredentials = @\(([^\n]+)\)/)[1];
  assert.doesNotMatch(excluded, /AzureCliCredential/);
  for (const credential of ['EnvironmentCredential', 'WorkloadIdentityCredential', 'ManagedIdentityCredential', 'InteractiveBrowserCredential']) assert.ok(excluded.includes(credential));
});
