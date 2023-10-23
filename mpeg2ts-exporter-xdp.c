//go:build ignore

// clang -c -g -O2 -target bpf mpeg2ts-exporter-xdp.c -o mpeg2ts-exporter-xdp.o

// bpftool btf dump file /sys/kernel/btf/vmlinux format c > vmlinux.h
//#include "./vmlinux.h"
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
  	void *data_end = (void *)(long)ctx->data_end;
	void *data = (void *)(long)ctx->data;
	struct hdr_cursor nh;
	struct ethhdr *eth;
	int eth_type;
	int ip_type;
	int udp_destination;
	struct iphdr *iphdr;
	struct ipv6hdr *ipv6hdr;
	struct udphdr *udphdr;
        int *qidconf, index = ctx->rx_queue_index;

        // if XSK bound to queue(i.e. userspace program is running)
        // and traffic is to correct UDP port, send to XSK
        qidconf = bpf_map_lookup_elem(&qidconf_map, &index);
        if (qidconf) {

          // These keep track of the next header type and iterator pointer
          nh.pos = data;

          eth_type = parse_ethhdr(&nh, data_end, &eth);
          if (eth_type == bpf_htons(ETH_P_IP)) {
            ip_type = parse_iphdr(&nh, data_end, &iphdr);
              if (ip_type == IPPROTO_UDP) {
                udp_destination = parse_udphdr_destination(&nh, data_end, &udphdr);
                //if (udp_destination == bpf_htons(mpeg2tsport)) {
                switch (udp_destination) {
                  case bpf_htons(5000):
                    return bpf_redirect_map(&xsks_map, index, XDP_PASS);
                  case bpf_htons(5555):
                    return bpf_redirect_map(&xsks_map, index, XDP_PASS);
                  case bpf_htons(5500):
                    return bpf_redirect_map(&xsks_map, index, XDP_PASS);
                  case bpf_htons(2058):
                    return bpf_redirect_map(&xsks_map, index, XDP_PASS);
                  default:
                    break;
                }
              }
//          } else if (eth_type == bpf_htons(ETH_P_IPV6)) {
//            ip_type = parse_ip6hdr(&nh, data_end, &ipv6hdr);
//            if (ip_type == IPPROTO_UDPV6)
          }
        }

	// else, not a packet we're interested in, pass on for kernel to handle
	return XDP_PASS;
}
