# Proposal

## Why

Archiving the native-UI migration changes merged their deltas into `openspec/specs/`. Four requirements in those deltas describe behavior that later commits changed or removed without an OpenSpec change, so the main specs would contradict the shipped 2.0.0 application.

## What Changes

- **desktop-shell**: Remove Restore Initial State and its startup progress window, which commit `52dae83` removed. Replace the exit-on-failure startup requirement with the recovery window that commit `38fd46d` added.
- **traffic-inspection**: Remove WebSocket conversation bubbles. The detail panel and WebSocket Client show only the message list.
- **rewrite-rules**: Deleting an edited rule offers Cancel and Delete, as commit `6d1b63e` changed it, instead of save, discard, and cancel.

## Impact

Specifications only. No code, data, or copy changes.
