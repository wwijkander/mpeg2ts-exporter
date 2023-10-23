# mpeg2ts-exporter
![image](https://github.com/wwijkander/mpeg2ts-exporter/assets/39839969/e7f3c9e9-75e7-49bb-ac03-f61271f2c394)

Prometheus exporter for MPEG-TS metrics from multicast IPTV streams. Currently supported metrics:

Name | Description
--- | ---
iptv_cc_errors | Number of times in a group where the continuity count value was not the proper iteration or the same as the previous packet
iptv_duplicate_cc_errors | Number of times in a group where the continuity count value was the same as the previous packet
iptv_pat_errors | Number of times in a group where the required PAT packet was not transmitted at least twice a second
iptv_payload_bitrate | Bitrate for group, excluding UDP and below headers, including MPEG-TS headers and payload
