import { Clipboard } from '@wailsio/runtime'

export function copyText(text: string) {
  return Clipboard.SetText(text)
}
