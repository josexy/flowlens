/**
 * Progressive enhancement for download navigation: recommends a package for
 * the visitor's platform and refreshes links when a newer release exists.
 * Without this script, every link in the HTML still works.
 */
import { detectPlatform, recommend, type PlatformHints, type Recommendation } from '../lib/platform';
import {
  buildReleaseModel,
  downloadHref,
  formatBytes,
  formatDate,
  type DownloadKey,
  type ReleaseModel,
} from '../lib/release';
import { format } from '../lib/text';

export interface DownloadConfig {
  repo: string;
  api: string;
  lang: string;
  release: ReleaseModel;
  strings: {
    downloadFor: Record<'windows' | 'macos' | 'linux', string>;
    captions: Record<string, string>;
    release: string;
    releaseUnknown: string;
  };
}

interface UserAgentData {
  platform?: string;
  getHighEntropyValues?(hints: string[]): Promise<{ architecture?: string }>;
}

const CACHE_TTL_MS = 10 * 60 * 1000;

function readConfig(): DownloadConfig | null {
  const node = document.getElementById('download-config');
  if (!node?.textContent) return null;
  try {
    return JSON.parse(node.textContent) as DownloadConfig;
  } catch {
    return null;
  }
}

function baseHints(): PlatformHints {
  const nav = navigator as Navigator & { userAgentData?: UserAgentData };
  return {
    userAgent: nav.userAgent,
    platform: nav.platform,
    maxTouchPoints: nav.maxTouchPoints,
    uaPlatform: nav.userAgentData?.platform || undefined,
  };
}

async function architectureHint(): Promise<string | undefined> {
  const data = (navigator as Navigator & { userAgentData?: UserAgentData }).userAgentData;
  if (!data?.getHighEntropyValues) return undefined;
  try {
    return (await data.getHighEntropyValues(['architecture'])).architecture || undefined;
  } catch {
    return undefined;
  }
}

/** Keeps only the fields the release model reads, so cached entries stay small. */
function trimPayload(payload: unknown): unknown {
  if (typeof payload !== 'object' || payload === null) return payload;
  const p = payload as Record<string, unknown>;
  return {
    tag_name: p.tag_name,
    html_url: p.html_url,
    draft: p.draft,
    prerelease: p.prerelease,
    published_at: p.published_at,
    assets: Array.isArray(p.assets)
      ? p.assets.map((asset: unknown) => {
          if (typeof asset !== 'object' || asset === null) return asset;
          const a = asset as Record<string, unknown>;
          return { name: a.name, size: a.size, state: a.state, browser_download_url: a.browser_download_url };
        })
      : p.assets,
  };
}

async function latestPayload(api: string): Promise<unknown> {
  const cacheKey = `flowlens:latest-release:${api}`;
  try {
    const cached = JSON.parse(sessionStorage.getItem(cacheKey) ?? 'null') as { at: number; payload: unknown } | null;
    if (cached && Date.now() - cached.at < CACHE_TTL_MS) return cached.payload;
  } catch {
    // Storage may be unavailable or hold stale data; fall through to the network.
  }

  const response = await fetch(api, { headers: { Accept: 'application/vnd.github+json' } });
  if (!response.ok) throw new Error(`GitHub API responded ${response.status}`);
  const payload = trimPayload(await response.json());
  try {
    sessionStorage.setItem(cacheKey, JSON.stringify({ at: Date.now(), payload }));
  } catch {
    // Caching is best effort.
  }
  return payload;
}

function applyRelease(config: DownloadConfig, release: ReleaseModel): void {
  for (const link of document.querySelectorAll<HTMLAnchorElement>('#download [data-asset-key]')) {
    const key = link.dataset.assetKey as DownloadKey;
    const item = release.items[key];
    link.href = downloadHref(release, key);
    const size = link.querySelector('[data-asset-size]');
    if (size) size.textContent = item ? formatBytes(item.size) : '';
    if (link.classList.contains('asset')) {
      if (item) link.title = item.name;
      else link.removeAttribute('title');
    }
  }

  const line = document.querySelector('[data-release-line]');
  if (line) {
    line.textContent = release.version
      ? format(config.strings.release, {
          version: release.version,
          date: formatDate(release.publishedAt, config.lang),
        })
      : config.strings.releaseUnknown;
  }
  const notes = document.querySelector<HTMLAnchorElement>('[data-release-notes]');
  if (notes) notes.href = release.htmlUrl;
}

function applyRecommendation(config: DownloadConfig, release: ReleaseModel, choice: Recommendation): void {
  for (const node of document.querySelectorAll('[data-detected]')) node.removeAttribute('data-detected');
  for (const node of document.querySelectorAll('[data-recommended]')) node.removeAttribute('data-recommended');
  if (choice.kind === 'all') return;

  const primary = document.querySelector<HTMLAnchorElement>('[data-primary-download]');
  const label = document.querySelector('[data-primary-label]');
  const caption = document.querySelector('[data-primary-caption]');
  const allDownloads = document.querySelector<HTMLElement>('[data-all-downloads]');
  const version = release.version ? `v${release.version}` : '';

  document.querySelector(`[data-platform="${choice.os}"]`)?.setAttribute('data-detected', '');
  if (label) label.textContent = config.strings.downloadFor[choice.os];
  if (allDownloads) allDownloads.hidden = false;

  if (choice.kind === 'asset') {
    const item = release.items[choice.key];
    if (primary) primary.href = downloadHref(release, choice.key);
    if (caption) {
      caption.textContent = [config.strings.captions[choice.key], item && formatBytes(item.size), version]
        .filter(Boolean)
        .join(' · ');
    }
    document.querySelector(`#download [data-asset-key="${choice.key}"]`)?.setAttribute('data-recommended', '');
  } else {
    if (primary) primary.href = '#download-linux';
    if (caption) {
      caption.textContent = [config.strings.captions[`linux-${choice.arch}`], version].filter(Boolean).join(' · ');
    }
    if (choice.arch !== 'unknown') {
      document.querySelector(`[data-linux-arch="${choice.arch}"]`)?.setAttribute('data-recommended', '');
    }
  }
}

function keepSectionOnLanguageSwitch(): void {
  for (const link of document.querySelectorAll<HTMLAnchorElement>('[data-locale-switch]')) {
    link.addEventListener('click', () => {
      if (window.location.hash) link.hash = window.location.hash;
    });
  }
}

async function main(): Promise<void> {
  keepSectionOnLanguageSwitch();
  const config = readConfig();
  if (!config) return;

  let release = config.release;
  const hints = baseHints();
  let choice = recommend(detectPlatform(hints));
  applyRecommendation(config, release, choice);

  const architecture = await architectureHint();
  if (architecture) {
    choice = recommend(detectPlatform({ ...hints, architecture }));
    applyRecommendation(config, release, choice);
  }

  try {
    const latest = buildReleaseModel(await latestPayload(config.api), config.repo);
    if (latest && latest.tag !== release.tag) {
      release = latest;
      applyRelease(config, release);
      applyRecommendation(config, release, choice);
    }
  } catch {
    // Keep the links the page was built with.
  }
}

void main();
