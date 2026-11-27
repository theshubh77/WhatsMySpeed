package network

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ifOperStatusUp        uint32 = 1
	ifTypeSoftwareLoopback uint32 = 24
	ifTypeTunnel          uint32 = 131

	flagHardwareInterface uint8 = 1 << 0
	flagFilterInterface   uint8 = 1 << 1
	flagConnectorPresent  uint8 = 1 << 2
)

var (
	modIphlpapi      = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetIfTable2  = modIphlpapi.NewProc("GetIfTable2")
	procFreeMibTable = modIphlpapi.NewProc("FreeMibTable")
)

// mibIfRow2 mirrors Windows MIB_IF_ROW2 from netioapi.h.
type mibIfRow2 struct {
	InterfaceLuid               uint64
	InterfaceIndex              uint32
	InterfaceGuid               windows.GUID
	Alias                       [257]uint16
	Description                 [257]uint16
	PhysicalAddressLength       uint32
	PhysicalAddress             [32]byte
	PermanentPhysicalAddress    [32]byte
	Mtu                         uint32
	Type                        uint32
	TunnelType                  uint32
	MediaType                   uint32
	PhysicalMediumType          uint32
	AccessType                  uint32
	DirectionType               uint32
	InterfaceAndOperStatusFlags uint8
	OperStatus                  uint32
	AdminStatus                 uint32
	MediaConnectState           uint32
	NetworkGuid                 windows.GUID
	ConnectionType              uint32
	TransmitLinkSpeed           uint64
	ReceiveLinkSpeed            uint64
	InOctets                    uint64
	InUcastPkts                 uint64
	InNUcastPkts                uint64
	InDiscards                  uint64
	InErrors                    uint64
	InUnknownProtos             uint64
	InUcastOctets               uint64
	InMulticastOctets           uint64
	InBroadcastOctets           uint64
	OutOctets                   uint64
	OutUcastPkts                uint64
	OutNUcastPkts               uint64
	OutDiscards                 uint64
	OutErrors                   uint64
	OutUcastOctets              uint64
	OutMulticastOctets          uint64
	OutBroadcastOctets          uint64
	OutQLen                     uint64
}

// mibIfTable2 mirrors Windows MIB_IF_TABLE2 from netioapi.h.
type mibIfTable2 struct {
	NumEntries uint32
	_          uint32 // natural 8-byte alignment padding for Table[0].InterfaceLuid
	Table      [1]mibIfRow2
}

// SpeedSample represents instantaneous upload and download speeds in bytes/second.
type SpeedSample struct {
	DownloadBytesPerSec float64
	UploadBytesPerSec   float64
	ActiveInterfaces    int
	Timestamp           time.Time
}

type interfaceCounters struct {
	luid      uint64
	inOctets  uint64
	outOctets uint64
}

type counterSnapshotFn func() ([]interfaceCounters, error)

// Monitor tracks cumulative interface octets and computes per-second upload/download rates.
type Monitor struct {
	mu          sync.Mutex
	initialized bool
	lastTime    time.Time
	lastByLUID  map[uint64]interfaceCounters
	readFn      counterSnapshotFn
	nowFn       func() time.Time
}

// NewMonitor creates a Monitor backed by Windows GetIfTable2.
func NewMonitor() *Monitor {
	return newMonitorWithDeps(readWindowsCounters, time.Now)
}

func newMonitorWithDeps(readFn counterSnapshotFn, nowFn func() time.Time) *Monitor {
	return &Monitor{
		lastByLUID: make(map[uint64]interfaceCounters),
		readFn:     readFn,
		nowFn:      nowFn,
	}
}

// Reset clears previous interface baselines so the next Sample call starts fresh.
func (m *Monitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initialized = false
	clear(m.lastByLUID)
}

// Sample reads current interface counters and returns bytes/sec since the previous Sample.
func (m *Monitor) Sample() (SpeedSample, error) {
	now := m.nowFn()
	rows, err := m.readFn()
	if err != nil {
		return SpeedSample{Timestamp: now}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	current := make(map[uint64]interfaceCounters, len(rows))
	for _, r := range rows {
		current[r.luid] = r
	}

	if !m.initialized {
		m.lastByLUID = current
		m.lastTime = now
		m.initialized = true
		return SpeedSample{
			ActiveInterfaces: len(current),
			Timestamp:        now,
		}, nil
	}

	elapsed := now.Sub(m.lastTime).Seconds()
	// Guard against system sleep/suspend spikes or clock jumps.
	if elapsed <= 0 || elapsed > 10.0 {
		m.lastByLUID = current
		m.lastTime = now
		return SpeedSample{
			ActiveInterfaces: len(current),
			Timestamp:        now,
		}, nil
	}

	var deltaIn, deltaOut uint64
	for luid, cur := range current {
		prev, ok := m.lastByLUID[luid]
		if !ok {
			continue
		}
		if cur.inOctets >= prev.inOctets {
			deltaIn += cur.inOctets - prev.inOctets
		}
		if cur.outOctets >= prev.outOctets {
			deltaOut += cur.outOctets - prev.outOctets
		}
	}

	m.lastByLUID = current
	m.lastTime = now

	return SpeedSample{
		DownloadBytesPerSec: float64(deltaIn) / elapsed,
		UploadBytesPerSec:   float64(deltaOut) / elapsed,
		ActiveInterfaces:    len(current),
		Timestamp:           now,
	}, nil
}

func isVirtualSwitchOrPseudoAdapter(alias, desc string) bool {
	combined := strings.ToLower(alias + " " + desc)
	for _, needle := range []string{
		"vethernet",
		"hyper-v",
		"wsl",
		"vmware",
		"virtualbox",
		"loopback",
		"npcap",
		"wintun",
		"wireguard",
		"tailscale",
		"zerotier",
		"tap-windows",
		"teredo",
		"isatap",
		"6to4",
		"kernel debug",
	} {
		if strings.Contains(combined, needle) {
			return true
		}
	}
	return false
}

func isCountableInterface(row *mibIfRow2) bool {
	if row.OperStatus != ifOperStatusUp {
		return false
	}
	if row.Type == ifTypeSoftwareLoopback || row.Type == ifTypeTunnel {
		return false
	}
	// Exclude NDIS Light-Weight Filters / WFP filter layers stacked on top of NICs.
	if row.InterfaceAndOperStatusFlags&flagFilterInterface != 0 {
		return false
	}
	// Prefer hardware NICs or connected physical/virtual endpoints to avoid virtual/pseudo bloat.
	if row.InterfaceAndOperStatusFlags&(flagHardwareInterface|flagConnectorPresent) == 0 {
		return false
	}
	// Avoid double-counting internal Hyper-V/WSL/Docker/VM/VPN switches when not hardware NICs.
	if row.InterfaceAndOperStatusFlags&flagHardwareInterface == 0 {
		alias := windows.UTF16ToString(row.Alias[:])
		desc := windows.UTF16ToString(row.Description[:])
		if isVirtualSwitchOrPseudoAdapter(alias, desc) {
			return false
		}
	}
	return true
}

func readWindowsCounters() ([]interfaceCounters, error) {
	var tablePtr *mibIfTable2
	r1, _, _ := procGetIfTable2.Call(uintptr(unsafe.Pointer(&tablePtr)))
	if r1 != 0 {
		return nil, fmt.Errorf("GetIfTable2 failed with status 0x%x", r1)
	}
	if tablePtr == nil {
		return nil, nil
	}
	defer procFreeMibTable.Call(uintptr(unsafe.Pointer(tablePtr)))

	n := int(tablePtr.NumEntries)
	if n == 0 {
		return nil, nil
	}

	rows := unsafe.Slice(&tablePtr.Table[0], n)
	counters := make([]interfaceCounters, 0, n)
	for i := range rows {
		row := &rows[i]
		if !isCountableInterface(row) {
			continue
		}
		counters = append(counters, interfaceCounters{
			luid:      row.InterfaceLuid,
			inOctets:  row.InOctets,
			outOctets: row.OutOctets,
		})
	}
	return counters, nil
}
