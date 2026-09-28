import DOMPurify from 'dompurify'
import { marked } from 'marked'

const allowedProtocols = new Set(['http:', 'https:'])

export function isSafeUpdateURL(value: string): boolean {
  try {
    return allowedProtocols.has(new URL(value).protocol)
  } catch {
    return false
  }
}

export function renderUpdateNotes(markdown: string): string {
  if (!markdown.trim()) return ''
  const html = marked.parse(markdown, {
    async: false,
    gfm: true,
  }) as string
  return DOMPurify.sanitize(html, {
    ALLOWED_URI_REGEXP: /^https?:/i,
    FORBID_TAGS: [
      'button',
      'form',
      'iframe',
      'img',
      'input',
      'option',
      'select',
      'style',
      'textarea',
    ],
  })
}
