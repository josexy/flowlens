<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TreeItem } from '@nuxt/ui'
import type { ApiCollectionFolderTreeOption, SaveApiRequestForm } from '@/types/api-collection'

const props = defineProps<{
  show: boolean
  defaultName: string
  defaultParentId: string
  folderOptions: ApiCollectionFolderTreeOption[]
  saving?: boolean
}>()

const emit = defineEmits<{
  'update:show': [value: boolean]
  save: [form: SaveApiRequestForm]
}>()

const { t } = useI18n()
const name = ref('')
const parentId = ref('')
const expandedFolderKeys = ref<Record<string, boolean>>({})
const folderPopoverOpen = ref(false)

interface FolderTreeSelectOption extends TreeItem {
  key: string
  value: string
  label: string
  displayLabel: string
  children?: FolderTreeSelectOption[]
}

function toTreeSelectOptions(
  options: ApiCollectionFolderTreeOption[],
  parentPath = '',
): FolderTreeSelectOption[] {
  return options.map((option) => ({
    label: parentPath ? `${parentPath}/${option.label}` : `/${option.label}`,
    key: option.key,
    value: option.value,
    displayLabel: option.label,
    children: toTreeSelectOptions(
      option.children ?? [],
      parentPath ? `${parentPath}/${option.label}` : `/${option.label}`,
    ),
  }))
}

const folderTreeOptions = computed<FolderTreeSelectOption[]>(() => {
  return toTreeSelectOptions(props.folderOptions)
})

const flatFolderOptions = computed<FolderTreeSelectOption[]>(() => {
  const out: FolderTreeSelectOption[] = []
  const visit = (options: FolderTreeSelectOption[]) => {
    for (const option of options) {
      out.push(option)
      if (option.children?.length) {
        visit(option.children)
      }
    }
  }
  visit(folderTreeOptions.value)
  return out
})

const selectedFolderOption = computed(() =>
  flatFolderOptions.value.find((option) => option.value === parentId.value),
)

const selectedFolderLabel = computed(() => selectedFolderOption.value?.label ?? '')

const expandedFolderKeyList = computed(() =>
  Object.keys(expandedFolderKeys.value).filter((key) => expandedFolderKeys.value[key]),
)

const canSubmit = computed(() => Boolean(name.value.trim() && parentId.value))

const visible = computed({
  get: () => props.show,
  set: (value: boolean) => emit('update:show', value),
})

function collectExpandedKeys(options: FolderTreeSelectOption[]) {
  const keys: Record<string, boolean> = {}
  const visit = (option: FolderTreeSelectOption) => {
    if (option.children?.length) {
      keys[option.key] = true
      option.children.forEach(visit)
    }
  }
  options.forEach(visit)
  return keys
}

watch(
  () => props.show,
  (show) => {
    if (!show) return
    name.value = props.defaultName.trim()
    parentId.value = props.defaultParentId
    expandedFolderKeys.value = collectExpandedKeys(folderTreeOptions.value)
  },
  { immediate: true },
)

function getFolderKey(option: FolderTreeSelectOption) {
  return option.key
}

function selectFolder(option: FolderTreeSelectOption) {
  parentId.value = option.value
  folderPopoverOpen.value = false
}

function close() {
  emit('update:show', false)
}

function submit() {
  const trimmedName = name.value.trim()
  if (!trimmedName || !parentId.value) {
    return
  }
  emit('save', {
    parentId: parentId.value,
    name: trimmedName,
  })
}
</script>

<template>
  <UModal
    v-model:open="visible"
    :title="t('api_collection.save_modal_title')"
    :dismissible="!saving"
    :close="{ disabled: saving }"
    :ui="{ content: 'max-w-[min(460px,calc(100vw-32px))]' }"
  >
    <template #body>
      <form class="flex flex-col gap-3" @submit.prevent="submit">
        <label class="flex min-w-0 flex-col gap-1.5">
          <span class="text-sm font-semibold text-app-text-secondary">{{
            t('api_collection.request_name')
          }}</span>
          <UInput
            v-model="name"
            :placeholder="t('api_collection.request_name_placeholder')"
            :disabled="saving"
            @keyup.enter="submit"
          />
        </label>
        <label class="flex min-w-0 flex-col gap-1.5">
          <span class="text-sm font-semibold text-app-text-secondary">{{
            t('api_collection.destination_folder')
          }}</span>
          <UPopover v-model:open="folderPopoverOpen" :content="{ align: 'start' }">
            <UButton
              class="w-full justify-between"
              color="neutral"
              variant="outline"
              :disabled="saving"
              trailing-icon="i-lucide-chevron-down"
              :label="selectedFolderLabel || t('api_collection.destination_folder')"
            />
            <template #content>
              <div class="max-h-64 min-w-55 overflow-auto p-1">
                <UTree
                  :items="folderTreeOptions"
                  :expanded="expandedFolderKeyList"
                  :model-value="selectedFolderOption"
                  :get-key="getFolderKey"
                  :ui="{
                    item: 'w-full',
                    link: 'gap-0 p-0 text-left',
                    linkLabel: 'min-w-0 flex-1',
                    listWithChildren: 'ms-3.5',
                  }"
                >
                  <template #item="{ item, expanded }">
                    <div
                      class="flex min-h-7 w-full min-w-0 items-center gap-1.5 rounded-md px-2 text-sm"
                      :data-key="(item as FolderTreeSelectOption).key"
                      @click.stop="selectFolder(item as FolderTreeSelectOption)"
                    >
                      <UIcon
                        :name="
                          expanded && (item as FolderTreeSelectOption).children?.length
                            ? 'i-lucide-folder-open'
                            : 'i-lucide-folder'
                        "
                        class="size-4 shrink-0 text-app-text-muted"
                        aria-hidden="true"
                      />
                      <span class="min-w-0 truncate leading-5">{{
                        (item as FolderTreeSelectOption).displayLabel
                      }}</span>
                    </div>
                  </template>
                </UTree>
              </div>
            </template>
          </UPopover>
        </label>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="outline" :disabled="saving" @click="close">
          {{ t('api_collection.cancel') }}
        </UButton>
        <UButton
          :disabled="!canSubmit"
          :loading="saving"
          :label="t('api_collection.save')"
          @click="submit"
        />
      </div>
    </template>
  </UModal>
</template>
