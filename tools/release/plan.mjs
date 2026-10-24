import { execFileSync } from 'node:child_process';
import { writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { getReleaseByTag } from './github.mjs';
import { planFromCommits, resolveReleasePlan } from './plan-logic.mjs';
import { collectCommits, highestReachableStableTag } from './git-history.mjs';

const toolDir = path.dirname(fileURLToPath(import.meta.url));
const repositoryDir = path.resolve(toolDir, '../..');
const tagPattern = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const markerFor = (tag) => `Track Tagger automated release ${tag}`;
const githubRepositoryUrl = `${process.env.GITHUB_SERVER_URL ?? 'https://github.com'}/${process.env.GITHUB_REPOSITORY}.git`;

function git(args) {
  return execFileSync('git', args, { cwd: repositoryDir, encoding: 'utf8' }).trim();
}

function annotatedNotes(tag) {
  if (git(['cat-file', '-t', tag]) !== 'tag') return null;
  const object = git(['cat-file', 'tag', tag]);
  const separator = object.indexOf('\n\n');
  if (separator < 0) return null;
  const body = object.slice(separator + 2);
  const marker = markerFor(tag);
  if (!body.startsWith(marker)) return null;
  return body.slice(marker.length).replace(/^\r?\n/, '');
}

async function findRecovery(sha) {
  const tags = git(['tag', '--list', 'v[0-9]*']).split(/\r?\n/).filter((tag) => tagPattern.test(tag));
  let currentTag;

  for (const tag of tags) {
    const commit = git(['rev-parse', `${tag}^{commit}`]);
    const notes = annotatedNotes(tag);
    const isAutomationTag = notes !== null;
    if (commit !== sha && !isAutomationTag) continue;

    const release = await getReleaseByTag({
      repository: process.env.GITHUB_REPOSITORY,
      tag,
      token: process.env.GITHUB_TOKEN,
      apiUrl: process.env.GITHUB_API_URL ?? 'https://api.github.com',
    });
    if (commit === sha) {
      if (release && !release.isDraft && !release.isPrerelease) {
        return { state: 'published', tag, version: tag.slice(1), notes: '' };
      }
      if (isAutomationTag) {
        currentTag = {
          state: release?.isDraft ? 'draft' : 'missing',
          tag,
          version: tag.slice(1),
          notes,
        };
      } else if (release?.isDraft) {
        currentTag = { state: 'draft', tag, version: tag.slice(1), notes: release.body ?? '' };
      }
      continue;
    }

    if (!release || release.isDraft) {
      throw new Error(`Unpublished release ${tag} targets ${commit}. Rerun its failed workflow before releasing newer main commits.`);
    }
  }

  return currentTag;
}

async function main() {
  const eventName = process.env.GITHUB_EVENT_NAME ?? '';
  const branch = process.env.GITHUB_REF_NAME ?? '';
  const sha = process.env.GITHUB_SHA ?? git(['rev-parse', 'HEAD']);
  let recovery;
  let nextRelease;

  if (eventName === 'push' && branch === 'main') {
    recovery = await findRecovery(sha);
    if (!recovery) {
      const runGit = (args) => git(args);
      const lastRelease = highestReachableStableTag({ runGit, sha });
      const commits = collectCommits({ runGit, baseTag: lastRelease.tag, sha });
      nextRelease = await planFromCommits({
        commits,
        lastRelease,
        targetSha: sha,
        repositoryUrl: githubRepositoryUrl,
        cwd: repositoryDir,
      });
    }
  }

  const plan = resolveReleasePlan({ eventName, branch, sha, recovery, nextRelease });
  const outPath = path.join(repositoryDir, 'release-plan.json');
  await writeFile(outPath, `${JSON.stringify(plan, null, 2)}\n`, 'utf8');
  if (process.env.GITHUB_OUTPUT) {
    await writeFile(process.env.GITHUB_OUTPUT, `release=${plan.release}\nversion=${plan.version}\ntag=${plan.tag}\n`, { encoding: 'utf8', flag: 'a' });
  }
  console.log(plan.release ? `Planned ${plan.tag} at ${plan.targetSha}${plan.recovered ? ' (recovery)' : ''}` : 'No release planned.');
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
