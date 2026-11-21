# Spec Delta

## ADDED Requirements

### Requirement: ICO image preview
The shared body viewer SHALL preview ICO image responses using a stored image representation, including PNG and DIB representations and their transparency, without external programs or cgo. Hex and saving SHALL continue to use the exact original ICO bytes.

#### Scenario: ICO response
- **WHEN** a valid ICO body is viewed with `image/x-icon` or `image/vnd.microsoft.icon` as its content type
- **THEN** Image shows the decoded icon, its dimensions, and the original body size; binary Raw and Fmt remain hidden

#### Scenario: Multiple stored sizes
- **WHEN** an ICO body contains multiple image representations
- **THEN** Image previews one stored representation and reports that representation's dimensions

#### Scenario: Transparent icon
- **WHEN** an ICO contains PNG alpha, DIB alpha, or a DIB transparency mask
- **THEN** the preview retains the supported representation's transparent pixels in both light and dark themes

#### Scenario: Invalid ICO
- **WHEN** an ICO has an empty or truncated directory, an invalid image range, an undecodable payload, or selected image dimensions outside 1 through 256 pixels per side
- **THEN** Image shows the existing decoding-failure notice, while Hex and saving preserve the original nonempty body

#### Scenario: Replacing or closing an icon preview
- **WHEN** the selected body changes or its view closes while ICO decoding runs
- **THEN** the existing job cancellation and stale-result checks prevent the old icon from replacing the new view
