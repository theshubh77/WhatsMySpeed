package network

import (
	"math"
	"testing"
	"time"
	"unsafe"
)

func TestMibIfRow2Layout(t *testing.T) {
	var row mibIfRow2
	var tbl mibIfTable2

	if got := unsafe.Offsetof(tbl.Table); got != 8 {
		t.Fatalf("MIB_IF_TABLE2.Table offset = %d, want 8", got)
	}
	if got := unsafe.Sizeof(row); got != 1352 {
		t.Fatalf("sizeof(MIB_IF_ROW2) = %d, want 1352", got)
	}

	checks := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"InterfaceLuid", unsafe.Offsetof(row.InterfaceLuid), 0},
		{"InterfaceIndex", unsafe.Offsetof(row.InterfaceIndex), 8},
		{"InterfaceGuid", unsafe.Offsetof(row.InterfaceGuid), 12},
		{"Alias", unsafe.Offsetof(row.Alias), 28},
		{"Description", unsafe.Offsetof(row.Description), 542},
		{"Type", unsafe.Offsetof(row.Type), 1128},
		{"Flags", unsafe.Offsetof(row.InterfaceAndOperStatusFlags), 1152},
		{"OperStatus", unsafe.Offsetof(row.OperStatus), 1156},
		{"InOctets", unsafe.Offsetof(row.InOctets), 1208},
		{"OutOctets", unsafe.Offsetof(row.OutOctets), 1280},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("offsetof(%s) = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestFormatSpeed(t *testing.T) {
	cases := []struct {
		in   float64
		val  string
		unit string
	}{
		{-5, "0", "B/s"},
		{math.NaN(), "0", "B/s"},
		{math.Inf(1), "0", "B/s"},
		{0, "0", "B/s"},
		{512, "512", "B/s"},
		{1023, "1023", "B/s"},
		{1023.8, "1.00", "KB/s"},
		{1024, "1.00", "KB/s"},
		{1536, "1.50", "KB/s"},
		{10 * 1024, "10.0", "KB/s"},
		{42.75 * 1024, "42.8", "KB/s"},
		{1023.95 * 1024, "1.00", "MB/s"},
		{1.5 * 1024 * 1024, "1.50", "MB/s"},
		{256 * 1024 * 1024, "256", "MB/s"},
		{2.25 * 1024 * 1024 * 1024, "2.25", "GB/s"},
	}

	for _, tc := range cases {
		got := FormatSpeed(tc.in)
		if got.Value != tc.val || got.Unit != tc.unit {
			t.Errorf("FormatSpeed(%v) = %q %q, want %q %q", tc.in, got.Value, got.Unit, tc.val, tc.unit)
		}
	}
}

func TestFormatSpeedBits(t *testing.T) {
	cases := []struct {
		inBytesPerSec float64
		val           string
		unit          string
	}{
		{-1, "0", "bps"},
		{math.NaN(), "0", "bps"},
		{0, "0", "bps"},
		{100, "800", "bps"},
		{125, "1.00", "Kbps"},
		{1500000, "12.0", "Mbps"},  // 12 Mbps
		{3750000, "30.0", "Mbps"},  // 30 Mbps
		{125000000, "1.00", "Gbps"}, // 1 Gbps
	}

	for _, tc := range cases {
		got := FormatSpeedBits(tc.inBytesPerSec)
		if got.Value != tc.val || got.Unit != tc.unit {
			t.Errorf("FormatSpeedBits(%v) = %q %q, want %q %q", tc.inBytesPerSec, got.Value, got.Unit, tc.val, tc.unit)
		}
	}
}

func TestMonitorDeltaAndWrapProtection(t *testing.T) {
	now := time.Unix(1000, 0)
	step := 0
	snapshots := [][]interfaceCounters{
		{{luid: 1, inOctets: 1000, outOctets: 500}},
		{{luid: 1, inOctets: 3000, outOctets: 1500}},
		// Counter reset on luid 1 + new interface luid 2 appears
		{{luid: 1, inOctets: 100, outOctets: 50}, {luid: 2, inOctets: 9000, outOctets: 4000}},
		// Both increment cleanly
		{{luid: 1, inOctets: 1100, outOctets: 550}, {luid: 2, inOctets: 10000, outOctets: 4500}},
	}

	m := newMonitorWithDeps(
		func() ([]interfaceCounters, error) {
			rows := snapshots[step]
			step++
			return rows, nil
		},
		func() time.Time { return now },
	)

	s0, err := m.Sample()
	if err != nil {
		t.Fatal(err)
	}
	if s0.DownloadBytesPerSec != 0 || s0.UploadBytesPerSec != 0 {
		t.Fatalf("first sample should be zero baseline, got %+v", s0)
	}

	now = now.Add(2 * time.Second)
	s1, err := m.Sample()
	if err != nil {
		t.Fatal(err)
	}
	if s1.DownloadBytesPerSec != 1000 || s1.UploadBytesPerSec != 500 {
		t.Fatalf("unexpected s1: %+v", s1)
	}

	now = now.Add(1 * time.Second)
	s2, err := m.Sample()
	if err != nil {
		t.Fatal(err)
	}
	if s2.DownloadBytesPerSec != 0 || s2.UploadBytesPerSec != 0 {
		t.Fatalf("counter reset/new NIC should not spike, got %+v", s2)
	}

	now = now.Add(1 * time.Second)
	s3, err := m.Sample()
	if err != nil {
		t.Fatal(err)
	}
	if s3.DownloadBytesPerSec != 2000 || s3.UploadBytesPerSec != 1000 {
		t.Fatalf("unexpected s3: %+v", s3)
	}
}

func TestLiveGetIfTable2(t *testing.T) {
	m := NewMonitor()
	s, err := m.Sample()
	if err != nil {
		t.Fatalf("live GetIfTable2 failed: %v", err)
	}
	t.Logf("Live active countable interfaces: %d", s.ActiveInterfaces)
}

func TestIsCountableInterfaceFiltersVirtualSwitches(t *testing.T) {
	copyU16 := func(dst []uint16, s string) {
		for i := 0; i < len(s) && i < len(dst)-1; i++ {
			dst[i] = uint16(s[i])
		}
	}

	// Real hardware NIC must always pass even if name has "Hyper-V" in driver text.
	hw := mibIfRow2{
		OperStatus:                  ifOperStatusUp,
		Type:                        6,
		InterfaceAndOperStatusFlags: flagHardwareInterface | flagConnectorPresent,
	}
	copyU16(hw.Alias[:], "Wi-Fi")
	if !isCountableInterface(&hw) {
		t.Fatal("expected hardware NIC to be countable")
	}

	// Non-hardware USB/cellular adapter with connector present should still pass.
	usb := mibIfRow2{
		OperStatus:                  ifOperStatusUp,
		Type:                        6,
		InterfaceAndOperStatusFlags: flagConnectorPresent,
	}
	copyU16(usb.Alias[:], "USB Ethernet")
	copyU16(usb.Description[:], "Remote NDIS Compatible Device")
	if !isCountableInterface(&usb) {
		t.Fatal("expected non-virtual RNDIS/tether adapter to be countable")
	}

	// Non-hardware vEthernet / WSL / VMware / VPN adapters must be excluded to prevent double-counting.
	for _, name := range []string{
		"vEthernet (WSL (Hyper-V firewall))",
		"Hyper-V Virtual Ethernet Adapter",
		"VMware Network Adapter VMnet8",
		"VirtualBox Host-Only Ethernet Adapter",
		"Tailscale Tunnel",
		"Npcap Loopback Adapter",
	} {
		vRow := mibIfRow2{
			OperStatus:                  ifOperStatusUp,
			Type:                        6,
			InterfaceAndOperStatusFlags: flagConnectorPresent,
		}
		copyU16(vRow.Alias[:], name)
		if isCountableInterface(&vRow) {
			t.Fatalf("virtual adapter %q should be excluded from physical counters", name)
		}
	}
}
