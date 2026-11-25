/** English copy. Plain, user-facing language; details live in the user guide. */
export const en = {
  /** Language tag for `<html lang>` and Intl formatting. */
  lang: 'en',
  meta: {
    title: 'FlowLens: see every request your apps make',
    description:
      'FlowLens is a free desktop app for Windows, macOS, and Linux that captures web traffic from your browser and apps, shows which app sent each request, and lets you inspect, edit, and replay it.',
  },
  nav: {
    skip: 'Skip to content',
    label: 'Main',
    home: 'FlowLens home',
    features: 'Features',
    howItWorks: 'How it works',
    download: 'Download',
    github: 'GitHub',
    otherLanguage: '中文',
    otherLanguageLabel: '简体中文版本',
  },
  hero: {
    title: 'See every request your apps make.',
    lead: 'FlowLens captures web traffic from your browser and apps, shows which app sent each request, and lets you inspect, edit, and replay it. Everything it records stays on your computer.',
    download: 'Download',
    downloadFor: {
      windows: 'Download for Windows',
      macos: 'Download for macOS',
      linux: 'Download for Linux',
    },
    caption: 'Free and open source for Windows, macOS, and Linux',
    allDownloads: 'Other platforms and versions',
    viewOnGitHub: 'View on GitHub',
  },
  screenshot: {
    label: 'Screenshot theme',
    light: 'Light',
    dark: 'Dark',
    altLight: 'FlowLens in the light theme, showing a list of captured requests and the details of one request',
    altDark: 'FlowLens in the dark theme, showing a list of captured requests and the details of one request',
  },
  features: {
    title: 'Everything you need to understand your traffic',
    lead: 'Capture, inspect, and replay requests in one desktop app.',
    items: {
      capture: {
        title: 'Capture web and app traffic',
        body: 'Record HTTP and HTTPS requests, WebSocket messages, and live event streams. Pause recording any time without interrupting your apps.',
      },
      apps: {
        title: 'See which app sent it',
        body: 'Connections from your computer show the app that made them, with its icon. Filter by app, site, or path to find what you need.',
      },
      inspect: {
        title: 'Inspect every detail',
        body: 'Headers, cookies, timing, and sizes at a glance. View bodies as formatted text, images, or raw bytes.',
      },
      replay: {
        title: 'Edit and replay',
        body: 'Resend any request, or change it and send it again. A WebSocket client and optional Python scripting are built in.',
      },
      rewrite: {
        title: 'Change traffic on the fly',
        body: 'Rewrite rules redirect URLs and change headers, query parameters, and text as traffic passes through.',
      },
      organize: {
        title: 'Save and share your work',
        body: 'Keep requests in collections, reopen past sessions from history, import HAR files, and export to HAR or CSV.',
      },
    },
  },
  howItWorks: {
    title: 'Get started in three steps',
    steps: [
      {
        title: 'Start capturing',
        body: 'Click Start Proxy, then set your browser or app to use the proxy at {address}. On Windows and macOS, FlowLens can set the system proxy for you.',
      },
      {
        title: 'Turn on HTTPS',
        body: 'To see encrypted traffic, open Settings › Proxy and trust the FlowLens certificate. On Windows and macOS, one button installs it.',
      },
      {
        title: 'Explore and replay',
        body: 'Click any request to see exactly what was sent and received. Resend it as is, or edit it first.',
      },
    ],
    guide: 'Need help? Read the user guide',
  },
  download: {
    title: 'Download FlowLens',
    release: 'Version {version}, released {date}',
    releaseUnknown: 'Latest version',
    releaseNotes: "What's new",
    allReleases: 'All versions',
    detected: 'Your system',
    platforms: {
      windows: { name: 'Windows', detail: 'Installer' },
      macos: { name: 'macOS', detail: 'macOS {minimum} or later' },
      linux: { name: 'Linux', detail: 'Packages for Debian, Ubuntu, Fedora, and more' },
    },
    assets: {
      'windows-x64': 'Most PCs (x64)',
      'windows-arm64': 'ARM PCs (ARM64)',
      'macos-arm64': 'Apple Silicon (M1 or later)',
      'macos-universal': 'Any Mac (Intel or Apple Silicon)',
      'linux-x64-deb': 'Debian, Ubuntu',
      'linux-x64-rpm': 'Fedora, RHEL',
      'linux-x64-tar': 'Other distributions',
      'linux-arm64-deb': 'Debian, Ubuntu',
      'linux-arm64-rpm': 'Fedora, RHEL',
      'linux-arm64-tar': 'Other distributions',
      'install-sh': 'install.sh',
      checksums: 'SHA256SUMS',
    },
    /** Primary button captions once the visitor's platform is known. */
    captions: {
      'windows-x64': 'Installer for most PCs (x64)',
      'windows-arm64': 'Installer for ARM PCs',
      'macos-arm64': 'For Macs with Apple Silicon',
      'macos-universal': 'For any Mac, Intel or Apple Silicon',
      'linux-x64': 'Choose a package for your distribution (x64)',
      'linux-arm64': 'Choose a package for your distribution (ARM64)',
      'linux-unknown': 'Choose a package for your distribution',
    },
    linuxArch: { x64: 'x64 (Intel or AMD)', arm64: 'ARM64' },
    installScript: 'For the .tar.gz, also download {script} into the same folder and run {command}.',
    notes: {
      title: 'Good to know',
      warning:
        "The Windows installer isn't code-signed and the Mac app isn't notarized by Apple, so your system may ask you to confirm the first time you open FlowLens.",
      updates:
        'Use the update button in the status bar to check for new versions. Upgrading from 1.0.x? Install the latest version from this page once. Your data is kept.',
      linux: 'The Linux version needs GTK 3. Installing the .deb or .rpm with your package manager adds it automatically.',
      checksums: { before: 'Want to verify your download? Compare it with the ', link: 'SHA-256 checksums', after: '.' },
    },
  },
  privacy: {
    title: 'Your captures stay on your computer',
    lead: 'FlowLens saves captures, history, collections, and settings on your computer. Please use it responsibly.',
    points: [
      'Only inspect traffic on devices and networks you own or have permission to inspect.',
      'Captures and exported files can include passwords and other private data. FlowLens doesn\'t remove it, so share files with care.',
      'Remove the FlowLens certificate from your system when you no longer need to see HTTPS traffic.',
    ],
    more: 'Read the security notes',
  },
  footer: {
    license: '{license} license',
    guide: 'User guide',
    releases: 'Release notes',
    issues: 'Report an issue',
    security: 'Security',
    github: 'GitHub',
  },
};

export type Dictionary = typeof en;
