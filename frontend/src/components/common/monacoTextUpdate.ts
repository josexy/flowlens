export function getReadonlyMonacoAppendText(
  previousValue: string,
  nextValue: string,
  modelValue: string,
): string | null {
  if (
    !nextValue.startsWith(previousValue) ||
    normalizeLineEndings(modelValue) !== normalizeLineEndings(previousValue)
  ) {
    return null
  }
  return nextValue.slice(previousValue.length)
}

function normalizeLineEndings(value: string): string {
  return value.replace(/\r\n|\r|\n/g, '\n')
}
