/**
 * Pure release model shared by the build and the browser.
 *
 * Asset patterns mirror the `check-assets` job in .github/workflows/release.yml.
 * Keep both in step when release asset names change.
 */

export const DOWNLOAD_KEYS = [
  'windows-x64',
  'windows-arm64',
  'macos-arm64',
  'macos-universal',
  'linux-x64-deb',
  'linux-x64-rpm',
  'linux-x64-tar',
  'linux-arm64-deb',
  'linux-arm64-rpm',
  'linux-arm64-tar',
  'install-sh',
  'checksums',
] as const;

export type DownloadKey = (typeof DOWNLOAD_KEYS)[number];

export interface DownloadAsset {
  name: string;
  url: string;
  /** Size in bytes. */
  size: number;
}

export interface ReleaseModel {
  /** Release tag such as `v2.0.1`; empty when no release data is available. */
  tag: string;
  /** Version without the leading `v`; empty when no release data is available. */
  version: string;
  /** ISO 8601 publication time, or null when unknown. */
  publishedAt: string | null;
  /** Release page; every missing asset links here. */
  htmlUrl: string;
  items: Partial<Record<DownloadKey, DownloadAsset>>;
}

const DEB = /^flowlens_.*_(amd64|arm64)\.deb$/;
const RPM = /^flowlens-.*\.(x86_64|aarch64)\.rpm$/;

export function versionFromTag(tag: string): string {
  return tag.startsWith('v') ? tag.slice(1) : tag;
}

/**
 * Maps a release asset name to its download key. Update archives for Windows
 * and macOS, delta files, update manifests, and anything unknown return null.
 */
export function classifyAsset(name: string, version: string): DownloadKey | null {
  if (version !== '') {
    switch (name) {
      case `flowlens-setup-${version}-amd64.exe`:
        return 'windows-x64';
      case `flowlens-setup-${version}-arm64.exe`:
        return 'windows-arm64';
      case `flowlens-${version}-arm64.dmg`:
        return 'macos-arm64';
      case `flowlens-${version}.dmg`:
        return 'macos-universal';
      case `flowlens-${version}-linux-amd64.tar.gz`:
        return 'linux-x64-tar';
      case `flowlens-${version}-linux-arm64.tar.gz`:
        return 'linux-arm64-tar';
    }
  }
  if (name === 'install.sh') return 'install-sh';
  if (name === 'SHA256SUMS') return 'checksums';

  // Package managers may rewrite prerelease versions, so match by architecture only.
  const deb = DEB.exec(name);
  if (deb) return deb[1] === 'amd64' ? 'linux-x64-deb' : 'linux-arm64-deb';
  const rpm = RPM.exec(name);
  if (rpm) return rpm[1] === 'x86_64' ? 'linux-x64-rpm' : 'linux-arm64-rpm';
  return null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** Release data before any network response: every link opens the latest-release page. */
export function fallbackRelease(repo: string): ReleaseModel {
  return {
    tag: '',
    version: '',
    publishedAt: null,
    htmlUrl: `https://github.com/${repo}/releases/latest`,
    items: {},
  };
}

/**
 * Validates an untrusted GitHub "latest release" payload and builds the model.
 * Returns null for drafts, prereleases, malformed data, or URLs outside the
 * repository's releases, so callers keep their previous links.
 */
export function buildReleaseModel(payload: unknown, repo: string): ReleaseModel | null {
  if (!isRecord(payload)) return null;
  const { tag_name: tag, html_url: htmlUrl, draft, prerelease, published_at: publishedAt, assets } = payload;
  const releasesPrefix = `https://github.com/${repo}/releases/`;

  if (typeof tag !== 'string' || tag === '') return null;
  if (typeof htmlUrl !== 'string' || !htmlUrl.startsWith(releasesPrefix)) return null;
  if (draft === true || prerelease === true) return null;
  if (publishedAt !== null && publishedAt !== undefined && typeof publishedAt !== 'string') return null;
  if (!Array.isArray(assets)) return null;

  const version = versionFromTag(tag);
  const downloadPrefix = `${releasesPrefix}download/`;
  const items: ReleaseModel['items'] = {};

  for (const asset of assets) {
    if (!isRecord(asset)) return null;
    const { name, browser_download_url: url, size, state } = asset;
    if (typeof name !== 'string' || typeof url !== 'string') return null;
    if (state !== undefined && state !== 'uploaded') continue;
    if (!url.startsWith(downloadPrefix)) continue;
    if (typeof size !== 'number' || !Number.isFinite(size) || size < 0) continue;

    const key = classifyAsset(name, version);
    if (key && !items[key]) items[key] = { name, url, size };
  }

  return { tag, version, publishedAt: publishedAt ?? null, htmlUrl, items };
}

/** The asset URL, or the release page when the asset is missing. */
export function downloadHref(release: ReleaseModel, key: DownloadKey): string {
  return release.items[key]?.url ?? release.htmlUrl;
}

/** Formats bytes the way GitHub's release page does (binary units, one decimal). */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB'];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value.toFixed(1)} ${units[unit]}`;
}

/** Formats a publication date in UTC so build and browser output agree. */
export function formatDate(iso: string | null, locale: string): string {
  if (!iso) return '';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  return new Intl.DateTimeFormat(locale, { dateStyle: 'long', timeZone: 'UTC' }).format(date);
}
