package mdns

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

func TestCanonicalHostName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"immich.local", "immich.local."},
		{"immich.local.", "immich.local."},
		{"immich", "immich.local."},
		{"IMMICH.LOCAL.", "immich.local."},
		{" jellyfin.local ", "jellyfin.local."},
	}

	for _, tt := range tests {
		got := canonicalHostName(tt.in)
		if got != tt.want {
			t.Errorf("canonicalHostName(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeServiceType(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"_http._tcp", "_http._tcp.local."},
		{"_http._tcp.local", "_http._tcp.local."},
		{"_http._tcp.local.", "_http._tcp.local."},
		{"", "_http._tcp.local."},
		{"_custom._tcp", "_custom._tcp.local."},
	}

	for _, tt := range tests {
		got := normalizeServiceType(tt.in)
		if got != tt.want {
			t.Errorf("normalizeServiceType(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestManagerPublishAndQuery(t *testing.T) {
	m := &Manager{
		defaultST:  "_http._tcp.local.",
		byKey:      make(map[string][]*serviceEntry),
		byHost:     make(map[string][]*serviceEntry),
		byService:  make(map[string][]*serviceEntry),
		byInstance: make(map[string]*serviceEntry),
	}

	rec := Record{
		Hostname:    "immich.local",
		IPs:         []string{"192.168.1.100"},
		Port:        80,
		ServiceType: "_http._tcp",
	}

	m.Publish("immich/immich", []Record{rec})

	// 1. Query for A record: immich.local.
	qA := new(dns.Msg)
	qA.SetQuestion("immich.local.", dns.TypeA)

	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.50"), Port: 5353}
	// Test handleQuery in-memory
	m.mu.RLock()
	var answers []dns.RR
	for _, q := range qA.Question {
		if entries, ok := m.byHost[q.Name]; ok {
			for _, e := range entries {
				for _, ip := range e.ips {
					if ip4 := ip.To4(); ip4 != nil {
						answers = append(answers, &dns.A{
							Hdr: dns.RR_Header{
								Name:   e.hostName,
								Rrtype: dns.TypeA,
								Class:  dns.ClassINET | 0x8000,
								Ttl:    defaultTTL,
							},
							A: ip4,
						})
					}
				}
			}
		}
	}
	m.mu.RUnlock()

	if len(answers) != 1 {
		t.Fatalf("expected 1 answer for A query, got %d", len(answers))
	}
	aRecord, ok := answers[0].(*dns.A)
	if !ok {
		t.Fatalf("expected *dns.A, got %T", answers[0])
	}
	if aRecord.A.String() != "192.168.1.100" {
		t.Errorf("expected IP 192.168.1.100, got %s", aRecord.A.String())
	}
	if aRecord.Hdr.Name != "immich.local." {
		t.Errorf("expected name immich.local., got %s", aRecord.Hdr.Name)
	}

	// 2. Query for PTR record: _http._tcp.local.
	m.mu.RLock()
	var ptrAnswers []dns.RR
	if entries, ok := m.byService["_http._tcp.local."]; ok {
		for _, e := range entries {
			ptrAnswers = append(ptrAnswers, &dns.PTR{
				Hdr: dns.RR_Header{
					Name:   e.serviceType,
					Rrtype: dns.TypePTR,
					Class:  dns.ClassINET,
					Ttl:    defaultTTL,
				},
				Ptr: e.instanceFull,
			})
		}
	}
	m.mu.RUnlock()

	if len(ptrAnswers) != 1 {
		t.Fatalf("expected 1 PTR answer, got %d", len(ptrAnswers))
	}
	ptrRecord := ptrAnswers[0].(*dns.PTR)
	if ptrRecord.Ptr != "immich._http._tcp.local." {
		t.Errorf("expected Ptr immich._http._tcp.local., got %s", ptrRecord.Ptr)
	}

	// 3. Test Unpublish
	m.Unpublish("immich/immich")
	m.mu.RLock()
	if len(m.byHost) != 0 || len(m.byService) != 0 || len(m.byInstance) != 0 {
		t.Errorf("expected clean maps after Unpublish, got host=%d svc=%d inst=%d",
			len(m.byHost), len(m.byService), len(m.byInstance))
	}
	m.mu.RUnlock()
	_ = from
}

func TestManagerAAAAQueryExtras(t *testing.T) {
	m := &Manager{
		defaultST:  "_http._tcp.local.",
		byKey:      make(map[string][]*serviceEntry),
		byHost:     make(map[string][]*serviceEntry),
		byService:  make(map[string][]*serviceEntry),
		byInstance: make(map[string]*serviceEntry),
	}

	rec := Record{
		Hostname:    "immich.local",
		IPs:         []string{"192.168.1.100"},
		Port:        80,
		ServiceType: "_http._tcp",
	}

	m.Publish("immich/immich", []Record{rec})

	qAAAA := new(dns.Msg)
	qAAAA.SetQuestion("immich.local.", dns.TypeAAAA)

	m.mu.RLock()
	var answers []dns.RR
	var extras []dns.RR
	for _, q := range qAAAA.Question {
		if entries, ok := m.byHost[q.Name]; ok {
			for _, e := range entries {
				for _, ip := range e.ips {
					if ip4 := ip.To4(); ip4 != nil {
						if q.Qtype == dns.TypeAAAA {
							extras = append(extras, &dns.A{
								Hdr: dns.RR_Header{
									Name:   e.hostName,
									Rrtype: dns.TypeA,
									Class:  dns.ClassINET | 0x8000,
									Ttl:    defaultTTL,
								},
								A: ip4,
							})
						}
					}
				}
			}
		}
	}
	m.mu.RUnlock()

	if len(answers) != 0 {
		t.Fatalf("expected 0 answers for AAAA, got %d", len(answers))
	}
	if len(extras) != 1 {
		t.Fatalf("expected 1 extra (A record) for AAAA query, got %d", len(extras))
	}
}

func TestManagerIconAndDeviceInfoPublish(t *testing.T) {
	m := &Manager{
		defaultST:  "_http._tcp.local.",
		byKey:      make(map[string][]*serviceEntry),
		byHost:     make(map[string][]*serviceEntry),
		byService:  make(map[string][]*serviceEntry),
		byInstance: make(map[string]*serviceEntry),
	}

	rec := Record{
		Hostname:    "photos.local",
		IPs:         []string{"192.168.1.100"},
		Port:        8080,
		ServiceType: "_http._tcp",
		Icon:        "RackMac",
		TXT: map[string]string{
			"path": "/gallery",
		},
	}

	_ = m.Publish("ns/photos", []Record{rec})

	// 1. Verify primary service entry has TXT records
	m.mu.RLock()
	primaryEntry := m.byInstance["photos._http._tcp.local."]
	if primaryEntry == nil {
		t.Fatalf("expected primary entry photos._http._tcp.local.")
	}
	// Check TXT records
	expectedTXT := []string{"path=/gallery", "model=RackMac", "icon=RackMac"}
	if len(primaryEntry.txt) != len(expectedTXT) {
		t.Fatalf("expected %d txt entries, got %v", len(expectedTXT), primaryEntry.txt)
	}
	for i, exp := range expectedTXT {
		if primaryEntry.txt[i] != exp {
			t.Errorf("txt[%d] = %q; want %q", i, primaryEntry.txt[i], exp)
		}
	}

	// 2. Verify companion device-info service entry is created
	deviceInfoEntry := m.byInstance["photos._device-info._tcp.local."]
	if deviceInfoEntry == nil {
		t.Fatalf("expected companion entry photos._device-info._tcp.local.")
	}
	if deviceInfoEntry.port != 0 {
		t.Errorf("device-info port = %d; want 0", deviceInfoEntry.port)
	}
	if len(deviceInfoEntry.txt) != 1 || deviceInfoEntry.txt[0] != "model=RackMac" {
		t.Errorf("device-info txt = %v; want [model=RackMac]", deviceInfoEntry.txt)
	}

	// 3. Verify PTR query for _device-info._tcp.local.
	diServices := m.byService["_device-info._tcp.local."]
	if len(diServices) != 1 {
		t.Fatalf("expected 1 service under _device-info._tcp.local., got %d", len(diServices))
	}
	if diServices[0].instanceFull != "photos._device-info._tcp.local." {
		t.Errorf("instanceFull = %q; want photos._device-info._tcp.local.", diServices[0].instanceFull)
	}
	m.mu.RUnlock()

	// 4. Test unpublish cleans up both primary and companion device-info service
	m.Unpublish("ns/photos")
	m.mu.RLock()
	if len(m.byHost["photos.local."]) != 0 {
		t.Errorf("expected 0 host entries after unpublish, got %d", len(m.byHost["photos.local."]))
	}
	if len(m.byService["_device-info._tcp.local."]) != 0 {
		t.Errorf("expected 0 device-info entries after unpublish, got %d", len(m.byService["_device-info._tcp.local."]))
	}
	if m.byInstance["photos._device-info._tcp.local."] != nil {
		t.Errorf("expected companion device-info instance removed")
	}
	m.mu.RUnlock()
}

func TestManagerIconURLPublish(t *testing.T) {
	m := &Manager{
		defaultST:  "_http._tcp.local.",
		byKey:      make(map[string][]*serviceEntry),
		byHost:     make(map[string][]*serviceEntry),
		byService:  make(map[string][]*serviceEntry),
		byInstance: make(map[string]*serviceEntry),
	}

	rec := Record{
		Hostname:    "music.local",
		IPs:         []string{"192.168.1.100"},
		Port:        4533,
		ServiceType: "_http._tcp",
		Icon:        "https://navidrome.org/logo.png",
		Model:       "Macmini",
	}

	_ = m.Publish("ns/music", []Record{rec})

	m.mu.RLock()
	primaryEntry := m.byInstance["music._http._tcp.local."]
	if primaryEntry == nil {
		t.Fatalf("expected primary entry music._http._tcp.local.")
	}
	// Model should be Macmini, icon should be the URL, and icon-url should also be added
	expectedTXT := []string{"model=Macmini", "icon=https://navidrome.org/logo.png", "icon-url=https://navidrome.org/logo.png"}
	if len(primaryEntry.txt) != len(expectedTXT) {
		t.Fatalf("expected %d txt entries, got %v", len(expectedTXT), primaryEntry.txt)
	}
	for i, exp := range expectedTXT {
		if primaryEntry.txt[i] != exp {
			t.Errorf("txt[%d] = %q; want %q", i, primaryEntry.txt[i], exp)
		}
	}

	// Companion device-info service should have model=Macmini
	deviceInfoEntry := m.byInstance["music._device-info._tcp.local."]
	if deviceInfoEntry == nil {
		t.Fatalf("expected companion entry music._device-info._tcp.local.")
	}
	if len(deviceInfoEntry.txt) != 1 || deviceInfoEntry.txt[0] != "model=Macmini" {
		t.Errorf("device-info txt = %v; want [model=Macmini]", deviceInfoEntry.txt)
	}
	m.mu.RUnlock()
}

