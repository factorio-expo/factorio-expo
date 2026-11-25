// LD_PRELOAD shim that caps the frame rate of the software-rendered client
// while /tmp/fpslimit.on exists.
// Xvfb has no vblank, so Factorio redraws as fast as llvmpipe allows and keeps
// every raster thread busy. Sleeping after each buffer swap bounds that work.
#define _GNU_SOURCE
#include <dlfcn.h>
#include <stdlib.h>
#include <time.h>
#include <unistd.h>

typedef void (*swap_fn)(void *display, unsigned long drawable);
typedef void *(*dlsym_fn)(void *handle, const char *name);

// The interposed dlsym below must reach glibc's own.
static void *real_dlsym(void *handle, const char *name) {
    static dlsym_fn real;
    if (!real) real = (dlsym_fn)dlvsym(RTLD_NEXT, "dlsym", "GLIBC_2.2.5");
    return real(handle, name);
}

static long frame_ns(void) {
    static long ns;
    if (!ns) {
        const char *v = getenv("FACTORIO_CLIENT_FPS");
        long fps = v ? atol(v) : 10;
        ns = 1000000000L / (fps > 0 ? fps : 10);
    }
    return ns;
}

void glXSwapBuffers(void *display, unsigned long drawable) {
    static swap_fn real;
    static struct timespec last;
    if (!real) real = (swap_fn)real_dlsym(RTLD_NEXT, "glXSwapBuffers");
    real(display, drawable);

    // Loading screens advance one step per frame, so the cap only applies once
    // the entrypoint's watchdog saw the client in the game (flag file).
    if (access("/tmp/fpslimit.on", F_OK) != 0) return;

    struct timespec now;
    clock_gettime(CLOCK_MONOTONIC, &now);
    long elapsed = (now.tv_sec - last.tv_sec) * 1000000000L + (now.tv_nsec - last.tv_nsec);
    if (elapsed < frame_ns()) {
        struct timespec wait = {0, frame_ns() - elapsed};
        nanosleep(&wait, NULL);
    }
    clock_gettime(CLOCK_MONOTONIC, &last);
}

// Factorio resolves GL entry points at run time, which bypasses plain symbol
// interposition; hand out the limiter when it asks for the swap function.
typedef void *(*proc_fn)(const unsigned char *name);

static void *lookup(const char *symbol, const unsigned char *name) {
    static const char swap[] = "glXSwapBuffers";
    const char *a = (const char *)name, *b = swap;
    while (*a && *a == *b) a++, b++;
    if (!*a && !*b) return (void *)glXSwapBuffers;
    proc_fn real = (proc_fn)real_dlsym(RTLD_NEXT, symbol);
    return real ? real(name) : NULL;
}

void *glXGetProcAddress(const unsigned char *name) { return lookup("glXGetProcAddress", name); }
void *glXGetProcAddressARB(const unsigned char *name) { return lookup("glXGetProcAddressARB", name); }

// SDL and Factorio look GL functions up with dlsym on the libGL handle, which
// ignores LD_PRELOAD order; answer the lookups the limiter cares about.
void *dlsym(void *handle, const char *name) {
    if (name && !__builtin_strcmp(name, "glXSwapBuffers")) return (void *)glXSwapBuffers;
    if (name && !__builtin_strcmp(name, "glXGetProcAddress")) return (void *)glXGetProcAddress;
    if (name && !__builtin_strcmp(name, "glXGetProcAddressARB")) return (void *)glXGetProcAddressARB;
    return real_dlsym(handle, name);
}
