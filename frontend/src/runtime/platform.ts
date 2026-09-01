export function inferPlatformFromUserAgent(): 'windows' | 'darwin' | 'linux' {
  const value = navigator.userAgent.toLowerCase()
  if (value.includes('mac')) {
    return 'darwin'
  }
  if (value.includes('linux')) {
    return 'linux'
  }
  return 'windows'
}
