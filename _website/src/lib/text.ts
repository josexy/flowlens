/** Replaces `{name}` placeholders; unknown placeholders are left as written. */
export function format(template: string, values: Record<string, string>): string {
  return template.replace(/\{(\w+)\}/g, (match, name: string) => values[name] ?? match);
}
