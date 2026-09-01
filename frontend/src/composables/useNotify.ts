import { useToast } from '@nuxt/ui/composables/useToast'

export interface NotifyApi {
  success: (detail: string, summary?: string, life?: number) => void
  info: (detail: string, summary?: string, life?: number) => void
  warn: (detail: string, summary?: string, life?: number) => void
  warning: (detail: string, summary?: string, life?: number) => void
  error: (detail: string, summary?: string, life?: number) => void
}

type NotifySeverity = 'success' | 'info' | 'warn' | 'error'

// Map the app's severities onto Nuxt UI toast colors + icons.
const SEVERITY_COLOR = {
  success: 'success',
  info: 'info',
  warn: 'warning',
  error: 'error',
} as const

const SEVERITY_ICON = {
  success: 'i-lucide-circle-check',
  info: 'i-lucide-info',
  warn: 'i-lucide-triangle-alert',
  error: 'i-lucide-circle-x',
} as const

function defaultLife(severity: NotifySeverity) {
  if (severity === 'error') return 4000
  if (severity === 'warn') return 3000
  return 1800
}

export function useNotify(): NotifyApi {
  const toast = useToast()

  function add(severity: NotifySeverity, detail: string, summary?: string, life?: number) {
    const hasSummary = summary !== undefined && summary !== ''

    // One-line toasts put the message in `title` (no empty description row);
    // when a summary is given it becomes the title and detail the description.
    toast.add({
      title: hasSummary ? summary : detail,
      ...(hasSummary ? { description: detail } : {}),
      color: SEVERITY_COLOR[severity],
      icon: SEVERITY_ICON[severity],
      duration: life ?? defaultLife(severity),
    })
  }

  return {
    success: (detail, summary, life) => add('success', detail, summary, life),
    info: (detail, summary, life) => add('info', detail, summary, life),
    warn: (detail, summary, life) => add('warn', detail, summary, life),
    warning: (detail, summary, life) => add('warn', detail, summary, life),
    error: (detail, summary, life) => add('error', detail, summary, life),
  }
}
