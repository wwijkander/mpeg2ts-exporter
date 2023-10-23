//go:generate clang -c -g -O2 -target bpf mpeg2ts-exporter-xdp.c -o mpeg2ts-exporter-xdp.o
//go:generate clang -c -g -O2 -target bpf mpeg2ts-exporter-xdp-flow-director.c -o mpeg2ts-exporter-xdp-flow-director.o

package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"strings"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	xdp "github.com/wwijkander/go-xdp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

var (
	patErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "iptv_pat_errors",
			Help: "IPTV: ETSI TR 101 290 PAT errors",
		},
		[]string{"group"},
	)
	pmtErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "iptv_pmt_errors",
			Help: "IPTV: ETSI TR 101 290 PMT errors",
		},
		[]string{"group"},
	)
	ccErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "iptv_cc_errors",
			Help: "IPTV: ETSI TR 101 290 continuity count(CC) out of sync errors ",
		},
		[]string{"group"},
	)
	ccDuplicateErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "iptv_duplicate_cc_errors",
			Help: "IPTV: ETSI TR 101 290 continuity count(CC) duplicate errors ",
		},
		[]string{"group"},
	)
	bitsps = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "iptv_payload_bitrate",
			Help: "IPTV: payload bitrate",
		},
		[]string{"group"},
	)
	cc                                         = make(map[string]map[uint64]uint64)
	pmtCounter                                 = make(map[string]map[uint64]uint64)
	patCounter                                 = make(map[string]uint64)
	bytesCounter                               = make(map[string]uint64)
	bytesCounterMu, patCounterMu, pmtCounterMu = sync.RWMutex{}, sync.RWMutex{}, sync.RWMutex{}
	packetGroup                                string
	pid                                        uint64

	eth           layers.Ethernet
	ip4           layers.IPv4
	ip6           layers.IPv6
	udp           layers.UDP
	payload       gopacket.Payload
	groups        = []string{}
	payloadLength int
)

func main() {
	var ifaceName string
	var queueID int
	var groupsStr string

	flag.StringVar(&ifaceName, "interface", "enp3s0", "The interface on which the program should run on.")
	flag.IntVar(&queueID, "queueid", 0, "The ID of the Rx queue to which to attach to on the network link.")
	flag.StringVar(&groupsStr, "groups", "239.24.9.13", "comma separated list of multicast groups to work on")
	flag.Parse()

	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		log.Fatalf("Failed to look up specified interface %q: %s", ifaceName, err)
	}

	ifIndex := iface.Index

	// Join a mcast group. Port 9999 is irrelevant and will never receive any packets
	connection, err := net.ListenPacket("udp", "127.0.0.1:9999")
	if err != nil {
		panic(err)
	}
	defer connection.Close()

	connection6, err := net.ListenPacket("udp6", "[::1]:9999")
	if err != nil {
		panic(err)
	}
	defer connection6.Close()

	packetConn := ipv4.NewPacketConn(connection)
	packet6Conn := ipv6.NewPacketConn(connection6)

	groups = strings.Split(groupsStr, ",")
	for _, v := range groups {
		group := net.ParseIP(v)
		if !group.IsMulticast() {
			log.Printf("Ignoring invalid multicast group %s", v)
		}
		if group.To4() == nil {
			// IPv6
			//log.Fatalf("IPv6 multicast not currently supported")
			if err := packet6Conn.JoinGroup(iface, &net.UDPAddr{IP: group}); err != nil {
				log.Fatalf("Failed to join multicast group %s", v)
			}
			log.Println("Joined IPv6 " + group.String())
		} else {
			// IPv4
			if err := packetConn.JoinGroup(iface, &net.UDPAddr{IP: group}); err != nil {
				log.Fatalf("Failed to join multicast group %s", v)
			}
			log.Println("Joined IPv4 " + group.String())
		}

		cc[group.String()] = make(map[uint64]uint64)
		pmtCounter[group.String()] = make(map[uint64]uint64)

	}

	go func() {
		//time.Sleep(20 * time.Second)
		log.Println("Starting metrics server on port 2112...")
		http.Handle("/metrics", promhttp.Handler())
		log.Fatal(http.ListenAndServe(":2112", nil))
	}()

	go ticker()

	var program *xdp.Program

	program, err = xdp.LoadProgram("./mpeg2ts-exporter-xdp.o", "xdp_mpeg2ts_exporter_prog", "qidconf_map", "xsks_map")
	if err != nil {
		log.Fatalf("Failed to load xdp program: %v\n", err)
	}
	defer program.Close()

	if err := program.Attach(ifIndex); err != nil {
		log.Fatalf("Failed to attach xdp program to interface: %v\n", err)
	}
	defer program.Detach(ifIndex)

	// Create and initialize an XDP socket attached to our chosen network link.
	xsk, err := xdp.NewSocket(ifIndex, queueID, nil)
	if err != nil {
		log.Fatalf("Failed to create an XDP socket: %v\n", err)
	}

	// Register our XDP socket file descriptor with the eBPF program so it can be redirected packets
	if err = program.Register(queueID, xsk.FD()); err != nil {
		log.Fatalf("Failed to register socket in BPF map: %v\n", err)
	}
	defer program.Unregister(queueID)

	parser := gopacket.NewDecodingLayerParser(layers.LayerTypeEthernet, &eth, &ip4, &ip6, &udp, &payload)
	//decoded := []gopacket.LayerType{}
	decoded := make([]gopacket.LayerType, 0, 5)

	log.Println("Starting XSK polling...")
	for {
		// If there are any free slots on the Fill queue...
		if n := xsk.NumFreeFillSlots(); n > 0 {
			// ...then fetch up to that number of not-in-use
			// descriptors and push them onto the Fill ring queue
			// for the kernel to fill them with the received
			// frames.
			xsk.Fill(xsk.GetDescs(n, true))
		}

		// Wait for receive - meaning the kernel has
		// produced one or more descriptors filled with a received
		// frame onto the Rx ring queue.
		//log.Printf("waiting for frame(s) to be received...")
		numRx, _, err := xsk.Poll(-1)
		if err != nil {
			log.Fatalf("Failed polling XSK: %v\n", err)
		}

		if numRx > 0 {
			// Consume the descriptors filled with received frames
			// from the Rx ring queue.
			rxDescs := xsk.Receive(numRx)

			for i := 0; i < len(rxDescs); i++ {
				pktData := xsk.GetFrame(rxDescs[i])

				if err := parser.DecodeLayers(pktData, &decoded); err != nil {
					log.Printf("Could not decode layers: %v\n", err)
					continue
				}

				for _, layerType := range decoded {
					switch layerType {
					case layers.LayerTypeIPv6:
						//log.Println("IPv6 packet: ", ip6.SrcIP, " ", ip6.DstIP)
						packetGroup = ip6.DstIP.String()
					case layers.LayerTypeIPv4:
						//log.Println("IPv4 packet: ", ip4.SrcIP, " ", ip4.DstIP)
						//log.Println("IPv4 Checksum: " + fmt.Sprintf("%x", ip4.Checksum))
						//log.Println("UDP Checksum: " + fmt.Sprintf("%x", udp.Checksum))
						packetGroup = ip4.DstIP.String()
					}
				}

				if cc[packetGroup] == nil {
					log.Println("Packet from group we're not part of: " + packetGroup)
					//log.Println(hex.Dump(payload))
					continue
				}

				payloadLength = len(payload)

				bytesCounterMu.Lock()
				bytesCounter[packetGroup] += uint64(payloadLength)
				bytesCounterMu.Unlock()

				// Transport stream packets shall be 188 bytes long. The sync_byte is a fixed 8-bit field whose value is '0100 0111' (0x47).
				for mp2tHeader := 0; mp2tHeader+187 <= payloadLength && payload[mp2tHeader] == 0x47; mp2tHeader += 188 {

					// TODO transport_error_indicator
					// We do not care about payload_unit_start_indicator
					// We do not care about transport_indicator
					// what pid is this mp2t packet from
					pid := uint64((payload[mp2tHeader+1]>>4)&0x01) + uint64(payload[mp2tHeader+1]&0x0f) + uint64(payload[mp2tHeader+2])

					if pid == 0x0000 {
						// Program Allocation Table
						patCounterMu.Lock()
						patCounter[packetGroup]++
						patCounterMu.Unlock()
					}

					//	if payload[mp2tHeader+187] == 0xff {
					//		switch payload[mp2tHeader+5] {
					//		 	case 0x00:
					//		 		patCounterMu.Lock()
					//		 		patCounter[packetGroup]++
					//		 		patCounterMu.Unlock()
					//		case 0x02:
					//			pmtCounterMu.Lock()
					//			pmtCounter[packetGroup][pid]++
					//			pmtCounterMu.Unlock()
					//		}
					//	}
					// We do not care about transport_scrambling_control
					// We do not care about adaptation_field_control
					// continuity_count
					c := uint64(payload[mp2tHeader+3] & 0x0f)
					if (cc[packetGroup][pid] < 0xf && c != cc[packetGroup][pid]+0x1) || (cc[packetGroup][pid] == 0xf && c != 0x0) {
						//	ccSkip := c - cc[packetGroup][pid]
						//	if cc[packetGroup][pid] > c {
						//		ccSkip += 0xf
						//	}
						//	ccErrors.WithLabelValues(packetGroup).Add(float64(ccSkip))
						if cc[packetGroup][pid] == c {
							ccDuplicateErrors.WithLabelValues(packetGroup).Inc()
							//log.Printf("Dup! %s PID %x counters: %x %x\n", packetGroup, pid, cc[packetGroup][pid], c)
							continue
						} else {
							ccErrors.WithLabelValues(packetGroup).Inc()
						}
						//log.Printf("Skip! %s PID %x counters: %x %x\n", packetGroup, pid, cc[packetGroup][pid], c)
					}
					cc[packetGroup][pid] = c
				}
			}
		}
	}
}

func ticker() {
	for {
		timeCounter := time.Now()
		time.Sleep(1 * time.Second)
		tick := uint64(time.Since(timeCounter).Seconds())
		for _, group := range groups {
			bytesCounterMu.Lock()
			bitsps.WithLabelValues(group).Set(float64((bytesCounter[group] / tick) * 8))
			bytesCounter[group] = 0
			bytesCounterMu.Unlock()
			patCounterMu.Lock()
			if patCounter[group]/tick < 2 {
				patErrors.WithLabelValues(group).Inc()
			}
			patCounter[group] = 0
			patCounterMu.Unlock()
			// TODO
			//pmtCounterMu.Lock()
			//for _, v := range pmtCounter[group] {
			//	if pmtCounter[group][v]/tick < 1 {
			//		//log.Println(group + " PID " + string(pid))
			//		pmtErrors.WithLabelValues(group).Inc()
			//	}
			//	pmtCounter[group][v] = 0
			//}
			//pmtCounterMu.Unlock()
			//ccDesyncCounterMu.Lock()
			//ccErrors.WithLabelValues(group).Add(ccDesyncCounter[group])
		}
	}
}
