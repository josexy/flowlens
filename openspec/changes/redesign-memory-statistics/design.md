# Design

## Context

See proposal.md (Why) for the problems with the current page. The current state that constrains the approach:

- **Service.** `internal/services/mem_stats_service` builds a `MemSnapshot` (31 fields) from `runtime.ReadMemStats`, which stops the world, and emits it on `memstats:update` through the in-process `serviceruntime.EventBus`. The snapshot is never persisted or serialized for storage. `GetMemStats` takes one sample, and `StartMonitoring`/`StopMonitoring` control a ticker goroutine that stops on shutdown.
- **Store.** `internal/desktop/store/memstats.go` keeps up to `MaxMemStatsSamples = 60` samples and the latest one. It also holds the monitoring flag and interval. `Start`, `Stop`, `SetInterval`, `FetchOnce`, and `Clear` call the service through `runtime.Run`.
- **View.** `internal/desktop/memstats/view.go` renders the toolbar, 4 summary cards, a 3-series `nui.Chart` (`heap`, `sys`, and `stack`, with untranslated labels), and two detail cards with 23 rows. The shell calls `Content` only while the section is the active surface (`internal/desktop/shell/app.go`).
- **Other displays.** The status bar's Environment dialog already shows the Go version and platform (`internal/desktop/shell/statusbar.go`).
- **Platforms.** macOS builds keep `CGO_ENABLED=1`, and a `darwin && !cgo` build exists for tests and cross-checks (`process_attribution` reports unsupported there). Windows code calls `kernel32` through `windows.NewLazySystemDLL`. `nui` provides `Collapsible`, and `nui.Chart` treats NaN Y values as gaps.
- **Formatting.** `logic.FormatFileSizeWith` uses 1024-based `KB`/`MB`/`GB`, the same convention as Task Manager.

## Goals / Non-Goals

**Goals:**
- The headline number matches the platform's own tool for the FlowLens process, so users can compare the two directly.
- Every value shown has a plain-language label, appears once, and, when it belongs to a breakdown, adds up with the others.
- Sampling neither stops the world nor allocates in proportion to the process's size.

**Non-Goals:**
- Python Worker or other child-process memory (user decision). Workers stay out of the figure and the chart.
- Peak memory. The platforms disagree on what peak means: Windows peak working set includes shared pages, while macOS and Linux track lifetime footprint or RSS maxima. The chart already shows the maximum in its window.
- A derived "native/other" figure computed as Memory minus Go-held memory (see Decision 4).
- Starting monitoring automatically when the section becomes visible, or a "release memory"/force-GC action.
- Persisting the details' expansion state or the interval to settings.

## Decisions

### 1. What is exposed, collapsed, and removed

| Value | Placement | Reason |
| --- | --- | --- |
| Memory (process, OS measure) | Summary card 1, chart | The number users already know from OS tools |
| Heap in use | Summary card 2, chart, details | Memory held by FlowLens's own data. It responds to captures and clears |
| Held by Go runtime | Details (group total) | Explains how much of Memory Go accounts for |
| Kept for reuse, Stacks, Runtime overhead | Details (parts of the total) | Explains why Memory stays high after data is cleared |
| Returned to the OS | Details (outside the total) | Shows that memory is being given back |
| Next GC target, GC cycles, Last GC + pause, Goroutines | Details | Diagnostics for growth, GC activity, and goroutine leaks |
| Sys, HeapSys, HeapInuse, HeapIdle, StackSys | Removed | Mapped address-space views that duplicate the breakdown above, and Sys includes released memory |
| Mallocs, Frees, live objects ×2, heap object count, cumulative alloc | Removed | Lifetime counters and object counts with no action a user can take |
| Forced GC count, GC CPU fraction, total GC pause | Removed | Lifetime averages that hide current behavior and are hard to read |
| Go version, OS/arch, CPU cores | Removed | Not memory. Version and platform are in the Environment dialog |

The details group starts collapsed. The summary and chart answer "how much memory does FlowLens use, and is it growing?", and the details answer "where does the Go part go?".

### 2. Platform process measure

`ProcessMemory{Bytes int64, Measure ProcessMeasure}` holds `Bytes = -1` when the value is unavailable. That follows the project's convention for unknown values. `Measure` names what was read, and the view picks its caption from `Measure` rather than from `runtime.GOOS`.

| Platform | Measure | Source | Matches |
| --- | --- | --- | --- |
| Windows | `private_working_set` | `K32GetProcessMemoryInfo(GetCurrentProcess(), PROCESS_MEMORY_COUNTERS_EX2)`, field `PrivateWorkingSetSize`. If the call fails or the field is 0 (an OS without EX2), fall back to `K32QueryWorkingSet` and count the entries without the Shared bit, multiplied by the page size | Task Manager's Memory column ("Memory (active private working set)") |
| macOS (cgo) | `phys_footprint` | `task_info(mach_task_self(), TASK_VM_INFO, …)`, field `phys_footprint` | Activity Monitor's Memory column |
| macOS (no cgo), other OS | unavailable | — | — |
| Linux | `resident` | `/proc/self/statm`, field 2 × `os.Getpagesize()` | `RES` in `top`/`htop`, `RSS` in `ps` |

The readers live in `mem_stats_service` as `process_memory_windows.go`, `process_memory_darwin.go` (`darwin && cgo`), `process_memory_darwin_nocgo.go`, `process_memory_linux.go`, and `process_memory_other.go`, all behind one `readProcessMemory() ProcessMemory`. They only read FlowLens's own process, so no shared `internal/pkg` package and no change to AGENTS.md's module table are needed.

Alternatives considered:
- **Go `Sys − HeapReleased` as a cross-platform stand-in.** It is not what any OS tool shows and misses native memory (renderer, fonts, system libraries). Rejected, because a familiar-looking but different number is the confusion this change removes.
- **Windows working set or private bytes (commit).** The working set includes shared DLL pages, and commit counts non-resident memory. Neither matches Task Manager's default column. Rejected.
- **`NtQuerySystemInformation(SystemProcessInformation)`.** It enumerates every process and relies on fields the public headers do not document. Rejected in favor of documented APIs.
- **Linux PSS/USS from `smaps_rollup`.** These are closer to "private" but expensive on large processes and not shown by `top`/`htop`. Rejected for this release.

### 3. Go values from `runtime/metrics`

`runtime/metrics.Read` does not stop the world. All `/memory/classes/...` values come from one aggregate and add up to `/memory/classes/total:bytes`. The snapshot's `RuntimeMemory` holds:

- `HeapInUse` = `heap/objects`
- `KeptForReuse` = `heap/free` + `heap/unused`
- `Stacks` = `heap/stacks` + `os-stacks`
- `Released` = `heap/released`
- `Held` = `total` − `Released`
- `Overhead` = `Held` − `HeapInUse` − `KeptForReuse` − `Stacks`, so metadata, profiling, `other`, and any class a future Go release adds land here and the breakdown always adds up
- `NextGC` = `/gc/heap/goal:bytes`, `GCCycles` = `/gc/cycles/total:gc-cycles`, `Goroutines` = `/sched/goroutines:goroutines`
- `LastGC` (Unix ms, 0 before the first GC) and `LastPauseNs` from `debug.ReadGCStats`, which takes the heap lock without stopping the world. The service reuses one `GCStats` value under a mutex, so its pause slice is not reallocated on every sample.

A metric whose `Kind` is `KindBad` (unsupported) reads as 0 and is logged once. Tests pin the metric names against the toolchain in `go.mod`.

### 4. No derived "native/other" figure

Memory (resident or private-resident) and Held by Go runtime (mapped minus released) measure different things. Part of Go's held memory may not be resident, and Windows' private working set excludes paged-out pages, so `Memory − Held` can be negative or wrong. The details show a one-line note instead: Memory also counts the interface renderer, fonts, and system libraries, which Go does not track.

### 5. Snapshot model and store

```go
type MemSnapshot struct {
    Timestamp int64         // Unix ms
    Process   ProcessMemory // Bytes -1 when unavailable
    Runtime   RuntimeMemory
}
```

The old flat fields are removed. The event name, `GetMemStats`, `GetMonitoringStatus`, `StartMonitoring`, `StopMonitoring`, and shutdown behavior are unchanged.

`MaxMemStatsSamples` becomes 600: 20 minutes at the default 2 s and 5 minutes at 500 ms, at roughly 100 bytes per sample. The view keeps `viewed bool`. On its first `Content` call, if `Samples` is empty and monitoring is off, it calls `Data.FetchOnce()` once. It never fetches again because of viewing, so Clear leaves the section empty as the spec requires.

### 6. View structure

- **Toolbar:** unchanged controls. The "Go Backend Runtime" badge is removed.
- **Summary:** two cards (`Grow(1).Basis(240)`). Each shows a label, a value, and a muted wrapping caption. Memory's value uses `theme.Primary`, and Heap in use's uses `theme.Text`.
- **Chart:** the series are `process` (Memory, `nui.Primary`) and `heap` (Heap in use, `nui.Neutral`), both with localized labels. `nui.Info` is blue, the same as the default primary color, while Neutral is never a primary choice. Keeping the `heap` ID keeps existing visibility state and tests working. An unknown `Process.Bytes` becomes `math.NaN()` (a gap). If no retained sample has a process value, the `process` series is still listed, so the legend does not jump between samples.
- **Details:** `nui.Collapsible` bound to `v.detailsOpen` (default `false`) with the title "Go runtime details". The content starts with the note from Decision 4, followed by two `metrics` tables in a wrapping row:
  - **Go memory:** Held by Go runtime (the total row, emphasized), followed by Heap in use, Kept for reuse, Stacks, and Runtime overhead as indented part rows, then Returned to the OS.
  - **Garbage collection:** Next GC target, GC cycles, Last GC (`<local time> · <pause>`, or the no-GC-yet copy), and Goroutines.
- **Formatting:** bytes use `logic.FormatFileSizeWith` with precision 1 everywhere on the page. Times use `logic.FormatTimeLocal`. Pauses reuse a `logic` duration helper, adding one for nanosecond input only if none fits. The local `formatPercent`, `groupDigits`, and `liveObjects` helpers are deleted.

### 7. Copy

| Key purpose | en | zh-Hans |
| --- | --- | --- |
| Memory | Memory | 内存 |
| Caption, Windows | FlowLens process, as in Task Manager's Memory column | FlowLens 进程，与任务管理器“内存”列一致 |
| Caption, macOS | As in Activity Monitor's Memory column | 与活动监视器“内存”列一致 |
| Caption, Linux | Resident memory, as RES in top | 常驻内存，与 top 的 RES 列一致 |
| Caption, unavailable | Not available on this system | 此系统无法读取 |
| Heap in use | Heap in use | 已用堆内存 |
| Caption, heap | Go objects such as captured traffic and caches | 抓包数据、缓存等 Go 对象 |
| Details title | Go runtime details | Go 运行时详情 |
| Details note | Memory also counts the interface renderer, fonts, and system libraries, which Go does not track. | “内存”还包括界面渲染、字体和系统库，这些不由 Go 统计。 |
| Held by Go runtime | Held by Go runtime | Go 运行时占用 |
| Kept for reuse | Kept for reuse | 保留待复用 |
| Stacks | Stacks | 栈 |
| Runtime overhead | Runtime overhead | 运行时开销 |
| Returned to the OS | Returned to the OS | 已归还系统 |
| Next GC target | Next GC target | 下次 GC 阈值 |
| GC cycles | GC cycles | GC 次数 |
| Last GC value | {time} · {pause} pause | {time} · 暂停 {pause} |

Existing keys are reused where the wording still fits (title, toolbar, no data, no GC yet, goroutines, and the section heading for garbage collection). Keys that become unused are deleted from both catalogs.

## Risks / Trade-offs

- [`PROCESS_MEMORY_COUNTERS_EX2` is missing on older Windows 10 builds] → The `QueryWorkingSet` fallback measures the same quantity. Its cost grows with the working set (about 8 bytes per resident page, or about 1 MiB of buffer at 512 MiB), which is acceptable at a 500 ms minimum interval. The fallback is used only when EX2 fails.
- [Task Manager groups child processes under the app row, so the group total includes Python Workers] → The Windows caption says "FlowLens process", and the user guide says to compare with the FlowLens process row, not the group total.
- [Heap in use rises and falls with GC cycles (a sawtooth)] → This is expected. Its caption describes contents rather than claiming it is "live data", and the chart keeps 600 samples, so the trend is visible.
- [Memory does not drop right after a capture is cleared] → The details show Kept for reuse and Returned to the OS, and the user guide explains that the Go runtime returns freed memory gradually.
- [A future Go release renames or removes a metric] → `KindBad` reads as 0 and is logged once. Overhead is a remainder, so the breakdown still adds up, and a test fails when a name stops being supported.
- [The first-view sample changes the empty state that existing render and layout tests expect] → Fixtures provide `GetMemStats` and the tests are updated. The spec's no-data scenario still holds after Clear or when the first sample fails.

## Migration Plan

None is needed. The snapshot is only on the in-process bus and is not persisted, and no settings change. To roll back, revert the change.

## Open Questions

- Which Windows build first accepts `PROCESS_MEMORY_COUNTERS_EX2`? This only decides how often the fallback runs and does not change the approach. Record the answer in the reader's comment during implementation.
