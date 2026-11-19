# Memory Diagnostics

How FlowLens's memory behaves after heavy use, what it keeps on purpose, and how to profile a running instance.

## Profiling a Running Instance

Profiling is off unless FlowLens starts with `FLOWLENS_PPROF_ADDR` set to a loopback IP and port. Any other address is refused and logged, and the server stops when FlowLens exits.

```sh
# macOS
FLOWLENS_PPROF_ADDR=127.0.0.1:6060 /Applications/FlowLens.app/Contents/MacOS/FlowLens
# Linux
FLOWLENS_PPROF_ADDR=127.0.0.1:6060 flowlens
# Windows (PowerShell)
$env:FLOWLENS_PPROF_ADDR = "127.0.0.1:6060"; & "$env:LOCALAPPDATA\Programs\FlowLens\FlowLens.exe"
```

| Endpoint | Use |
| --- | --- |
| `/debug/pprof/` | Index of the Go runtime profiles: `heap`, `allocs`, `goroutine`, `goroutineleak`, `threadcreate`, `profile` (CPU), and `trace` |
| `POST /debug/flowlens/collect` | Runs two collections with cleanups between them, returns free memory to the OS, and reports process memory, heap in use, kept for reuse, and held memory at each step |

```sh
curl -X POST http://127.0.0.1:6060/debug/flowlens/collect
go tool pprof -sample_index=inuse_space http://127.0.0.1:6060/debug/pprof/heap?gc=1
curl -o before.pprof 'http://127.0.0.1:6060/debug/pprof/heap?gc=1'   # later, again as after.pprof
go tool pprof -sample_index=inuse_space -top -base before.pprof after.pprof
curl 'http://127.0.0.1:6060/debug/pprof/goroutineleak?debug=1'
```

Read the collect report from its last line:

- Process memory falls to about the held memory: the memory was garbage or free heap waiting for the runtime, not a leak.
- Heap in use stays high: live objects keep it. Diff heap profiles taken before and after an action to find what holds them.
- Kept for reuse stays high while heap in use is low: the heap is fragmented. Live objects are spread over many partly used pages that cannot go back to the OS.
- Process memory stays well above held memory: the rest is outside the Go heap, such as GPU textures, fonts, and native libraries. Go profiles cannot show it; use the platform's tools.

## Why Memory Stays High While Idle

The Go runtime starts a collection when allocation reaches the heap goal, which is twice the heap left live by the previous collection. An idle FlowLens allocates almost nothing, so after a burst of capture nothing collects the garbage until the runtime's forced collection, which runs every two minutes. Process memory can therefore stay at its peak for up to two minutes.

Pooled objects outlive their last use by one more collection: `sync.Pool` keeps an idle object through the first collection and drops it at the second. The largest are the zstd encoders that write spill segments, saved histories, and zstd request bodies. An encoder that has written a body over 128 KiB keeps a history buffer twice its 1 MiB window, about 5 MiB in all, and a capture burst can leave several in the pool. mitmproxy-go also keeps each proxied response's request context, and with it the connection's TLS state, in a table keyed by a weak pointer until a cleanup runs after the response is collected. After a burst, an idle FlowLens frees this memory about four minutes after the traffic stops.

After each collection the runtime returns free pages to the OS in the background and keeps about 10% over what the heap needs. Pages that still hold a live object stay in use. Entries kept from a burst are spread over many such pages, so "kept for reuse" can stay larger than the entries themselves until they are evicted or cleared.

## What FlowLens Keeps on Purpose

| Kept | Bound | Released when |
| --- | --- | --- |
| Live capture | The newest 5000 settled entries plus in-progress exchanges; bodies above the cache threshold (10 KiB by default) are on disk | Older entries spill to disk; Clear or restarting capture releases the rest |
| Opened history | Every entry of that history, headers without bodies | Its tab closes or another history opens |
| Selected entry | Its bodies and the viewers' formatted, Raw, Hex, and image state | Selection changes, details close, or the capture is cleared |
| History indexes | The four most recently used histories | Least recently used first |
| Pooled zstd encoders | About 5 MiB each after a body over 128 KiB, as many as wrote at once | The second collection after their last use |
| MyGo text layouts | About 8 MiB normally, up to 64 MiB while recent frames use them | Trimmed only while frames are drawn, not while all windows are idle |
| MyGo element state and frame arena | The largest frame drawn so far, plus up to 256 recycled element states | Kept for reuse |
