import assert from 'node:assert/strict';
import test from 'node:test';
import semver from 'semver';
import { analyzeCommitMessages, planFromCommits, resolveReleasePlan } from './plan-logic.mjs';
import { getReleaseByTag } from './github.mjs';

const sha = '0123456789abcdef0123456789abcdef01234567';

test('docs-only commits do not release', async () => {
  assert.equal(await analyzeCommitMessages(['docs: clarify Windows install steps']), null);
  assert.deepEqual(resolveReleasePlan({ eventName: 'push', branch: 'main', sha, nextRelease: null }), {
    release: false, version: '', tag: '', targetSha: sha, notes: '',
  });
});

test('feat, fix, and perf commits map to minor or patch versions', async (t) => {
  for (const [message, expectedType, lastVersion, expectedVersion] of [
    ['feat: add album filters', 'minor', '1.0.0', '1.1.0'],
    ['fix: retain saved review decisions', 'patch', '1.0.0', '1.0.1'],
    ['perf(catalog): reduce repeated hashing', 'patch', '1.0.0', '1.0.1'],
  ]) {
    await t.test(message, async () => {
      const type = await analyzeCommitMessages([message]);
      assert.equal(type, expectedType);
      assert.equal(semver.inc(lastVersion, type), expectedVersion);
    });
  }
});

test('breaking syntax in a header or footer maps to a major version', async (t) => {
  for (const message of [
    'feat!: replace the settings format',
    'feat: replace the settings format\n\nBREAKING CHANGE: old launcher settings require migration',
  ]) {
    await t.test(message.split('\n')[0], async () => {
      const type = await analyzeCommitMessages([message]);
      assert.equal(type, 'major');
      assert.equal(semver.inc('1.2.3', type), '2.0.0');
    });
  }
});

test('release planning uses analyzer, semver, and notes generator APIs without a Git remote', async (t) => {
  const repositoryUrl = 'https://github.com/baseonmars/track-tagger.git';
  const commits = [{ hash: sha, message: 'feat: add launcher\n\nAdd the native Windows launcher.' }];
  const initial = await planFromCommits({
    commits,
    lastRelease: { gitTag: null, gitHead: null, version: '0.0.0' },
    targetSha: sha,
    repositoryUrl,
    cwd: process.cwd(),
  });
  assert.equal(initial.version, '1.0.0');
  assert.equal(initial.gitTag, 'v1.0.0');
  assert.equal(initial.gitHead, sha);
  assert.match(initial.notes, /add launcher/);

  const patch = await planFromCommits({
    commits: [{ hash: sha, message: 'fix: preserve saved settings' }],
    lastRelease: { gitTag: 'v1.0.0', gitHead: 'previous', version: '1.0.0' },
    targetSha: sha,
    repositoryUrl,
    cwd: process.cwd(),
  });
  assert.equal(patch.version, '1.0.1');
  assert.match(patch.notes, /github\.com\/baseonmars\/track-tagger/);

  await t.test('docs-only range has no release', async () => {
    assert.equal(await planFromCommits({
      commits: [{ hash: sha, message: 'docs: clarify offline behavior' }],
      lastRelease: { gitTag: 'v1.0.0', gitHead: 'previous', version: '1.0.0' },
      targetSha: sha,
      repositoryUrl,
      cwd: process.cwd(),
    }), null);
  });
});

test('only a push to main can publish', () => {
  for (const [eventName, branch] of [['pull_request', 'main'], ['push', 'feature/windows']]) {
    assert.equal(resolveReleasePlan({ eventName, branch, sha, nextRelease: { version: '1.0.0', gitTag: 'v1.0.0', gitHead: sha, notes: '' } }).release, false);
  }
});

test('a draft or missing release recovers the annotated version and notes', () => {
  for (const state of ['draft', 'missing']) {
    assert.deepEqual(resolveReleasePlan({ eventName: 'push', branch: 'main', sha, recovery: { state, version: '1.4.0', tag: 'v1.4.0', notes: 'release notes' } }), {
      release: true, version: '1.4.0', tag: 'v1.4.0', targetSha: sha, notes: 'release notes', recovered: true,
    });
  }
});

test('an already published tag at HEAD is not published twice', () => {
  assert.equal(resolveReleasePlan({ eventName: 'push', branch: 'main', sha, recovery: { state: 'published', version: '1.4.0', tag: 'v1.4.0' } }).release, false);
});

test('planned semantic-release commit must equal the checked out SHA', () => {
  assert.throws(() => resolveReleasePlan({ eventName: 'push', branch: 'main', sha, nextRelease: { version: '1.0.0', gitTag: 'v1.0.0', gitHead: 'different', notes: '' } }), /expected checked out commit/);
});

test('release lookup treats only HTTP 404 as missing and fails closed otherwise', async (t) => {
  const base = { repository: 'example/track-tagger', tag: 'v1.0.0', token: 'test-token' };
  await t.test('404 means missing release', async () => {
    const result = await getReleaseByTag({ ...base, fetcher: async () => new Response('', { status: 404 }) });
    assert.equal(result, null);
  });
  for (const status of [401, 403, 429, 500]) {
    await t.test(`HTTP ${status} is an error`, async () => {
      await assert.rejects(
        getReleaseByTag({ ...base, fetcher: async () => new Response('', { status }) }),
        new RegExp(`HTTP ${status}`),
      );
    });
  }
  await t.test('network errors propagate', async () => {
    await assert.rejects(getReleaseByTag({ ...base, fetcher: async () => { throw new Error('network unavailable'); } }), /network unavailable/);
  });
});
