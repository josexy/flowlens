package processattribution

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

const (
	// XNU's pcblist_n format is a leading xinpgen, fixed-size aligned
	// protocol records, and (when records are present) a trailing xinpgen.
	darwinXinpgenSize         = 24
	darwinXinpcbSize          = 104
	darwinXsocketOffset       = 104
	darwinXsocketSize         = 104
	darwinTCPControlBlockSize = 208

	darwinPCBBaseSizeBefore22 = 384
	darwinPCBBaseSizeFrom22   = 408

	darwinXsoInpcbKind  = 0x010
	darwinXsoSocketKind = 0x001

	darwinIPv4Flag = 0x1
	darwinIPv6Flag = 0x2

	darwinPCBForeignPortOffset = 16
	darwinPCBLocalPortOffset   = 18
	darwinPCBAddressFlagOffset = 44
	darwinPCBForeignIPv6Offset = 48
	darwinPCBForeignIPv4Offset = 60
	darwinPCBLocalIPv6Offset   = 64
	darwinPCBLocalIPv4Offset   = 76

	darwinXsocketLastPIDOffset      = 68
	darwinXsocketEffectivePIDOffset = 72
	darwinXsocketPIDEnd             = darwinXsocketEffectivePIDOffset + 4
)

type darwinPCBLayout struct {
	itemSize int
}

type darwinSocketOwnerRow struct {
	local  netip.AddrPort
	remote netip.AddrPort
	pid    uint32
}

func darwinPCBLayoutForRelease(release string) (darwinPCBLayout, error) {
	majorValue, _, _ := strings.Cut(strings.TrimSpace(release), ".")
	major, err := strconv.ParseUint(majorValue, 10, 32)
	if err != nil || major == 0 {
		return darwinPCBLayout{}, fmt.Errorf("parse Darwin kernel release %q", release)
	}
	baseSize := darwinPCBBaseSizeBefore22
	if major >= 22 {
		baseSize = darwinPCBBaseSizeFrom22
	}
	return darwinPCBLayout{itemSize: baseSize + darwinTCPControlBlockSize}, nil
}

func parseDarwinTCPPCBList(
	data []byte,
	layout darwinPCBLayout,
) ([]darwinSocketOwnerRow, error) {
	if layout.itemSize < darwinXsocketOffset+darwinXsocketPIDEnd {
		return nil, errors.New("Darwin PCB item size is too small")
	}
	if len(data) < darwinXinpgenSize {
		return nil, errors.New("Darwin PCB list is missing its leading xinpgen")
	}
	if err := validateDarwinXinpgen(data[:darwinXinpgenSize]); err != nil {
		return nil, fmt.Errorf("Darwin PCB leading xinpgen: %w", err)
	}

	announcedCount := binary.NativeEndian.Uint32(data[4:8])
	if len(data) == darwinXinpgenSize {
		if announcedCount != 0 {
			return nil, errors.New("Darwin PCB list announced rows without record data")
		}
		return nil, nil
	}
	if len(data) < 2*darwinXinpgenSize {
		return nil, errors.New("Darwin PCB list is missing its trailing xinpgen")
	}

	trailerOffset := len(data) - darwinXinpgenSize
	if err := validateDarwinXinpgen(data[trailerOffset:]); err != nil {
		return nil, fmt.Errorf("Darwin PCB trailing xinpgen: %w", err)
	}
	bodySize := trailerOffset - darwinXinpgenSize
	if bodySize%layout.itemSize != 0 {
		return nil, fmt.Errorf(
			"Darwin PCB record bytes %d are not divisible by item size %d",
			bodySize,
			layout.itemSize,
		)
	}

	recordCount := bodySize / layout.itemSize
	rows := make([]darwinSocketOwnerRow, 0, recordCount)
	for index := range recordCount {
		recordOffset := darwinXinpgenSize + index*layout.itemSize
		record := data[recordOffset : recordOffset+layout.itemSize]
		row, include, err := parseDarwinTCPPCBRecord(record)
		if err != nil {
			return nil, fmt.Errorf("Darwin PCB record %d: %w", index, err)
		}
		if include {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func validateDarwinXinpgen(data []byte) error {
	if len(data) < darwinXinpgenSize {
		return errors.New("is truncated")
	}
	if length := binary.NativeEndian.Uint32(data[0:4]); length != darwinXinpgenSize {
		return fmt.Errorf("length = %d, want %d", length, darwinXinpgenSize)
	}
	return nil
}

func parseDarwinTCPPCBRecord(
	record []byte,
) (darwinSocketOwnerRow, bool, error) {
	if len(record) < darwinXsocketOffset+darwinXsocketPIDEnd {
		return darwinSocketOwnerRow{}, false, errors.New("is truncated before socket owner fields")
	}
	if length := binary.NativeEndian.Uint32(record[0:4]); length != darwinXinpcbSize {
		return darwinSocketOwnerRow{}, false, fmt.Errorf(
			"xinpcb_n length = %d, want %d",
			length,
			darwinXinpcbSize,
		)
	}
	if kind := binary.NativeEndian.Uint32(record[4:8]); kind != darwinXsoInpcbKind {
		return darwinSocketOwnerRow{}, false, fmt.Errorf(
			"xinpcb_n kind = %#x, want %#x",
			kind,
			darwinXsoInpcbKind,
		)
	}

	socket := record[darwinXsocketOffset:]
	socketLength := binary.NativeEndian.Uint32(socket[0:4])
	if socketLength != darwinXsocketSize {
		return darwinSocketOwnerRow{}, false, fmt.Errorf(
			"xsocket_n length = %d, want %d",
			socketLength,
			darwinXsocketSize,
		)
	}
	if kind := binary.NativeEndian.Uint32(socket[4:8]); kind != darwinXsoSocketKind {
		return darwinSocketOwnerRow{}, false, fmt.Errorf(
			"xsocket_n kind = %#x, want %#x",
			kind,
			darwinXsoSocketKind,
		)
	}

	localAddress, remoteAddress, ok := darwinPCBAddresses(record)
	if !ok {
		return darwinSocketOwnerRow{}, false, nil
	}
	localPort := binary.BigEndian.Uint16(
		record[darwinPCBLocalPortOffset : darwinPCBLocalPortOffset+2],
	)
	remotePort := binary.BigEndian.Uint16(
		record[darwinPCBForeignPortOffset : darwinPCBForeignPortOffset+2],
	)
	if localPort == 0 || remotePort == 0 {
		return darwinSocketOwnerRow{}, false, nil
	}

	effectivePID := validDarwinPID(binary.NativeEndian.Uint32(
		socket[darwinXsocketEffectivePIDOffset : darwinXsocketEffectivePIDOffset+4],
	))
	lastPID := validDarwinPID(binary.NativeEndian.Uint32(
		socket[darwinXsocketLastPIDOffset : darwinXsocketLastPIDOffset+4],
	))
	pid := effectivePID
	if pid == 0 {
		pid = lastPID
	}
	if pid == 0 {
		return darwinSocketOwnerRow{}, false, nil
	}

	return darwinSocketOwnerRow{
		local:  netip.AddrPortFrom(localAddress, localPort),
		remote: netip.AddrPortFrom(remoteAddress, remotePort),
		pid:    pid,
	}, true, nil
}

func darwinPCBAddresses(record []byte) (netip.Addr, netip.Addr, bool) {
	flags := record[darwinPCBAddressFlagOffset]
	switch {
	case flags&darwinIPv4Flag != 0:
		localBytes := [4]byte(record[darwinPCBLocalIPv4Offset : darwinPCBLocalIPv4Offset+4])
		remoteBytes := [4]byte(record[darwinPCBForeignIPv4Offset : darwinPCBForeignIPv4Offset+4])
		local := netip.AddrFrom4(localBytes)
		remote := netip.AddrFrom4(remoteBytes)
		return local, remote, true
	case flags&darwinIPv6Flag != 0:
		localBytes := [16]byte(record[darwinPCBLocalIPv6Offset : darwinPCBLocalIPv6Offset+16])
		remoteBytes := [16]byte(record[darwinPCBForeignIPv6Offset : darwinPCBForeignIPv6Offset+16])
		local := netip.AddrFrom16(localBytes).Unmap()
		remote := netip.AddrFrom16(remoteBytes).Unmap()
		return local, remote, true
	default:
		return netip.Addr{}, netip.Addr{}, false
	}
}

func validDarwinPID(value uint32) uint32 {
	if value == 0 || value > math.MaxInt32 {
		return 0
	}
	return value
}

func darwinTupleOwnerPIDs(
	rows []darwinSocketOwnerRow,
	tuple EndpointTuple,
) []uint32 {
	tuple = normalizeEndpointTuple(tuple)
	seen := make(map[uint32]struct{})
	for _, row := range rows {
		if normalizeAddrPort(row.local) == tuple.Client &&
			normalizeAddrPort(row.remote) == tuple.Proxy {
			seen[row.pid] = struct{}{}
		}
	}
	pids := make([]uint32, 0, len(seen))
	for pid := range seen {
		pids = append(pids, pid)
	}
	slices.Sort(pids)
	return pids
}
