export interface FileFilter {
  DisplayName?: string
  Pattern?: string
}

const allFilePatterns = new Set(['*', '*.*'])

export function getErrorMessage(error: unknown): string {
  if (error instanceof Error && error.message) {
    return error.message
  }
  return String(error)
}

export function isDialogCancelError(error: unknown): boolean {
  const message = getErrorMessage(error).toLowerCase()
  return message.includes('cancelled by user') || message.includes('canceled by user')
}

export function withAllFilesFilter(
  filters: FileFilter[] | undefined,
  displayName: string,
): FileFilter[] | undefined {
  if (!filters?.length) {
    return filters
  }

  const hasAllFilesFilter = filters.some((filter) =>
    (filter.Pattern ?? '')
      .split(';')
      .map((pattern) => pattern.trim())
      .some((pattern) => allFilePatterns.has(pattern)),
  )
  if (hasAllFilesFilter) {
    return filters
  }

  return [
    ...filters,
    {
      DisplayName: displayName,
      Pattern: '*',
    },
  ]
}
