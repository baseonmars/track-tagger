import semver from 'semver';

export function collectCommits({ runGit, baseTag, sha }) {
  const range = baseTag ? `${baseTag}..${sha}` : sha;
  const records = runGit(['log', '--format=%H%x00%B%x00', range]).split('\0');
  const commits = [];
  for (let index = 0; index + 1 < records.length; index += 2) {
    const hash = records[index].trim();
    const message = records[index + 1].trim();
    if (hash && message) commits.push({ hash, message });
  }
  return commits;
}

export function highestReachableStableTag({ runGit, sha }) {
  const tags = runGit(['tag', '--list', 'v[0-9]*']).split(/\r?\n/)
    .filter((tag) => semver.valid(tag) && semver.prerelease(tag) === null)
    .map((tag) => ({ tag, version: semver.clean(tag) }))
    .sort((left, right) => semver.rcompare(left.version, right.version));
  for (const candidate of tags) {
    try {
      runGit(['merge-base', '--is-ancestor', candidate.tag, sha]);
      return { tag: candidate.tag, gitTag: candidate.tag, version: candidate.version, gitHead: runGit(['rev-parse', `${candidate.tag}^{commit}`]) };
    } catch (error) {
      // An unreachable tag does not define the release baseline for this SHA.
      if (error.status !== 1) throw error;
    }
  }
  return { tag: null, gitTag: null, version: '0.0.0', gitHead: null };
}
