package handler

import "testing"

func TestNormalizePacketCaptureRequestDefaultsAndCaps(t *testing.T) {
	req, err := normalizePacketCaptureRequest(PacketCaptureRequest{
		TargetType: "local",
		Interface:  "",
		Port:       "08080",
		Protocol:   "",
		Count:      999,
		Duration:   999,
	})
	if err != nil {
		t.Fatalf("normalizePacketCaptureRequest returned error: %v", err)
	}
	if req.Interface != "any" {
		t.Fatalf("expected default interface any, got %q", req.Interface)
	}
	if req.Port != "8080" {
		t.Fatalf("expected normalized port 8080, got %q", req.Port)
	}
	if req.Protocol != "any" {
		t.Fatalf("expected default protocol any, got %q", req.Protocol)
	}
	if req.Count != 500 {
		t.Fatalf("expected capped count 500, got %d", req.Count)
	}
	if req.Duration != 60 {
		t.Fatalf("expected capped duration 60, got %d", req.Duration)
	}
}

func TestNormalizePacketCaptureRequestRejectsUnsafeValues(t *testing.T) {
	cases := []PacketCaptureRequest{
		{TargetType: "server", ServerID: 0},
		{TargetType: "local", Interface: "eth0;rm -rf /"},
		{TargetType: "local", Port: "70000"},
		{TargetType: "local", Host: "example.com;curl bad"},
		{TargetType: "local", Protocol: "http"},
	}
	for _, tc := range cases {
		if _, err := normalizePacketCaptureRequest(tc); err == nil {
			t.Fatalf("expected error for %#v", tc)
		}
	}
}

func TestBuildPacketCaptureFilter(t *testing.T) {
	req := PacketCaptureRequest{Protocol: "tcp", Port: "443", Host: "192.168.1.10"}
	got := buildPacketCaptureFilter(req)
	want := "tcp and port 443 and host 192.168.1.10"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestParsePacketCaptureOutput(t *testing.T) {
	output := `tcpdump: listening on any, link-type LINUX_SLL2 (Linux cooked v2), snapshot length 262144 bytes
2026-07-12 10:20:30.123456 IP 192.168.1.8.52210 > 93.184.216.34.443: Flags [P.], seq 1:45, ack 1, win 256, length 44
2026-07-12 10:20:31.123456 IP 192.168.1.8.5353 > 224.0.0.251.5353: UDP, length 64
`
	records := parsePacketCaptureOutput(output)
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].Protocol != "TCP" || records[0].Source != "192.168.1.8.52210" || records[0].Length != "44" {
		t.Fatalf("unexpected first record: %#v", records[0])
	}
	if records[1].Protocol != "UDP" || records[1].Length != "64" {
		t.Fatalf("unexpected second record: %#v", records[1])
	}
	summary := summarizePacketRecords(records)
	if summary.Total != 2 || summary.Protocols["TCP"] != 1 || summary.Protocols["UDP"] != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}
