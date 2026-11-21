# Design

## Context

See proposal.md for motivation. `decodeImage` passes raster bytes to MyGo's `ui.DecodeBitmap`, which uses Go's registered image decoders. ICO MIME types already classify as images and display an ICO format label. AVIF already extends this path through decoder registration.

## Goals / Non-Goals

Add ICO decoding to the existing viewer without changing body storage, classification, or UI controls. No format conversion is persisted and no icon-size chooser is added.

## Decisions

- Pin `github.com/sergeymakinen/go-ico` v1.0.0 (and its pure-Go BMP dependency) to register ICO decoding. Reuse `ui.DecodeBitmap` so bitmap pixel release/recreation retains the existing lifetime behavior. The codec selects a stored representation; the viewer does not decode and retain all sizes.
- Before decoding a recognized ICO signature, validate its directory and payload ranges against the available bytes. Use the codec's config decoder to reject dimensions outside ICO's 1..256 range before allocating pixels. Invalid ICO data uses the existing failure notice; arbitrary unknown formats keep the unsupported notice.
- Decode within `inspect`'s existing `runtime.Run` image job. Results remain main-thread-owned; cancellation, replacement, stale-result rejection, and cleanup follow the existing image preview implementation.
- Retain the current `Image / Hex` controls, image metadata, zoom, pan, and saving in all shared viewer surfaces. No new English/Chinese copy, theme colors, key handlers, or window APIs are required.
- Extend `logic`'s existing filename table so both ICO MIME types suggest `.ico` when the URL has no usable extension; file contents remain unchanged.

## Risks / Trade-offs

- A new image registration must not regress existing formats; run the existing image format tests and all Go packages.
- Broken directory sizes must not drive allocations based on untrusted lengths; validate before the registered decoder reads them.
- The codec does not implement every historical DIB variant. Unsupported payloads continue to show the existing error and preserve Hex/save access.

## Migration Plan

No persistence or codec migration. Revert the dependency and preview integration to roll back. Verification is headless on Windows, with no claim of native macOS/Linux runtime acceptance.
