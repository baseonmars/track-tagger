import assert from 'node:assert/strict';
import test from 'node:test';
import { assertReleaseKind, publishRelease } from './publish-logic.mjs';

const assets = ['a.zip', 'b.tar.gz', 'SHA256SUMS'];
const sha = '0123456789abcdef0123456789abcdef01234567';

function harness(overrides = {}) {
  const calls = [];
  let release = null;
  const args = {
    tag: 'v1.0.0',
    targetSha: sha,
    expectedPrerelease: false,
    expectedAssets: assets,
    prepareAssets: async () => calls.push('prepare-assets'),
    getRelease: async () => { calls.push('get-release'); return release; },
    verifyTag: async (_tag, target) => { calls.push('verify-tag'); assert.equal(target, sha); },
    ensureTag: async (_tag, target) => { calls.push('ensure-tag'); assert.equal(target, sha); },
    createDraft: async () => { calls.push('create-draft'); release = { isDraft: true, isPrerelease: false, tagName: 'v1.0.0', databaseId: 1 }; },
    uploadDraft: async () => calls.push('upload-draft'),
    getAssetNames: async () => { calls.push('get-assets'); return assets; },
    publishDraft: async () => calls.push('publish-draft'),
    ...overrides,
  };
  return { args, calls, setRelease: (value) => { release = value; } };
}

test('automated publication validates assets, then creates a tag and draft, uploads, verifies, and publishes', async () => {
  const h = harness();
  await publishRelease(h.args);
  assert.deepEqual(h.calls, [
    'prepare-assets', 'get-release', 'ensure-tag', 'create-draft', 'upload-draft',
    'get-release', 'get-assets', 'publish-draft',
  ]);
});

test('incomplete artifacts fail before tag creation, draft creation, or publication', async () => {
  const h = harness({ prepareAssets: async () => { h.calls.push('prepare-assets'); throw new Error('missing binary'); } });
  await assert.rejects(publishRelease(h.args), /missing binary/);
  assert.deepEqual(h.calls, ['prepare-assets']);
});

test('tag target mismatch stops before release creation', async () => {
  const h = harness({ ensureTag: async () => { h.calls.push('ensure-tag'); throw new Error('tag SHA mismatch'); } });
  await assert.rejects(publishRelease(h.args), /tag SHA mismatch/);
  assert.deepEqual(h.calls, ['prepare-assets', 'get-release', 'ensure-tag']);
});

test('failed draft upload never publishes the release', async () => {
  const h = harness({ uploadDraft: async () => { h.calls.push('upload-draft'); throw new Error('upload failed'); } });
  await assert.rejects(publishRelease(h.args), /upload failed/);
  assert.ok(h.calls.includes('create-draft'));
  assert.ok(!h.calls.includes('publish-draft'));
});

test('published releases are immutable and only skipped when all assets already exist', async () => {
  const h = harness({
    getRelease: async () => { h.calls.push('get-release'); return { isDraft: false, isPrerelease: false, tagName: 'v1.0.0' }; },
  });
  const result = await publishRelease(h.args);
  assert.deepEqual(result, { alreadyPublished: true });
  assert.deepEqual(h.calls, ['prepare-assets', 'get-release', 'verify-tag', 'get-assets']);
});

test('a public release missing assets is rejected without mutation', async () => {
  const h = harness({
    getRelease: async () => { h.calls.push('get-release'); return { isDraft: false, isPrerelease: false, tagName: 'v1.0.0' }; },
    getAssetNames: async () => { h.calls.push('get-assets'); return ['a.zip']; },
  });
  await assert.rejects(publishRelease(h.args), /already public but is missing assets/);
  assert.ok(!h.calls.includes('upload-draft'));
  assert.ok(!h.calls.includes('publish-draft'));
});

test('stable release workflow refuses an existing prerelease draft', async () => {
  assert.throws(() => assertReleaseKind({ isPrerelease: true }, false, 'v1.0.0'), /expected prerelease=false/);
});

test('manual prereleases remain prereleases and stable versions do not', () => {
  assert.doesNotThrow(() => assertReleaseKind({ isPrerelease: true }, true, 'v1.1.0-rc.1'));
  assert.doesNotThrow(() => assertReleaseKind({ isPrerelease: false }, false, 'v1.1.0'));
});
