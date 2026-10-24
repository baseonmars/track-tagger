function normalizeRelease(release) {
  return {
    ...release,
    databaseId: release.id,
    isDraft: release.draft,
    isPrerelease: release.prerelease,
    tagName: release.tag_name,
  };
}

function apiHeaders(token) {
  return {
    accept: 'application/vnd.github+json',
    authorization: `Bearer ${token}`,
    'x-github-api-version': '2022-11-28',
  };
}

function nextLink(linkHeader) {
  for (const entry of (linkHeader ?? '').split(',')) {
    const match = entry.trim().match(/^<([^>]+)>\s*;\s*rel="?next"?(?:\s*;|$)/i);
    if (match) return match[1];
  }
  return null;
}

function checkedNextLink(link, currentUrl, expectedPath) {
  const next = new URL(link, currentUrl);
  if (next.origin !== currentUrl.origin || next.pathname !== expectedPath || next.username || next.password || next.hash) {
    throw new Error(`GitHub release pagination returned an unexpected URL: ${next.href}`);
  }
  return next;
}

async function findDraftByTag({ apiUrl, repository, tag, token, fetcher }) {
  const releasesUrl = new URL(`${apiUrl.replace(/\/$/, '')}/repos/${repository.split('/').map(encodeURIComponent).join('/')}/releases`);
  releasesUrl.searchParams.set('per_page', '100');
  releasesUrl.searchParams.set('page', '1');
  const visited = new Set();
  let url = releasesUrl;

  while (url) {
    if (visited.has(url.href) || visited.size >= 1000) {
      throw new Error('GitHub release pagination did not terminate.');
    }
    visited.add(url.href);

    const response = await fetcher(url, { headers: apiHeaders(token) });
    if (!response.ok) {
      throw new Error(`GitHub release list lookup for ${tag} failed with HTTP ${response.status}`);
    }
    const releases = await response.json();
    if (!Array.isArray(releases)) throw new Error('GitHub release list response was not an array.');
    const release = releases.find((candidate) => candidate?.tag_name === tag);
    if (release) return normalizeRelease(release);

    const link = nextLink(response.headers.get('link'));
    url = link ? checkedNextLink(link, url, releasesUrl.pathname) : null;
  }
  return null;
}

export async function getReleaseByTag({ repository, tag, token, apiUrl = 'https://api.github.com', fetcher = fetch }) {
  const tagUrl = `${apiUrl.replace(/\/$/, '')}/repos/${repository.split('/').map(encodeURIComponent).join('/')}/releases/tags/${encodeURIComponent(tag)}`;
  const response = await fetcher(tagUrl, { headers: apiHeaders(token) });
  if (response.status === 404) {
    // Get a release by tag returns published releases only. The authenticated
    // list endpoint also returns drafts to callers with repository push access.
    return findDraftByTag({ apiUrl, repository, tag, token, fetcher });
  }
  if (!response.ok) throw new Error(`GitHub release lookup for ${tag} failed with HTTP ${response.status}`);
  return normalizeRelease(await response.json());
}
