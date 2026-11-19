# Proposal

## Why

The Memory Statistics section shows only Go runtime counters, and none of them is the number users compare it with: the memory figure for FlowLens in Task Manager, Activity Monitor, or `top`. Its headline "Total Sys" (shown in Chinese as "系统内存", "system memory") is the address space the Go runtime has mapped, including memory it has already returned to the OS. That figure usually exceeds what the OS reports, while "Heap Alloc" is far below it, so neither matches what users see elsewhere. The page also shows about 25 expert values with duplicates ("Live objects" twice, "Heap Alloc"/"Heap Allocated"), lifetime counters (mallocs, frees, cumulative allocation, total pause), a 4-decimal "GC CPU fraction", and Go version/OS/CPU details that the status bar's Environment dialog already shows. Every figure stays as "-" until the user presses Start.

## What Changes

- **Lead with the memory figure the OS shows.** The first summary card, "Memory", shows FlowLens's own process memory as the platform's standard tool reports it: the private working set (Task Manager's Memory column) on Windows, the physical footprint (Activity Monitor's Memory column) on macOS, and resident memory (`RES` in `top`/`htop`) on Linux. A caption names the matching tool. When the platform value cannot be read, the card shows the unknown mark and says so; it never substitutes a Go-runtime number.
- **One secondary figure.** The second card shows "Heap in use": the memory held by live Go objects, such as captured traffic, caches, and interface state. The Total Sys, GC Count, and Goroutines cards are removed.
- **Chart what users compare.** The chart plots Memory and Heap in use, with localized series names. The Sys and StackInuse series are removed. Samples where process memory is unavailable appear as gaps, never as zero. The chart keeps up to 600 samples instead of 60, so the default 2-second interval covers 20 minutes.
- **Trim Go internals into a collapsed section.** "Go runtime details" starts collapsed and holds 10 values. The memory group shows Held by Go runtime and its four parts, which add up to it exactly (heap in use, kept for reuse, stacks, and runtime overhead), plus returned to the OS. The other rows show the next GC target, GC cycles, the last GC time and pause, and goroutines.
- **BREAKING (displayed metrics): remove** mallocs, frees, both live-object counts, heap object count, cumulative allocation, forced GC count, GC CPU fraction, total GC pause, Sys, HeapSys, HeapInuse, HeapIdle, StackSys, Go version, OS/architecture, and CPU cores. Go version and platform remain in the Environment dialog. The "Go Backend Runtime" badge is removed because the page no longer shows only Go runtime values.
- **Show figures on first view.** The first time the section is shown in a run with no samples, it takes one sample. Start/Pause stays explicit and unchanged, and Clear still empties everything.
- **Sample without stopping the world.** Go values come from `runtime/metrics` and `runtime/debug.ReadGCStats` instead of `runtime.ReadMemStats`. The in-process sample model shrinks to the values that are shown.
- **Docs.** The user guide explains what Memory matches on each platform and why it can stay high after data is cleared.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `memory-statistics`: "Runtime metrics" is replaced by "Process memory" and "Go runtime details". "Monitoring control" adds the first-view sample. "Memory usage chart" defines its series, gaps, and retention. "Layout changes preserve monitoring state" adds the expansion state of the details.

## Impact

- **Code:**
  - `internal/services/mem_stats_service`: a new snapshot model, `runtime/metrics` sampling, and platform process-memory readers (`process_memory_{windows,darwin,linux,other}.go`, plus a darwin no-cgo fallback that reports unavailable).
  - `internal/desktop/store/memstats.go`: sample limit and first-view sampling.
  - `internal/desktop/memstats/view.go`: summary, chart, collapsed details, and copy. Its layout and view tests change too.
  - `internal/app` only if the wiring changes (not expected).
- **Locales:** `locales/{en,zh-Hans}.json`. Removed keys go away, new keys are added, and `task i18n:generate` regenerates the accessors.
- **Dependencies:** none new. Windows uses `kernel32`'s `K32GetProcessMemoryInfo` through `golang.org/x/sys/windows`. macOS uses `task_info(TASK_VM_INFO)` through cgo, which macOS builds already enable.
- **Compatibility:** the snapshot exists only on the in-process event bus and is never persisted, so no stored data or settings migrate.
- **Docs:** `docs/user-guide.md`.
- **Tests:** `./internal/services/mem_stats_service ./internal/desktop/memstats ./internal/desktop/store`, `task vet:ui`, and render checks in light and dark themes in both languages.
