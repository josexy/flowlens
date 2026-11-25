import { LATEST_RELEASE_API, REPO } from '../config';
import { buildReleaseModel, fallbackRelease, type ReleaseModel } from './release';

const TIMEOUT_MS = 15_000;

let pending: Promise<ReleaseModel> | undefined;

async function fetchLatestRelease(): Promise<ReleaseModel> {
  const headers: Record<string, string> = {
    Accept: 'application/vnd.github+json',
    'X-GitHub-Api-Version': '2022-11-28',
  };
  const token = process.env.GITHUB_TOKEN;
  if (token) headers.Authorization = `Bearer ${token}`;

  const response = await fetch(LATEST_RELEASE_API, { headers, signal: AbortSignal.timeout(TIMEOUT_MS) });
  if (!response.ok) throw new Error(`GitHub API responded ${response.status} ${response.statusText}`);
  const model = buildReleaseModel(await response.json(), REPO);
  if (!model) throw new Error('the latest release payload is not a valid published release');
  return model;
}

/**
 * Latest published release, fetched once per build. With
 * SITE_REQUIRE_RELEASE=true a failure fails the build; otherwise every
 * download falls back to the latest-release page.
 */
export function loadRelease(): Promise<ReleaseModel> {
  pending ??= fetchLatestRelease().catch((error: unknown) => {
    const reason = error instanceof Error ? error.message : String(error);
    if (process.env.SITE_REQUIRE_RELEASE === 'true') {
      throw new Error(`Release data is required but unavailable: ${reason}`);
    }
    console.warn(`[website] Using latest-release page links; release data unavailable: ${reason}`);
    return fallbackRelease(REPO);
  });
  return pending;
}
