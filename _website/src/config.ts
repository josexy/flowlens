import mygo from '../../mygo.json';

/** GitHub `owner/name` of the release repository; mygo.json's update source is authoritative. */
export const REPO: string = mygo.updates.github;

export const REPO_URL = `https://github.com/${REPO}`;
export const RELEASES_URL = `${REPO_URL}/releases`;
export const LATEST_RELEASE_URL = `${RELEASES_URL}/latest`;
export const LATEST_RELEASE_API = `https://api.github.com/repos/${REPO}/releases/latest`;

/** Absolute URL of a file on the repository's main branch. */
export function repoFile(path: string): string {
  return `${REPO_URL}/blob/main/${path}`;
}

export const LICENSE_NAME: string = mygo.license;
export const COPYRIGHT: string = mygo.copyright;
export const MIN_MACOS: string = mygo.macos.minimumSystemVersion;
