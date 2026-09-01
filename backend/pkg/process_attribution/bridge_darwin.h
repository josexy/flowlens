#ifndef FLOWLENS_PROCESS_ATTRIBUTION_BRIDGE_DARWIN_H
#define FLOWLENS_PROCESS_ATTRIBUTION_BRIDGE_DARWIN_H

#include <stddef.h>
#include <stdint.h>

typedef struct {
    uint64_t start_seconds;
    uint64_t start_microseconds;
    char *display_name;
    char *process_name;
    char *executable_path;
    char *bundle_id;
    int32_t metadata_denied;
} flowlens_darwin_process_identity;

typedef struct {
    uint8_t *bytes;
    size_t length;
} flowlens_darwin_buffer;

int flowlens_darwin_copy_process_identity(int32_t pid, flowlens_darwin_process_identity *output);
void flowlens_darwin_free_process_identity(flowlens_darwin_process_identity *identity);

int flowlens_darwin_copy_process_icon(int32_t pid, const char *executable_path, flowlens_darwin_buffer *output);
void flowlens_darwin_free_buffer(flowlens_darwin_buffer *buffer);

size_t flowlens_darwin_outstanding_allocations(void);

#endif
