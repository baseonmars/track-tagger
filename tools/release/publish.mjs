import { execFileSync } from 'node:child_process';
import { createHash, randomUUID } from 'node:crypto';
import { readFile, readdir, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import semver from 'semver';
import { getReleaseByTag } from './github.mjs';
import { publishRelease } from './publish-logic.mjs';

const repositoryDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const distDir = path.join(repositoryDir, 'dist');
const expectedAssets = [
  'track-tagger-linux-amd64.tar.gz',
  'track-tagger-linux-arm64.tar.gz',
  'track-tagger-darwin-amd64.tar.gz',
  'track-tagger-darwin-arm64.tar.gz',
  'track-tagger-windows-amd64.zip',
  'track-tagger-windows-arm64.zip',
  'track-tagger-windows-x64-setup.exe',
  'track-tagger-windows-arm64-setup.exe',
];

function run(command, args, options = {}) {
  return execFileSync(command, args, {
    cwd: repositoryDir,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
    env: process.env,
    ...options,
  }).trim();
}

function releaseInfo(tag) {
  return getReleaseByTag({
    repository: process.env.GITHUB_REPOSITORY,
    tag,
    token: process.env.GITHUB_TOKEN ?? process.env.GH_TOKEN,
    apiUrl: process.env.GITHUB_API_URL ?? 'https://api.github.com',
  });
}

async function createChecksums() {
  const entries = (await readdir(distDir)).filter((name) => expectedAssets.includes(name)).sort();
  const missing = expectedAssets.filter((name) => !entries.includes(name));
  if (missing.length) throw new Error(`Release artifacts are missing: ${missing.join(', ')}`);
  const unexpected = (await readdir(distDir)).filter((name) => !expectedAssets.includes(name));
  if (unexpected.length) throw new Error(`Unexpected files in dist: ${unexpected.join(', ')}`);

  const lines = [];
  for (const name of entries) {
    const bytes = await readFile(path.join(distDir, name));
    lines.push(`${createHash('sha256').update(bytes).digest('hex')}  ${name}`);
  }
  await writeFile(path.join(distDir, 'SHA256SUMS'), `${lines.join('\n')}\n`, 'utf8');
}

async function ensureTag(tag, targetSha, notes) {
  let existing;
  try {
    existing = run('git', ['rev-parse', '--verify', `${tag}^{commit}`]);
  } catch {
    existing = '';
  }
  if (existing) {
    if (existing !== targetSha) throw new Error(`Tag ${tag} points to ${existing}, expected ${targetSha}`);
    return;
  }

  const messagePath = path.join(os.tmpdir(), `track-tagger-${randomUUID()}.txt`);
  const message = `Track Tagger automated release ${tag}\n\n${notes}`;
  await writeFile(messagePath, message, 'utf8');
  try {
    run('git', ['-c', 'user.name=github-actions[bot]', '-c', 'user.email=41898282+github-actions[bot]@users.noreply.github.com', 'tag', '-a', tag, '-F', messagePath, targetSha]);
    run('git', ['push', 'origin', `refs/tags/${tag}`], { encoding: 'utf8' });
  } finally {
    await import('node:fs/promises').then(({ unlink }) => unlink(messagePath).catch(() => {}));
  }
}

function verifyTag(tag, targetSha) {
  const taggedSha = run('git', ['rev-parse', '--verify', `${tag}^{commit}`]);
  if (taggedSha !== targetSha) throw new Error(`Tag ${tag} points to ${taggedSha}, expected ${targetSha}`);
}

async function main() {
  const mode = process.env.RELEASE_MODE;
  const targetSha = process.env.GITHUB_SHA;
  let tag;
  let notes = '';

  if (mode === 'automated') {
    const plan = JSON.parse(await readFile(path.join(repositoryDir, 'release-plan.json'), 'utf8'));
    if (!plan.release) return console.log('No release to publish.');
    if (plan.targetSha !== targetSha) throw new Error(`Plan targets ${plan.targetSha}, not workflow SHA ${targetSha}`);
    tag = plan.tag;
    notes = plan.notes;
    if (tag !== `v${plan.version}` || !/^v\d+\.\d+\.\d+$/.test(tag)) throw new Error(`Invalid planned release tag: ${tag}`);
  } else if (mode === 'manual') {
    tag = process.env.GITHUB_REF_NAME;
    if (!semver.valid(tag)) throw new Error(`Manual release must use a semver v-tag: ${tag}`);
  } else {
    throw new Error(`Unknown RELEASE_MODE: ${mode}`);
  }

  const expected = [...expectedAssets, 'SHA256SUMS'].sort();
  const expectedPrerelease = mode === 'manual' && Boolean(semver.prerelease(tag));
  const result = await publishRelease({
    tag,
    targetSha,
    expectedPrerelease,
    expectedAssets: expected,
    prepareAssets: createChecksums,
    getRelease: releaseInfo,
    verifyTag,
    ensureTag: mode === 'automated' ? (name, sha) => ensureTag(name, sha, notes) : verifyTag,
    createDraft: async (name, sha) => {
      const args = ['release', 'create', name, '--draft', '--verify-tag', '--target', sha, '--title', `Track Tagger ${name}`];
      if (mode === 'automated') {
        const notesPath = path.join(os.tmpdir(), `track-tagger-notes-${randomUUID()}.md`);
        await writeFile(notesPath, notes, 'utf8');
        try {
          run('gh', [...args, '--notes-file', notesPath]);
        } finally {
          await import('node:fs/promises').then(({ unlink }) => unlink(notesPath).catch(() => {}));
        }
      } else {
        if (expectedPrerelease) args.push('--prerelease');
        run('gh', [...args, '--generate-notes']);
      }
    },
    uploadDraft: async (name, names) => {
      run('gh', ['release', 'upload', name, ...names.map((asset) => path.join(distDir, asset)), '--clobber']);
    },
    getAssetNames: async (name) => run('gh', ['release', 'view', name, '--json', 'assets', '--jq', '.assets[].name']).split(/\r?\n/).filter(Boolean),
    publishDraft: async (draft) => {
      const published = JSON.parse(run('gh', ['api', '--method', 'PATCH', `repos/${process.env.GITHUB_REPOSITORY}/releases/${draft.databaseId}`, '--field', 'draft=false']));
      if (published.draft) throw new Error(`GitHub did not publish draft ${tag}.`);
    },
  });
  console.log(result.alreadyPublished ? `Release ${tag} is already published with all expected assets.` : `Published ${tag} after verifying ${expected.length} release assets.`);
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
