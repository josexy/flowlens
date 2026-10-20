import en from '../locales/python-plugin-help/en.json' with { type: 'json' }
import zh from '../locales/python-plugin-help/zh.json' with { type: 'json' }
import {
  API_CONSTRUCTORS,
  API_MEMBERS,
  PYTHON_PLUGIN_HOOKS,
  PYTHON_PLUGIN_MINIMAL_SCRIPT,
  type ApiMember,
  type ApiTypeName,
  type PythonHelpScope,
} from './pythonPluginApiCatalog.js'

export interface PythonApiEntry {
  name: string
  signature: string
  description: string
  kind: 'property' | 'method' | 'constructor' | 'hook'
  writable?: boolean
  parameters?: readonly string[]
  returnType?: string
}

export interface PythonApiGroup {
  name: string
  importable: boolean
  note: string
  entries: PythonApiEntry[]
}

export function getPythonPluginHelpMessages(locale: string) {
  return locale.toLowerCase().startsWith('zh') ? zh : en
}

function memberEntry(owner: ApiTypeName, member: ApiMember, locale: string): PythonApiEntry {
  const descriptions: Record<string, string> = getPythonPluginHelpMessages(locale).members[owner]
  return {
    name: member.name,
    signature: `${owner === 'FileDescriptorClass' ? 'FileDescriptor' : owner}.${member.signature}`,
    description: descriptions[member.name] ?? member.documentation,
    kind: member.kind,
    writable: member.writable,
    parameters: member.parameters,
    returnType: member.returnType,
  }
}

export function getPythonPluginApiGroups(locale: string, query = ''): PythonApiGroup[] {
  const messages = getPythonPluginHelpMessages(locale)
  const notes: Partial<Record<ApiTypeName | 'Hooks', string>> = messages.reference_notes
  const groups: PythonApiGroup[] = [
    {
      name: 'Hooks',
      importable: false,
      note: notes.Hooks ?? '',
      entries: PYTHON_PLUGIN_HOOKS.map((hook) => ({
        name: hook.name,
        signature: `def ${hook.signature}`,
        description: messages.hooks[hook.name],
        kind: 'hook',
        parameters: hook.parameters,
        returnType: hook.returnType,
      })),
    },
  ]
  for (const owner of Object.keys(API_MEMBERS) as ApiTypeName[]) {
    if (owner === 'FileDescriptorClass') continue
    const constructor = API_CONSTRUCTORS.find((item) => item.name === owner)
    const constructorDescriptions: Record<string, string> = messages.constructors
    const entries: PythonApiEntry[] = constructor
      ? [
          {
            name: owner,
            signature: constructor.signature,
            description: constructorDescriptions[owner] ?? constructor.documentation,
            kind: 'constructor',
            parameters: constructor.parameters,
            returnType: owner,
          },
        ]
      : []
    entries.push(...API_MEMBERS[owner].map((member) => memberEntry(owner, member, locale)))
    if (owner === 'FileDescriptor') {
      entries.push(
        ...API_MEMBERS.FileDescriptorClass.map((member) =>
          memberEntry('FileDescriptorClass', member, locale),
        ),
      )
    }
    groups.push({
      name: owner,
      importable: Boolean(constructor) || owner === 'Context',
      note: notes[owner] ?? '',
      entries,
    })
  }
  const search = query.trim().toLowerCase()
  if (!search) return groups
  const summaries: Record<string, string> = messages.group_summaries
  return groups.flatMap((group) => {
    if (`${group.name}\n${summaries[group.name] ?? ''}`.toLowerCase().includes(search))
      return [group]
    const entries = group.entries.filter((entry) =>
      `${entry.signature}\n${entry.description}`.toLowerCase().includes(search),
    )
    return entries.length ? [{ ...group, entries }] : []
  })
}

export function buildPythonPluginAIContext({
  scope,
  locale,
}: {
  scope: PythonHelpScope
  locale: string
}): string {
  const messages = getPythonPluginHelpMessages(locale)
  const groups = getPythonPluginApiGroups(locale)
  const api = groups
    .map((group) =>
      [
        `### ${group.name} (${group.name === 'Hooks' ? messages.hook : group.importable ? messages.importable : messages.provided})`,
        ...group.entries.map((entry) =>
          [
            `- \`${entry.signature}\``,
            entry.kind === 'property'
              ? `(${entry.writable ? messages.writable : messages.readonly})`
              : '',
            `— ${entry.description}`,
          ]
            .filter(Boolean)
            .join(' '),
        ),
      ].join('\n'),
    )
    .join('\n\n')
  return [
    '# FlowLens Python SDK API v1',
    messages.prompt_intro,
    `## ${messages[scope]}`,
    scope === 'global' ? messages.scope_global : messages.scope_inline,
    `## ${messages.prompt_rules}`,
    ...Object.values(messages.rules).map((rule) => `### ${rule.title}\n${rule.text}`),
    messages.provided_hint,
    `## ${messages.prompt_api}`,
    api,
    `## ${messages.prompt_template}`,
    `\`\`\`python\n${PYTHON_PLUGIN_MINIMAL_SCRIPT}\`\`\``,
    `## ${messages.prompt_output}`,
    messages.prompt_output_text,
    `## ${messages.prompt_requirement}`,
    messages.prompt_placeholder,
  ].join('\n\n')
}
