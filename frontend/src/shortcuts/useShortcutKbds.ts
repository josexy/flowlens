import { computed } from 'vue'
import { useSettingStore } from '@/stores/setting'
import { resolveShortcut, shortcutToUKbdKeys } from './binding'
import { shortcutCatalogById } from './catalog'

export function useShortcutKbds(commandId: string) {
  const settingStore = useSettingStore()

  return computed(() => {
    const command = shortcutCatalogById.get(commandId)
    return command
      ? shortcutToUKbdKeys(resolveShortcut(command, settingStore.shortcutConfig).binding)
      : []
  })
}
