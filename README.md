# mpeg2ts-exporter
![image](https://github.com/wwijkander/mpeg2ts-exporter/assets/39839969/e7f3c9e9-75e7-49bb-ac03-f61271f2c394)

Prometheus exporter for MPEG-TS metrics from multicast IPTV streams. 

Implements a subset of ETSI TR 101 290,  "Digital Video Broadcasting (DVB); Measurement guidelines for DVB systems". Currently supported metrics:

Name | Description
--- | ---
iptv_cc_errors | Number of times in a group where the continuity count value was not the proper iteration or the same as the previous packet, indicating packet loss
iptv_duplicate_cc_errors | Number of times in a group where the continuity count value was the same as the previous packet
iptv_pat_errors | Number of times in a group where the required PAT packet was not transmitted at least twice a second, indicating packet loss
iptv_payload_bitrate | Bitrate for group, excluding UDP and below headers, including MPEG-TS headers and payload

Uses AF_XDP to bypass kernel network stack and as a result requires a recent Linux to run.

# Building

Requires clang, libbpf, and kernel headers

```
cd mpeg2ts-exporter
go generate
go build
```

# Running

Usage of ./mpeg2ts-exporter:
  -groups string
        comma separated list of multicast groups to work on (default "239.24.9.13")
  -interface string
        The interface on which the program should run on. (default "enp3s0")
  -queueid int
        The ID of the Rx queue to which to attach to on the network link.

The metrics server will listen on port 2112.

## Multi-queue NICs

If you run mpeg2ts-exporter on a multiqueue NIC you will first need to either set your NIC to only use one queue, or use Flow Director or equivalent(see ethtool -N and -U) to steer the relevant MPEG-TS packets to the queue you specify with --queueid flag(default 0)

```
# #to list current queues
# ethtool -l enp2s0
Channel parameters for enp2s0:
Pre-set maximums:
RX:             n/a
TX:             n/a
Other:          1
Combined:       2
Current hardware settings:
RX:             n/a
TX:             n/a
Other:          1
Combined:       2
# #in this case, change the number of combined rx/tx queues
# ethtool -L enp2s0 combined 1
```

## Known issues

mpeg2ts-exporter will replace all loaded XDP programs on the interface.

The XDP program will sometimes seemingly fail to replace the previously loaded XDP program resulting in no packets being passed to the userspace program, as a workaround do something like
```
ip l set enp2s0 xdp off
```
before starting anew.
