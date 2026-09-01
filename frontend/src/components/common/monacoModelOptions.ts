export interface MonacoBracketPairColorizationOptions {
  enabled: boolean
  independentColorPoolPerBracketType: boolean
}

interface MonacoBracketColorizationModel {
  getOptions(): {
    bracketPairColorizationOptions: MonacoBracketPairColorizationOptions
  }
  updateOptions(options: {
    bracketColorizationOptions?: MonacoBracketPairColorizationOptions
  }): void
}

export function syncMonacoModelBracketPairColorization(
  model: MonacoBracketColorizationModel,
  options: MonacoBracketPairColorizationOptions,
) {
  const current = model.getOptions().bracketPairColorizationOptions
  if (
    current.enabled === options.enabled &&
    current.independentColorPoolPerBracketType === options.independentColorPoolPerBracketType
  ) {
    return
  }

  // Monaco renders bracket colors from model decorations. Updating only the
  // editor's bracketPairColorization option does not update this model state.
  model.updateOptions({ bracketColorizationOptions: options })
}
