//go:build ignore

// clang -c -g -O2 -target bpf mpeg2ts-exporter-xdp.c -o mpeg2ts-exporter-xdp.o

#include <linux/bpf.h>
#include <linux/in.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include "./parsing_helpers.h"

char __license[] SEC("license") = "MIT";

struct {
       __uint(type, BPF_MAP_TYPE_XSKMAP);
       __uint(key_size, sizeof(int));
       __uint(value_size, sizeof(int));
       __uint(max_entries, 256);
} xsks_map SEC(".maps");

struct {
       __uint(type, BPF_MAP_TYPE_ARRAY);
       __uint(key_size, sizeof(int));
       __uint(value_size, sizeof(int));
       __uint(max_entries, 256);
} qidconf_map SEC(".maps");

SEC("xdp_mpeg2ts_exporter")
int xdp_mpeg2ts_exporter_prog(struct xdp_md *ctx) {
        int *qidconf, index = ctx->rx_queue_index;

        // if XSK bound to queue(i.e. userspace program is running)
        // and traffic is to correct UDP port, send to XSK
        qidconf = bpf_map_lookup_elem(&qidconf_map, &index);
        if (qidconf) {
          return bpf_redirect_map(&xsks_map, index, XDP_PASS);
        }
        // else, no user space program, pass on for kernel to handle
        return XDP_PASS;
}
