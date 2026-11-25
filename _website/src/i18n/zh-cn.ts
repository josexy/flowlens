import type { Dictionary } from './en';

/** Simplified Chinese copy. Plain, user-facing language; details live in the user guide. */
export const zhCn: Dictionary = {
  lang: 'zh-Hans',
  meta: {
    title: 'FlowLens：看清应用发出的每一个请求',
    description:
      'FlowLens 是一款免费的桌面应用，支持 Windows、macOS 与 Linux。它能捕获浏览器和应用的网络流量，显示每个请求来自哪个应用，并支持查看、编辑与重放请求。',
  },
  nav: {
    skip: '跳到正文',
    label: '主导航',
    home: 'FlowLens 首页',
    features: '功能',
    howItWorks: '使用方法',
    download: '下载',
    github: 'GitHub',
    otherLanguage: 'English',
    otherLanguageLabel: 'English version',
  },
  hero: {
    title: '看清应用发出的每一个请求。',
    lead: 'FlowLens 能捕获浏览器和应用的网络流量，显示每个请求来自哪个应用，并让你查看、编辑与重放请求。所有记录都保存在你自己的电脑上。',
    download: '下载',
    downloadFor: {
      windows: '下载 Windows 版',
      macos: '下载 macOS 版',
      linux: '下载 Linux 版',
    },
    caption: '免费开源，支持 Windows、macOS 与 Linux',
    allDownloads: '其他平台与版本',
    viewOnGitHub: '在 GitHub 上查看',
  },
  screenshot: {
    label: '截图主题',
    light: '浅色',
    dark: '深色',
    altLight: 'FlowLens 浅色主题界面，显示已捕获的请求列表和其中一个请求的详情',
    altDark: 'FlowLens 深色主题界面，显示已捕获的请求列表和其中一个请求的详情',
  },
  features: {
    title: '看懂网络流量所需的一切',
    lead: '在一个桌面应用中完成请求的捕获、查看与重放。',
    items: {
      capture: {
        title: '捕获网页与应用流量',
        body: '记录 HTTP 和 HTTPS 请求、WebSocket 消息以及实时事件流。随时暂停录制，不会打断正在运行的应用。',
      },
      apps: {
        title: '知道是哪个应用发出的',
        body: '本机发出的连接会显示对应的应用及其图标。可按应用、网站或路径筛选，快速找到需要的请求。',
      },
      inspect: {
        title: '查看每一个细节',
        body: '请求头、Cookie、耗时与大小一目了然。正文可按格式化文本、图片或原始字节查看。',
      },
      replay: {
        title: '编辑与重放',
        body: '一键重发任意请求，或修改后再发送。内置 WebSocket 客户端，还可选用 Python 脚本。',
      },
      rewrite: {
        title: '实时修改流量',
        body: '通过重写规则，在流量经过时重定向 URL，修改请求头、查询参数和文本内容。',
      },
      organize: {
        title: '保存与分享',
        body: '把请求保存到集合，从历史记录中重新打开以往的会话，导入 HAR 文件，或导出为 HAR、CSV。',
      },
    },
  },
  howItWorks: {
    title: '三步开始使用',
    steps: [
      {
        title: '开始捕获',
        body: '点击“启动代理”，然后将浏览器或应用的代理设置为 {address}。在 Windows 和 macOS 上，FlowLens 也可以帮你设置系统代理。',
      },
      {
        title: '开启 HTTPS 解密',
        body: '如需查看加密流量，打开 设置 › 代理 并信任 FlowLens 证书。在 Windows 和 macOS 上，点击一个按钮即可安装。',
      },
      {
        title: '查看与重放',
        body: '点击任意请求，即可看到发送和收到的完整内容。可以直接重发，也可以修改后再发送。',
      },
    ],
    guide: '需要帮助？阅读用户指南（英文）',
  },
  download: {
    title: '下载 FlowLens',
    release: '版本 {version}，发布于 {date}',
    releaseUnknown: '最新版本',
    releaseNotes: '更新内容',
    allReleases: '全部版本',
    detected: '当前系统',
    platforms: {
      windows: { name: 'Windows', detail: '安装程序' },
      macos: { name: 'macOS', detail: 'macOS {minimum} 及以上' },
      linux: { name: 'Linux', detail: '适用于 Debian、Ubuntu、Fedora 等发行版' },
    },
    assets: {
      'windows-x64': '大多数电脑（x64）',
      'windows-arm64': 'ARM 电脑（ARM64）',
      'macos-arm64': 'Apple 芯片（M1 及以上）',
      'macos-universal': '所有 Mac（Intel 或 Apple 芯片）',
      'linux-x64-deb': 'Debian、Ubuntu',
      'linux-x64-rpm': 'Fedora、RHEL',
      'linux-x64-tar': '其他发行版',
      'linux-arm64-deb': 'Debian、Ubuntu',
      'linux-arm64-rpm': 'Fedora、RHEL',
      'linux-arm64-tar': '其他发行版',
      'install-sh': 'install.sh',
      checksums: 'SHA256SUMS',
    },
    captions: {
      'windows-x64': '适用于大多数电脑的安装程序（x64）',
      'windows-arm64': '适用于 ARM 电脑的安装程序',
      'macos-arm64': '适用于搭载 Apple 芯片的 Mac',
      'macos-universal': '适用于所有 Mac（Intel 或 Apple 芯片）',
      'linux-x64': '请选择适合你发行版的安装包（x64）',
      'linux-arm64': '请选择适合你发行版的安装包（ARM64）',
      'linux-unknown': '请选择适合你发行版的安装包',
    },
    linuxArch: { x64: 'x64（Intel 或 AMD）', arm64: 'ARM64' },
    installScript: '使用 .tar.gz 时，请将 {script} 下载到同一文件夹，然后运行 {command}。',
    notes: {
      title: '安装须知',
      warning: 'Windows 安装程序未进行代码签名，Mac 应用也未经 Apple 公证，因此首次打开 FlowLens 时，系统可能会要求你确认。',
      updates: '可通过状态栏的更新按钮检查新版本。从 1.0.x 升级？请从本页手动安装一次最新版本，原有数据会保留。',
      linux: 'Linux 版需要 GTK 3。使用包管理器安装 .deb 或 .rpm 时会自动安装。',
      checksums: { before: '想校验下载的文件？请对照 ', link: 'SHA-256 校验值', after: '。' },
    },
  },
  privacy: {
    title: '捕获的数据只保存在你的电脑上',
    lead: 'FlowLens 会把捕获记录、历史、集合和设置保存在你的电脑上。请负责任地使用。',
    points: [
      '仅检查你拥有或已获授权的设备与网络上的流量。',
      '捕获记录和导出文件可能包含密码等隐私信息，FlowLens 不会将其移除，分享文件时请谨慎。',
      '不再需要查看 HTTPS 流量时，请从系统中移除 FlowLens 证书。',
    ],
    more: '阅读安全说明（英文）',
  },
  footer: {
    license: '{license} 许可证',
    guide: '用户指南',
    releases: '更新日志',
    issues: '反馈问题',
    security: '安全',
    github: 'GitHub',
  },
};
