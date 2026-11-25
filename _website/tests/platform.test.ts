import { describe, expect, it } from 'vitest';
import { detectPlatform, recommend, type PlatformHints } from '../src/lib/platform';

const UA = {
  windowsChrome:
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36',
  windowsFirefox: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0',
  macSafari:
    'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/19.0 Safari/605.1.15',
  macChrome:
    'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36',
  linuxFirefox: 'Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0',
  linuxArmFirefox: 'Mozilla/5.0 (X11; Linux aarch64; rv:140.0) Gecko/20100101 Firefox/140.0',
  ubuntuFirefox: 'Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0',
  android:
    'Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Mobile Safari/537.36',
  iphone:
    'Mozilla/5.0 (iPhone; CPU iPhone OS 19_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/19.0 Mobile/15E148 Safari/604.1',
  chromeOS:
    'Mozilla/5.0 (X11; CrOS x86_64 16000.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36',
  unknown: 'curl/8.10.0',
};

const pick = (hints: PlatformHints) => recommend(detectPlatform(hints));

describe('detectPlatform and recommend', () => {
  it('recommends the x64 installer on Windows without architecture hints', () => {
    expect(detectPlatform({ userAgent: UA.windowsFirefox })).toEqual({ os: 'windows', arch: 'unknown' });
    expect(pick({ userAgent: UA.windowsFirefox })).toEqual({ kind: 'asset', os: 'windows', key: 'windows-x64' });
  });

  it('recommends the ARM64 installer when Chromium reports an ARM Windows device', () => {
    const hints = { userAgent: UA.windowsChrome, uaPlatform: 'Windows', architecture: 'arm' };
    expect(pick(hints)).toEqual({ kind: 'asset', os: 'windows', key: 'windows-arm64' });
  });

  it('recommends the universal image for Safari, whose user agent hides Apple Silicon', () => {
    expect(detectPlatform({ userAgent: UA.macSafari, maxTouchPoints: 0 })).toEqual({ os: 'macos', arch: 'unknown' });
    expect(pick({ userAgent: UA.macSafari })).toEqual({ kind: 'asset', os: 'macos', key: 'macos-universal' });
  });

  it('recommends the Apple Silicon image when Chromium reports ARM', () => {
    const hints = { userAgent: UA.macChrome, uaPlatform: 'macOS', architecture: 'arm' };
    expect(pick(hints)).toEqual({ kind: 'asset', os: 'macos', key: 'macos-arm64' });
  });

  it('keeps the universal image when Chromium reports an Intel Mac', () => {
    const hints = { userAgent: UA.macChrome, uaPlatform: 'macOS', architecture: 'x86' };
    expect(pick(hints)).toEqual({ kind: 'asset', os: 'macos', key: 'macos-universal' });
  });

  it.each([
    [UA.linuxFirefox, 'x64'],
    [UA.ubuntuFirefox, 'x64'],
    [UA.linuxArmFirefox, 'arm64'],
  ] as const)('leads Linux visitors to the package list (%s)', (userAgent, arch) => {
    expect(pick({ userAgent })).toEqual({ kind: 'section', os: 'linux', arch });
  });

  it('uses navigator.platform for Linux when the user agent omits the architecture', () => {
    const userAgent = 'Mozilla/5.0 (X11; Linux) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36';
    expect(detectPlatform({ userAgent, platform: 'Linux aarch64' })).toEqual({ os: 'linux', arch: 'arm64' });
    expect(detectPlatform({ userAgent })).toEqual({ os: 'linux', arch: 'unknown' });
  });

  it.each([
    ['Android', { userAgent: UA.android }],
    ['iPhone', { userAgent: UA.iphone }],
    ['iPadOS desktop mode', { userAgent: UA.macSafari, maxTouchPoints: 5 }],
    ['ChromeOS', { userAgent: UA.chromeOS }],
    ['ChromeOS reported by client hints', { userAgent: UA.windowsChrome, uaPlatform: 'Chrome OS' }],
    ['an unknown agent', { userAgent: UA.unknown }],
  ])('offers all downloads on %s', (_label, hints: PlatformHints) => {
    expect(pick(hints)).toEqual({ kind: 'all' });
  });
});
