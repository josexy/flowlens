import { describe, expect, it } from 'vitest';
import fixture from './fixtures/release-2.0.1.json';
import {
  DOWNLOAD_KEYS,
  buildReleaseModel,
  classifyAsset,
  downloadHref,
  fallbackRelease,
  formatBytes,
  formatDate,
} from '../src/lib/release';

const REPO = 'josexy/flowlens';
const clone = <T>(value: T): T => structuredClone(value);

describe('classifyAsset', () => {
  it.each([
    ['flowlens-setup-2.0.1-amd64.exe', 'windows-x64'],
    ['flowlens-setup-2.0.1-arm64.exe', 'windows-arm64'],
    ['flowlens-2.0.1-arm64.dmg', 'macos-arm64'],
    ['flowlens-2.0.1.dmg', 'macos-universal'],
    ['flowlens_2.0.1_amd64.deb', 'linux-x64-deb'],
    ['flowlens_2.0.1_arm64.deb', 'linux-arm64-deb'],
    ['flowlens-2.0.1-1.x86_64.rpm', 'linux-x64-rpm'],
    ['flowlens-2.0.1-1.aarch64.rpm', 'linux-arm64-rpm'],
    ['flowlens-2.0.1-linux-amd64.tar.gz', 'linux-x64-tar'],
    ['flowlens-2.0.1-linux-arm64.tar.gz', 'linux-arm64-tar'],
    ['install.sh', 'install-sh'],
    ['SHA256SUMS', 'checksums'],
  ])('classifies %s as %s', (name, key) => {
    expect(classifyAsset(name, '2.0.1')).toBe(key);
  });

  it.each([
    'flowlens-2.0.1-windows-amd64.tar.gz',
    'flowlens-2.0.1-windows-arm64.tar.gz',
    'flowlens-2.0.1-darwin-arm64.tar.gz',
    'flowlens-2.0.1-darwin-universal.tar.gz',
    'flowlens-2.0.0-to-2.0.1-linux-amd64.delta',
    'update-linux-amd64.json',
    'install.ps1',
    'flowlens-setup-2.0.0-amd64.exe',
    'flowlens-2.0.0.dmg',
  ])('ignores %s', (name) => {
    expect(classifyAsset(name, '2.0.1')).toBeNull();
  });

  it('matches rewritten prerelease package versions by architecture', () => {
    expect(classifyAsset('flowlens_2.1.0~beta.1_amd64.deb', '2.1.0-beta.1')).toBe('linux-x64-deb');
    expect(classifyAsset('flowlens-2.1.0~beta.1-1.aarch64.rpm', '2.1.0-beta.1')).toBe('linux-arm64-rpm');
  });
});

describe('buildReleaseModel', () => {
  it('builds every download from the v2.0.1 release', () => {
    const model = buildReleaseModel(fixture, REPO);
    expect(model).not.toBeNull();
    expect(model!.tag).toBe('v2.0.1');
    expect(model!.version).toBe('2.0.1');
    expect(model!.publishedAt).toBe('2026-11-23T08:45:56Z');
    expect(Object.keys(model!.items).sort()).toEqual([...DOWNLOAD_KEYS].sort());
    expect(model!.items['windows-x64']).toEqual({
      name: 'flowlens-setup-2.0.1-amd64.exe',
      url: 'https://github.com/josexy/flowlens/releases/download/v2.0.1/flowlens-setup-2.0.1-amd64.exe',
      size: 11361612,
    });
  });

  it('links a missing asset to the release page', () => {
    const payload = clone(fixture);
    payload.assets = payload.assets.filter((asset) => !asset.name.endsWith('arm64.exe'));
    const model = buildReleaseModel(payload, REPO)!;
    expect(model.items['windows-arm64']).toBeUndefined();
    expect(downloadHref(model, 'windows-arm64')).toBe(fixture.html_url);
    expect(downloadHref(model, 'windows-x64')).toMatch(/flowlens-setup-2\.0\.1-amd64\.exe$/);
  });

  it.each([
    ['a draft', { draft: true }],
    ['a prerelease', { prerelease: true }],
    ['an empty tag', { tag_name: '' }],
    ['a foreign release page', { html_url: 'https://example.com/josexy/flowlens/releases/tag/v2.0.1' }],
    ['another repository', { html_url: 'https://github.com/someone/flowlens/releases/tag/v2.0.1' }],
    ['non-array assets', { assets: 'none' }],
    ['a non-string date', { published_at: 42 }],
  ])('rejects %s', (_label, patch) => {
    expect(buildReleaseModel({ ...clone(fixture), ...patch }, REPO)).toBeNull();
  });

  it('rejects non-object payloads and malformed assets', () => {
    expect(buildReleaseModel(null, REPO)).toBeNull();
    expect(buildReleaseModel([], REPO)).toBeNull();
    expect(buildReleaseModel('v2.0.1', REPO)).toBeNull();
    const payload = clone(fixture) as { assets: unknown[] };
    payload.assets.push({ name: 42 });
    expect(buildReleaseModel(payload, REPO)).toBeNull();
  });

  it('skips assets that are not uploaded, off-repository, or without a size', () => {
    const payload = clone(fixture);
    for (const asset of payload.assets) {
      if (asset.name === 'flowlens-2.0.1.dmg') asset.state = 'new';
      if (asset.name === 'flowlens-2.0.1-arm64.dmg') asset.browser_download_url = 'https://example.com/flowlens.dmg';
      if (asset.name === 'install.sh') (asset as { size: unknown }).size = 'big';
    }
    const model = buildReleaseModel(payload, REPO)!;
    expect(model.items['macos-universal']).toBeUndefined();
    expect(model.items['macos-arm64']).toBeUndefined();
    expect(model.items['install-sh']).toBeUndefined();
    expect(model.items['windows-x64']).toBeDefined();
  });
});

describe('fallbackRelease', () => {
  it('sends every download to the latest-release page', () => {
    const model = fallbackRelease(REPO);
    for (const key of DOWNLOAD_KEYS) {
      expect(downloadHref(model, key)).toBe('https://github.com/josexy/flowlens/releases/latest');
    }
  });
});

describe('formatting', () => {
  it('formats sizes like the GitHub release page', () => {
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(21137)).toBe('20.6 KB');
    expect(formatBytes(11361612)).toBe('10.8 MB');
  });

  it('formats dates in UTC per locale', () => {
    expect(formatDate('2026-11-23T08:45:56Z', 'en')).toBe('November 23, 2026');
    expect(formatDate('2026-11-23T08:45:56Z', 'zh-Hans')).toBe('2026年11月23日');
    expect(formatDate(null, 'en')).toBe('');
    expect(formatDate('not a date', 'en')).toBe('');
  });
});
