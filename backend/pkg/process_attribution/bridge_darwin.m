//go:build darwin && cgo

#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>

#include "bridge_darwin.h"

#include <dispatch/dispatch.h>
#include <errno.h>
#include <libproc.h>
#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>
#include <sys/proc_info.h>

static _Atomic size_t flowlens_allocation_count = 0;

static void *flowlens_malloc(size_t size) {
    if (size == 0) {
        return NULL;
    }
    void *value = malloc(size);
    if (value != NULL) {
        atomic_fetch_add_explicit(&flowlens_allocation_count, 1, memory_order_relaxed);
    }
    return value;
}

static void flowlens_free(void *value) {
    if (value == NULL) {
        return;
    }
    free(value);
    atomic_fetch_sub_explicit(&flowlens_allocation_count, 1, memory_order_relaxed);
}

static char *flowlens_copy_string(NSString *value) {
    if (value == nil || value.length == 0) {
        return NULL;
    }
    const char *utf8 = value.UTF8String;
    if (utf8 == NULL) {
        return NULL;
    }
    size_t length = strlen(utf8) + 1;
    char *copy = flowlens_malloc(length);
    if (copy != NULL) {
        memcpy(copy, utf8, length);
    }
    return copy;
}

static NSBundle *flowlens_bundle_for_executable_path(NSString *executable_path) {
    if (executable_path.length == 0) {
        return nil;
    }
    NSString *candidate = executable_path.stringByDeletingLastPathComponent;
    NSBundle *bundle = nil;
    while (candidate.length > 1) {
        if ([candidate.pathExtension caseInsensitiveCompare:@"app"] == NSOrderedSame) {
            NSBundle *next = [NSBundle bundleWithPath:candidate];
            if (next != nil) {
                bundle = next;
            }
        }
        NSString *parent = candidate.stringByDeletingLastPathComponent;
        if ([parent isEqualToString:candidate]) {
            break;
        }
        candidate = parent;
    }
    return bundle;
}

typedef struct {
    pid_t pid;
    NSString *process_name;
    NSString *executable_path;
    flowlens_darwin_process_identity *output;
} flowlens_identity_context;

static void flowlens_fill_identity_on_main(void *raw_context) {
    flowlens_identity_context *context = raw_context;
    NSRunningApplication *running = [NSRunningApplication runningApplicationWithProcessIdentifier:context->pid];
    NSString *executable_path = running.executableURL.path;
    if (executable_path.length == 0) {
        executable_path = context->executable_path;
    }
    NSBundle *bundle = flowlens_bundle_for_executable_path(executable_path);

    NSString *bundle_id = running.bundleIdentifier;
    if (bundle_id.length == 0) {
        bundle_id = bundle.bundleIdentifier;
    }
    NSString *display_name = running.localizedName;
    if (display_name.length == 0) {
        NSDictionary *localized = bundle.localizedInfoDictionary;
        display_name = localized[@"CFBundleDisplayName"];
        if (display_name.length == 0) {
            display_name = localized[@"CFBundleName"];
        }
    }
    if (display_name.length == 0) {
        NSDictionary *info = bundle.infoDictionary;
        display_name = info[@"CFBundleDisplayName"];
        if (display_name.length == 0) {
            display_name = info[@"CFBundleName"];
        }
    }
    if (display_name.length == 0) {
        display_name = context->process_name;
    }

    context->output->display_name = flowlens_copy_string(display_name);
    context->output->process_name = flowlens_copy_string(context->process_name);
    context->output->executable_path = flowlens_copy_string(executable_path);
    context->output->bundle_id = flowlens_copy_string(bundle_id);
}

typedef struct {
    void *context;
    dispatch_function_t function;
} flowlens_main_call;

static void flowlens_invoke_appkit_call(void *raw_call) {
    flowlens_main_call *call = raw_call;
    @autoreleasepool {
        call->function(call->context);
    }
}

static void flowlens_run_on_appkit_main(void *context, dispatch_function_t function) {
    flowlens_main_call call = {.context = context, .function = function};
    if ([NSThread isMainThread] || NSApp == nil) {
        flowlens_invoke_appkit_call(&call);
        return;
    }
    dispatch_sync_f(dispatch_get_main_queue(), &call, flowlens_invoke_appkit_call);
}

int flowlens_darwin_copy_process_identity(int32_t pid, flowlens_darwin_process_identity *output) {
    if (pid <= 0 || output == NULL) {
        return EINVAL;
    }
    memset(output, 0, sizeof(*output));

    struct proc_bsdinfo bsd_info;
    memset(&bsd_info, 0, sizeof(bsd_info));
    errno = 0;
    int info_bytes = proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &bsd_info, sizeof(bsd_info));
    if (info_bytes != sizeof(bsd_info)) {
        return errno != 0 ? errno : ESRCH;
    }
    output->start_seconds = bsd_info.pbi_start_tvsec;
    output->start_microseconds = bsd_info.pbi_start_tvusec;

    char path_buffer[PROC_PIDPATHINFO_MAXSIZE];
    memset(path_buffer, 0, sizeof(path_buffer));
    errno = 0;
    int path_length = proc_pidpath(pid, path_buffer, sizeof(path_buffer));
    if (path_length <= 0) {
        output->metadata_denied = 1;
    }

    @autoreleasepool {
        NSString *executable_path = path_length > 0 ? [NSString stringWithUTF8String:path_buffer] : nil;
        size_t name_length = strnlen(bsd_info.pbi_name, sizeof(bsd_info.pbi_name));
        const char *process_bytes = bsd_info.pbi_name;
        if (name_length == 0) {
            name_length = strnlen(bsd_info.pbi_comm, sizeof(bsd_info.pbi_comm));
            process_bytes = bsd_info.pbi_comm;
        }
        NSString *process_name = name_length > 0
            ? [[NSString alloc] initWithBytes:process_bytes length:name_length encoding:NSUTF8StringEncoding]
            : [executable_path.lastPathComponent copy];
        flowlens_identity_context context = {
            .pid = pid,
            .process_name = process_name,
            .executable_path = executable_path,
            .output = output,
        };
        flowlens_run_on_appkit_main(&context, flowlens_fill_identity_on_main);
        [process_name release];
    }

    if (output->process_name == NULL && output->executable_path == NULL) {
        output->metadata_denied = 1;
    }
    return 0;
}

void flowlens_darwin_free_process_identity(flowlens_darwin_process_identity *identity) {
    if (identity == NULL) {
        return;
    }
    flowlens_free(identity->display_name);
    flowlens_free(identity->process_name);
    flowlens_free(identity->executable_path);
    flowlens_free(identity->bundle_id);
    memset(identity, 0, sizeof(*identity));
}

typedef struct {
    pid_t pid;
    NSString *executable_path;
    flowlens_darwin_buffer *output;
    int result;
} flowlens_icon_context;

static void flowlens_copy_icon_on_main(void *raw_context) {
    flowlens_icon_context *context = raw_context;
    NSRunningApplication *running = [NSRunningApplication runningApplicationWithProcessIdentifier:context->pid];
    NSImage *icon = running.icon;
    NSString *path = running.executableURL.path;
    if (path.length == 0) {
        path = context->executable_path;
    }
    NSBundle *bundle = flowlens_bundle_for_executable_path(path);
    if (icon == nil && bundle.bundlePath.length > 0) {
        icon = [[NSWorkspace sharedWorkspace] iconForFile:bundle.bundlePath];
    }
    if (icon == nil && path.length > 0) {
        icon = [[NSWorkspace sharedWorkspace] iconForFile:path];
    }
    if (icon == nil) {
        context->result = ENOENT;
        return;
    }

    CGImageRef image = [icon CGImageForProposedRect:NULL context:nil hints:nil];
    if (image == NULL) {
        context->result = ENOENT;
        return;
    }
    NSBitmapImageRep *representation = [[NSBitmapImageRep alloc] initWithCGImage:image];
    NSData *png = [representation representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
    [representation release];
    if (png.length == 0) {
        context->result = ENOENT;
        return;
    }
    uint8_t *bytes = flowlens_malloc(png.length);
    if (bytes == NULL) {
        context->result = ENOMEM;
        return;
    }
    memcpy(bytes, png.bytes, png.length);
    context->output->bytes = bytes;
    context->output->length = png.length;
    context->result = 0;
}

int flowlens_darwin_copy_process_icon(int32_t pid,
                                      const char *executable_path,
                                      flowlens_darwin_buffer *output) {
    if (output == NULL) {
        return EINVAL;
    }
    memset(output, 0, sizeof(*output));
    @autoreleasepool {
        NSString *path = executable_path != NULL ? [NSString stringWithUTF8String:executable_path] : nil;
        flowlens_icon_context context = {
            .pid = pid,
            .executable_path = path,
            .output = output,
            .result = ENOENT,
        };
        flowlens_run_on_appkit_main(&context, flowlens_copy_icon_on_main);
        return context.result;
    }
}

void flowlens_darwin_free_buffer(flowlens_darwin_buffer *buffer) {
    if (buffer == NULL) {
        return;
    }
    flowlens_free(buffer->bytes);
    memset(buffer, 0, sizeof(*buffer));
}

size_t flowlens_darwin_outstanding_allocations(void) {
    return atomic_load_explicit(&flowlens_allocation_count, memory_order_relaxed);
}
