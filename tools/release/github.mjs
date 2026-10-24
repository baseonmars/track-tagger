export async function getReleaseByTag({ repository, tag, token, apiUrl = 'https://api.github.com', fetcher = fetch }) {
  const response = await fetcher(`${apiUrl}/repos/${repository}/releases/tags/${encodeURIComponent(tag)}`, {
    headers: {
      accept: 'application/vnd.github+json',
      authorization: `Bearer ${token}`,
      'x-github-api-version': '2022-11-28',
    },
  });
  if (response.status === 404) return null;
  if (!response.ok) throw new Error(`GitHub release lookup for ${tag} failed with HTTP ${response.status}`);
  const release = await response.json();
  return {
    ...release,
    databaseId: release.id,
    isDraft: release.draft,
    isPrerelease: release.prerelease,
    tagName: release.tag_name,
  };
}
