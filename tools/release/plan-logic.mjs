import { analyzeCommits as analyze } from '@semantic-release/commit-analyzer';
import { generateNotes } from '@semantic-release/release-notes-generator';
import semver from 'semver';

const quietLogger = { log() {}, success() {}, warn() {}, error() {} };

export const conventionalCommitOptions = { preset: 'conventionalcommits' };

export async function analyzeCommitMessages(messages) {
  return analyze(conventionalCommitOptions, {
    commits: messages.map((message, index) => ({ hash: String(index), message })),
    logger: quietLogger,
  });
}

export async function planFromCommits({ commits, lastRelease, targetSha, repositoryUrl, cwd }) {
  const releaseType = await analyze(conventionalCommitOptions, {
    commits,
    cwd,
    logger: quietLogger,
  });
  if (!releaseType) return null;

  // Start the project at 1.0.0 once it has a release-worthy history.
  const version = lastRelease.gitTag ? semver.inc(lastRelease.version, releaseType) : '1.0.0';
  if (!version) throw new Error(`Cannot increment ${lastRelease.version} with release type ${releaseType}`);
  const nextRelease = { version, gitTag: `v${version}`, gitHead: targetSha };
  const notes = await generateNotes(conventionalCommitOptions, {
    commits,
    lastRelease,
    nextRelease,
    options: { repositoryUrl },
    cwd,
  });
  return { ...nextRelease, notes };
}

export function resolveReleasePlan({ eventName, branch, sha, recovery, nextRelease }) {
  if (eventName !== 'push' || branch !== 'main') {
    return { release: false, version: '', tag: '', targetSha: sha, notes: '' };
  }

  if (recovery?.state === 'published') {
    return { release: false, version: recovery.version, tag: recovery.tag, targetSha: sha, notes: '' };
  }

  if (recovery && ['draft', 'missing'].includes(recovery.state)) {
    return {
      release: true,
      version: recovery.version,
      tag: recovery.tag,
      targetSha: sha,
      notes: recovery.notes,
      recovered: true,
    };
  }

  if (!nextRelease) {
    return { release: false, version: '', tag: '', targetSha: sha, notes: '' };
  }
  if (nextRelease.gitHead !== sha) {
    throw new Error(`semantic-release planned ${nextRelease.gitHead}, expected checked out commit ${sha}`);
  }
  return {
    release: true,
    version: nextRelease.version,
    tag: nextRelease.gitTag,
    targetSha: sha,
    notes: nextRelease.notes,
    recovered: false,
  };
}
