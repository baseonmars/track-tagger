import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { collectCommits, highestReachableStableTag } from './git-history.mjs';
import { planFromCommits } from './plan-logic.mjs';

test('local Git history plans from reachable stable tags without changing refs or pushing', async () => {
  const temp = await mkdtemp(path.join(os.tmpdir(), 'track-tagger-release-'));
  const work = path.join(temp, 'work');
  const bare = path.join(temp, 'origin.git');
  const run = (cwd, args) => execFileSync('git', args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  const runGit = (args) => run(work, args);
  try {
    await import('node:fs/promises').then(({ mkdir }) => mkdir(work));
    run(temp, ['init', '--bare', bare]);
    run(work, ['init', '-b', 'main']);
    runGit(['config', 'user.name', 'Release test']);
    runGit(['config', 'user.email', 'release-test@example.invalid']);
    await writeFile(path.join(work, 'README.md'), 'initial\n');
    runGit(['add', 'README.md']);
    runGit(['commit', '-m', 'docs: add initial readme']);
    await writeFile(path.join(work, 'README.md'), 'initial\nfeature\n');
    runGit(['commit', '-am', 'feat: add the first launcher']);
    const firstSha = runGit(['rev-parse', 'HEAD']);

    const firstHistory = collectCommits({ runGit, baseTag: null, sha: firstSha });
    const first = await planFromCommits({
      commits: firstHistory,
      lastRelease: { gitTag: null, gitHead: null, version: '0.0.0' },
      targetSha: firstSha,
      repositoryUrl: 'https://github.com/baseonmars/track-tagger.git',
      cwd: work,
    });
    assert.equal(first.version, '1.0.0');
    assert.match(first.notes, /github\.com\/baseonmars\/track-tagger\/commit/);
    runGit(['tag', '-a', 'v1.0.0', '-m', 'first stable release', firstSha]);

    runGit(['checkout', '-b', 'abandoned']);
    await writeFile(path.join(work, 'README.md'), 'unreachable\n');
    runGit(['commit', '-am', 'feat: unreachable experiment']);
    runGit(['tag', '-a', 'v9.0.0', '-m', 'unreachable version']);
    runGit(['checkout', 'main']);
    await writeFile(path.join(work, 'README.md'), 'initial\nfeature\nfix\n');
    runGit(['commit', '-am', 'fix: preserve launcher state']);
    const targetSha = runGit(['rev-parse', 'HEAD']);

    const localRefsBefore = runGit(['show-ref', '--heads', '--tags']);
    const historyBefore = runGit(['log', '--format=%H %s']);
    const statusBefore = runGit(['status', '--porcelain']);
    runGit(['remote', 'add', 'origin', bare]);
    runGit(['push', '-u', 'origin', 'main']);
    runGit(['push', 'origin', 'v1.0.0']);
    const remoteRefsBefore = run(temp, ['--git-dir', bare, 'show-ref']);

    const baseline = highestReachableStableTag({ runGit, sha: targetSha });
    assert.equal(baseline.tag, 'v1.0.0');
    const commits = collectCommits({ runGit, baseTag: baseline.tag, sha: targetSha });
    assert.deepEqual(commits.map(({ message }) => message.split('\n')[0]), ['fix: preserve launcher state']);
    const patch = await planFromCommits({
      commits,
      lastRelease: baseline,
      targetSha,
      repositoryUrl: 'https://github.com/baseonmars/track-tagger.git',
      cwd: work,
    });
    assert.equal(patch.version, '1.0.1');
    assert.match(patch.notes, /github\.com\/baseonmars\/track-tagger\/commit/);
    assert.equal(runGit(['show-ref', '--heads', '--tags']), localRefsBefore);
    assert.equal(runGit(['log', '--format=%H %s']), historyBefore);
    assert.equal(runGit(['status', '--porcelain']), statusBefore);
    assert.equal(run(temp, ['--git-dir', bare, 'show-ref']), remoteRefsBefore);
  } finally {
    await rm(temp, { recursive: true, force: true });
  }
});

test('baseline selection ignores only merge-base status 1 and propagates Git failures', () => {
  const runGit = (args) => {
    if (args[0] === 'tag') return 'v2.0.0\nv1.0.0';
    if (args[0] === 'merge-base') {
      const error = new Error('git failed');
      error.status = args[1] === 'v2.0.0' ? 1 : 128;
      throw error;
    }
    throw new Error(`unexpected git command: ${args[0]}`);
  };
  assert.throws(() => highestReachableStableTag({ runGit, sha: 'deadbeef' }), /git failed/);
});
