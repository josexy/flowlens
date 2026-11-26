# Proposal

## Why

At baseline `20e2d64`, traffic rows allocate extra decorative elements and small window icons retain full-size decoded artwork. Reduce these application-owned costs while preserving interaction and history-release behavior.

## What Changes

- Paint row backgrounds, outlines, and markers on existing Nuxt cells, including the fill cell.
- Use the first displayed cell's committed bounds for right-click hit testing.
- Share one application bitmap bounded to 256 pixels per side between title bars and Software Update.
- Add retained-heap benchmarks, lifecycle regressions, and native acceptance checks.

## Scope and completion

The change stays in FlowLens with the current MyGo/Nuxt versions. It preserves headers, selection, shortcuts, accessibility, capture policy, history retention, and saved layouts. Dependency changes, upstream patches, and automatic forced GC are outside scope.

| Criterion | Verification |
| --- | --- |
| Fewer row elements | Remove `C + 1` ordinary or `2C + 2` outlined-row nodes, counting fill in `C`. |
| Lower retained UI memory | Target 20% in matched warmed fixtures; record actual results and stable or improved frame time/allocations. |
| Shared small icon | Retained CPU pixels including mipmaps below 0.5 MiB; opening Update creates no second full-size bitmap. |
| Stable history closure | Ten matched cycles; post-collection object-memory range over cycles 6-10 at most 5 MiB. |
| Preserved behavior | Passing regressions and Windows native checks across themes, languages, and representative scales; separate cross-platform compilation from runtime acceptance. |

Current results and remaining checks belong in [tasks.md](tasks.md).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This internal refactor declares `skip_specs: true`. Existing `traffic-capture`, `traffic-history`, `traffic-inspection`, `appearance`, and `desktop-shell` requirements still apply.

## Impact

- **Capture:** cell decoration, context-menu bounds, and existing benchmark/render/release tests.
- **Widgets and Update:** shared icon loading for main, settings, and update windows.
- **Test support:** clear delivered runtime-test callbacks and use an undeletable open-file fixture on Windows.
- **Data and services:** no persistence, protocol, Python Worker, process-attribution, event, shortcut-binding, or shutdown changes.
- **Documentation:** maintain [memory diagnostics](../../../../docs/technical/memory-diagnostics.md); no new interface strings.

The existing header spec says no vertical dividers, while current code/tests preserve them. This change leaves that discrepancy untouched.
