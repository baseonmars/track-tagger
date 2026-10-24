export function assertReleaseKind(release, expectedPrerelease, tag) {
  if (release.isPrerelease !== expectedPrerelease) {
    throw new Error(`Release ${tag} has prerelease=${release.isPrerelease}; expected prerelease=${expectedPrerelease}`);
  }
}

export async function publishRelease({
  tag,
  targetSha,
  expectedPrerelease,
  expectedAssets,
  prepareAssets,
  getRelease,
  verifyTag,
  ensureTag,
  createDraft,
  uploadDraft,
  getAssetNames,
  publishDraft,
}) {
  await prepareAssets();
  let release = await getRelease(tag);

  if (release) {
    assertReleaseKind(release, expectedPrerelease, tag);
    if (!release.isDraft) {
      await verifyTag(tag, targetSha);
      const currentAssets = await getAssetNames(tag);
      const missing = expectedAssets.filter((name) => !currentAssets.includes(name));
      if (missing.length) throw new Error(`Release ${tag} is already public but is missing assets: ${missing.join(', ')}`);
      return { alreadyPublished: true };
    }
  }

  await ensureTag(tag, targetSha);
  if (!release) await createDraft(tag, targetSha);
  await uploadDraft(tag, expectedAssets);

  release = await getRelease(tag);
  if (!release?.isDraft || release.tagName !== tag) throw new Error(`Release ${tag} is not a draft for the expected tag.`);
  assertReleaseKind(release, expectedPrerelease, tag);
  const currentAssets = await getAssetNames(tag);
  const missing = expectedAssets.filter((name) => !currentAssets.includes(name));
  if (missing.length) throw new Error(`Draft ${tag} is missing uploaded assets: ${missing.join(', ')}`);

  await publishDraft(release);
  return { alreadyPublished: false };
}
