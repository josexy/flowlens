import type { DownloadKey } from './release';

export type DesktopOS = 'windows' | 'macos' | 'linux';
export type DetectedArch = 'x64' | 'arm64' | 'unknown';

export interface DetectedPlatform {
  os: DesktopOS | 'other';
  arch: DetectedArch;
}

/** Browser signals; every field except `userAgent` is optional. */
export interface PlatformHints {
  userAgent: string;
  /** `navigator.platform`. */
  platform?: string;
  /** `navigator.maxTouchPoints`, used to tell iPadOS from macOS. */
  maxTouchPoints?: number;
  /** `navigator.userAgentData.platform` (Chromium). */
  uaPlatform?: string;
  /** High-entropy `architecture` (`arm` or `x86`, Chromium). */
  architecture?: string;
}

export type Recommendation =
  | { kind: 'asset'; os: 'windows' | 'macos'; key: DownloadKey }
  | { kind: 'section'; os: 'linux'; arch: DetectedArch }
  | { kind: 'all' };

function osFromHints(hints: PlatformHints): DetectedPlatform['os'] {
  const ua = hints.userAgent.toLowerCase();
  if (/android|iphone|ipad|ipod|cros/.test(ua)) return 'other';

  const uaPlatform = hints.uaPlatform?.toLowerCase() ?? '';
  if (uaPlatform !== '') {
    if (uaPlatform === 'windows') return 'windows';
    if (uaPlatform === 'macos') return 'macos';
    if (uaPlatform === 'linux') return 'linux';
    return 'other';
  }

  if (/windows nt|win64|win32/.test(ua)) return 'windows';
  if (/macintosh|mac os x/.test(ua)) {
    // iPadOS Safari reports a desktop macOS user agent but has touch points.
    return (hints.maxTouchPoints ?? 0) > 1 ? 'other' : 'macos';
  }
  if (/linux|x11/.test(ua)) return 'linux';
  return 'other';
}

function archFromHints(hints: PlatformHints, os: DetectedPlatform['os']): DetectedArch {
  const architecture = hints.architecture?.toLowerCase() ?? '';
  if (architecture === 'arm') return 'arm64';
  if (architecture === 'x86') return 'x64';

  // macOS and Windows user agents are frozen (Apple Silicon reports "Intel"),
  // so only Linux user agents carry a trustworthy architecture.
  if (os !== 'linux') return 'unknown';
  const text = `${hints.userAgent} ${hints.platform ?? ''}`.toLowerCase();
  if (/aarch64|arm64|armv8/.test(text)) return 'arm64';
  if (/x86_64|amd64|x64/.test(text)) return 'x64';
  return 'unknown';
}

export function detectPlatform(hints: PlatformHints): DetectedPlatform {
  const os = osFromHints(hints);
  return { os, arch: os === 'other' ? 'unknown' : archFromHints(hints, os) };
}

/**
 * Picks the primary download. Windows defaults to x64 and macOS to the
 * universal image unless ARM is detected. Linux leads to its package list
 * because the browser cannot reveal the package format.
 */
export function recommend(platform: DetectedPlatform): Recommendation {
  switch (platform.os) {
    case 'windows':
      return { kind: 'asset', os: 'windows', key: platform.arch === 'arm64' ? 'windows-arm64' : 'windows-x64' };
    case 'macos':
      return { kind: 'asset', os: 'macos', key: platform.arch === 'arm64' ? 'macos-arm64' : 'macos-universal' };
    case 'linux':
      return { kind: 'section', os: 'linux', arch: platform.arch };
    default:
      return { kind: 'all' };
  }
}
