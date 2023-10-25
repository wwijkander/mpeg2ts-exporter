# mpeg2ts-exporter
![image](https://github.com/wwijkander/mpeg2ts-exporter/assets/39839969/e7f3c9e9-75e7-49bb-ac03-f61271f2c394)

Prometheus exporter for MPEG-TS metrics from multicast IPTV streams. 

Implements a subset of ETSI technical report 101 290,  "Digital Video Broadcasting (DVB); Measurement guidelines for DVB systems". Currently supported metrics:

Name | Description
--- | ---
iptv_cc_errors | Number of times in a group where the continuity count value was not the proper iteration or the same as the previous packet, indicating packet loss
iptv_duplicate_cc_errors | Number of times in a group where the continuity count value was the same as the previous packet
iptv_pat_errors | Number of times in a group where the required PAT packet was not transmitted at least twice a second, indicating packet loss
iptv_payload_bitrate | Bitrate for group, excluding UDP and below headers, including MPEG-TS headers and payload

Uses AF_XDP to bypass kernel network stack and as a result requires a fairly recent Linux to run.

# Building

Requires go, clang, libbpf, and kernel headers.

TODO: It is assumed that your IPTV stream is on UDP port 2058,5000,5500 or 5555. If this is not the case you will have to edit `mpeg2ts-exporter-xdp.c` before compiling, for now.

To build, do something similar to this:

```
cd mpeg2ts-exporter
go generate
go build
```

# Running

See `mpeg2ts-exporter --help`.

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
before starting mpeg2ts-exporter anew.

## Performance

Not great, not terrible. CPU pressure on a dedicated probe with a 2GHz Celeron J4125 CPU:

```
some avg10=4.32 avg60=4.80 avg300=4.93 total=670592782627
```

## References

[INTERNATIONAL STANDARD ISO/IEC 13818-1 RECOMMENDATION ITU-T H.222.0 (06/2021)](https://www.itu.int/rec/dologin_pub.asp?lang=e&id=T-REC-H.222.0-202106-S!!PDF-E&type=items)
[ETSI TR 101 290 V1.4.1 (2020-06)](https://www.etsi.org/deliver/etsi_tr/101200_101299/101290/01.04.01_60/tr_101290v010401p.pdf)
