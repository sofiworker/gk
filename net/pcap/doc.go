// Package pcap provides capture file I/O for classic PCAP and PCAPNG.
// Readers detect formats automatically; writers select them with WithFormat.
// Format implementations and PCAPNG block APIs live in this package so callers
// share one Packet type and one set of reader, writer, and filter entry points.
package pcap
