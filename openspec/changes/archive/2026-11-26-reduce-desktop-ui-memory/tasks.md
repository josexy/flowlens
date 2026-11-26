# Tasks

Use the current official dependencies and `CGO_ENABLED=0` for ordinary Go checks. Do not modify upstream source, module caches, replacements, or dependency versions.

## 1. Record the baseline

- [x] 1.1 Build with `go tool mygo build -o .mygo/run -skip-installer -skip-dmg`; record HEAD/worktree status and `go version -m .mygo/run/windows-amd64/FlowLens.exe`, verifying the measured binary matches its source.
- [x] 1.2 Extend the table benchmark with warmed retained-heap measurement, marked rows, and column layouts; verify fixture row counts and separate memory sampling from timed setup.
- [x] 1.3 Run `go test ./internal/desktop/capture -run '^$' -bench '^BenchmarkTrafficTable' -benchmem -count=5`; collect Windows baseline profiles with data, DPI, theme, columns, capture, and UIA state recorded.

## 2. Share the scaled icon

- [x] 2.1 Add the widgets-owned `ui.DecodeBitmapScaled(resources.AppIcon, 256)` helper; verify title bars and Update share its bitmap and tolerate decode failure.
- [x] 2.2 Run `go test ./internal/desktop/widgets ./internal/desktop/update -count=1`; verify repeated consumers reuse the bitmap and retained CPU pixels including mipmaps stay below 0.5 MiB.
- [ ] 2.3 Inspect the main, settings, and update window icons in light/dark themes and both UI languages at 100%, 150%, 200%, and 400% scaling where supported; record clear icons, correct sizes, and the absence of another full-size decode when opening Update.
- [x] 2.4 Document loader ownership and the size bound in memory diagnostics, with before/after pixel measurements below; verify the description matches the final code.

## 3. Remove redundant table elements

- [x] 3.1 Pair Nuxt's synchronous Style/Cell callbacks through a build-local cell; verify repeated builds, columns, and history replacement never reuse stale elements.
- [x] 3.2 Use the first displayed cell's bounds and decorate fill cells; verify scrolled right-clicks, reordered/hidden columns, Path resizing, grouped selection, and empty tables.
- [x] 3.3 Remove background/outline/marker nodes; run `go test ./internal/desktop/capture ./internal/desktop/widgets -count=1` and verify row pixels, badges, headers, and full-width borders.
- [x] 3.4 Run `task vet:ui` and repeat the five-run benchmark from 1.3; verify scalar-only drawing callbacks and report retained memory against the 20% target, allocations, and frame time.
- [x] 3.5 Run the render command below; inspect table and icon/update views in both themes and languages.
- [x] 3.6 Document final node savings and benchmark/profile comparisons; verify ordinary/marked and fill/no-fill cases match the code.

```powershell
$env:CGO_ENABLED = '0'
$env:FLOWLENS_RENDER_DIR = Join-Path $env:TEMP 'flowlens-memory-renders'
go test ./internal/desktop/capture ./internal/desktop/widgets ./internal/desktop/update -run 'TestRender|TestTrafficRows|TestInspectedRow|TestClickedRowOutline' -count=1
```

## 4. Verify closure and integration

- [x] 4.1 Run `go test ./internal/desktop/capture ./internal/desktop/inspect ./internal/desktop/store ./internal/desktop/editor -count=1`; verify repeated history/body/close cycles release old payloads and callbacks while preserving preferences.
- [x] 4.2 Collect ten matched Windows history cycles; verify the object-memory range over cycles 6-10 is at most 5 MiB and record Process, objects, Held, and heap profiles.
- [x] 4.3 In a separate run after the same workload, sample Process, objects, and Held at 0/30/120/300 seconds without forced GC; document recovery and remaining allocation sources.
- [x] 4.4 Verify Windows selection, dragging, scrolling, menus, columns, high DPI, and UIA/keyboard behavior in both themes and languages; record macOS/Linux compilation and pending native checks separately.
- [x] 4.5 Run `task verify` and `openspec validate reduce-desktop-ui-memory --strict --no-interactive`; verify `git diff -- go.mod go.sum` is empty and compare implementation/results with the proposal, design, and existing specs.

## Acceptance status

**Archived on 2026-11-26 with user approval: 17/18 tasks complete.** Task 2.3 remains unverified. Native Windows coverage is at 125%; headless renders cover 100/150/200/400%.

Measurements from 2026-11-26:

| Check | Result |
| --- | --- |
| Shared icon CPU pixels | 5.332 → 0.332 MiB including mipmaps; below 0.5 MiB. |
| Retained table UI heap, 5,000 rows | Default: 7.503 → 6.075 MiB (19.0%). Marked: 9.134 → 6.611 MiB (27.6%). |
| Column-layout benchmarks | Reductions 19.0–29.6%; lower median frame time and allocations. Ordinary default/reordered cases missed the 20% target at 19.0%/19.8%. |
| Ten native history cycles | Object-memory range over cycles 6-10: 1.2 MiB; within the 5 MiB limit. |
| Five-minute natural idle | Process 117.76 → 74.31 MiB; objects 53.51 → 19.54 MiB; Held 87.91 → 45.49 MiB. No collection requests. |

Native memory checks used the same 3,594-row history and 813-byte Raw response, maximized window, Chinese/light, default columns, stopped proxy, and active UIA. Objects were near 19.5 MiB at 120 seconds; Process/Held continued falling. Remaining sampled allocations were mainly MyGo element/frame/drawing resources and runtime storage.

`task verify` and strict OpenSpec validation passed. The Windows cleanup failure fixture passed ten runs using an open file handle. Shared-UI tests cross-compiled for linux/amd64 and darwin/amd64/arm64; native runtime checks on those hosts remain pending.

The detailed reports and raw artifacts were removed by the user after verification. Preserve that deletion; completed checkboxes describe checks already performed.

## Workflow follow-up

- Task 2.3 remains a follow-up acceptance check. With `skip_specs: true`, no spec delta was synchronized.
