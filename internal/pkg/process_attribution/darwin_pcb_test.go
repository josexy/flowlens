package processattribution

import (
	"encoding/binary"
	"net/netip"
	"testing"
)

type darwinPCBFixtureRow struct {
	local        netip.AddrPort
	remote       netip.AddrPort
	lastPID      uint32
	effectivePID uint32
}

func TestDarwinPCBLayoutForRelease(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		release  string
		itemSize int
	}{
		{release: "21.6.0", itemSize: 384 + 208},
		{release: "22.0.0", itemSize: 408 + 208},
		{release: "25.5.0", itemSize: 408 + 208},
	} {
		t.Run(test.release, func(t *testing.T) {
			t.Parallel()
			layout, err := darwinPCBLayoutForRelease(test.release)
			if err != nil {
				t.Fatalf("darwinPCBLayoutForRelease(%q): %v", test.release, err)
			}
			if layout.itemSize != test.itemSize {
				t.Fatalf("item size = %d, want %d", layout.itemSize, test.itemSize)
			}
		})
	}

	for _, release := range []string{"", "invalid", "0.0.0"} {
		t.Run("invalid_"+release, func(t *testing.T) {
			t.Parallel()
			if _, err := darwinPCBLayoutForRelease(release); err == nil {
				t.Fatalf("darwinPCBLayoutForRelease(%q) unexpectedly succeeded", release)
			}
		})
	}
}

func TestParseDarwinTCPPCBListMatchesCompleteIPv4Tuple(t *testing.T) {
	t.Parallel()

	layout := mustDarwinPCBLayout(t, "25.5.0")
	client := netip.MustParseAddrPort("127.0.0.1:43120")
	proxy := netip.MustParseAddrPort("127.0.0.1:8080")
	otherProxy := netip.MustParseAddrPort("127.0.0.1:9090")
	data := makeDarwinTCPPCBFixture(t, layout,
		darwinPCBFixtureRow{
			local:        client,
			remote:       proxy,
			lastPID:      4101,
			effectivePID: 4102,
		},
		darwinPCBFixtureRow{
			local:        client,
			remote:       otherProxy,
			lastPID:      4201,
			effectivePID: 4202,
		},
	)

	rows, err := parseDarwinTCPPCBList(data, layout)
	if err != nil {
		t.Fatalf("parseDarwinTCPPCBList: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("row count = %d, want 2", len(rows))
	}
	if rows[0].local != client || rows[0].remote != proxy || rows[0].pid != 4102 {
		t.Fatalf("first row = %+v", rows[0])
	}
	if rows[1].local != client || rows[1].remote != otherProxy || rows[1].pid != 4202 {
		t.Fatalf("second row = %+v", rows[1])
	}

	pids := darwinTupleOwnerPIDs(rows, EndpointTuple{Client: client, Proxy: proxy})
	if len(pids) != 1 || pids[0] != 4102 {
		t.Fatalf("matching PIDs = %v, want [4102]", pids)
	}
}

func TestParseDarwinTCPPCBListIPv6FallsBackToLastPID(t *testing.T) {
	t.Parallel()

	layout := mustDarwinPCBLayout(t, "21.6.0")
	client := netip.MustParseAddrPort("[::1]:43121")
	proxy := netip.MustParseAddrPort("[::1]:8080")
	data := makeDarwinTCPPCBFixture(t, layout, darwinPCBFixtureRow{
		local:   client,
		remote:  proxy,
		lastPID: 5151,
	})

	rows, err := parseDarwinTCPPCBList(data, layout)
	if err != nil {
		t.Fatalf("parseDarwinTCPPCBList: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	if rows[0].local != client || rows[0].remote != proxy || rows[0].pid != 5151 {
		t.Fatalf("IPv6 row = %+v", rows[0])
	}
}

func TestParseDarwinTCPPCBListAcceptsEmptySnapshot(t *testing.T) {
	t.Parallel()

	layout := mustDarwinPCBLayout(t, "25.5.0")
	data := make([]byte, darwinXinpgenSize)
	binary.NativeEndian.PutUint32(data[0:4], darwinXinpgenSize)

	rows, err := parseDarwinTCPPCBList(data, layout)
	if err != nil {
		t.Fatalf("parseDarwinTCPPCBList: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("empty snapshot rows = %+v", rows)
	}
}

func TestParseDarwinTCPPCBListRejectsMalformedStructure(t *testing.T) {
	t.Parallel()

	layout := mustDarwinPCBLayout(t, "25.5.0")
	row := darwinPCBFixtureRow{
		local:        netip.MustParseAddrPort("127.0.0.1:43122"),
		remote:       netip.MustParseAddrPort("127.0.0.1:8080"),
		effectivePID: 6161,
	}
	valid := makeDarwinTCPPCBFixture(t, layout, row)
	recordOffset := darwinXinpgenSize
	socketOffset := recordOffset + darwinXsocketOffset

	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{
			name: "truncated_xinpgen",
			mutate: func(data []byte) []byte {
				return data[:darwinXinpgenSize-1]
			},
		},
		{
			name: "missing_trailing_xinpgen",
			mutate: func(data []byte) []byte {
				return data[:len(data)-darwinXinpgenSize]
			},
		},
		{
			name: "record_size_remainder",
			mutate: func(data []byte) []byte {
				result := append([]byte(nil), data[:len(data)-darwinXinpgenSize]...)
				result = append(result, 0)
				return append(result, data[len(data)-darwinXinpgenSize:]...)
			},
		},
		{
			name: "unexpected_xinpcb_length",
			mutate: func(data []byte) []byte {
				binary.NativeEndian.PutUint32(data[recordOffset:recordOffset+4], darwinXinpcbSize-8)
				return data
			},
		},
		{
			name: "unexpected_xinpcb_kind",
			mutate: func(data []byte) []byte {
				binary.NativeEndian.PutUint32(data[recordOffset+4:recordOffset+8], 0)
				return data
			},
		},
		{
			name: "truncated_xsocket",
			mutate: func(data []byte) []byte {
				binary.NativeEndian.PutUint32(data[socketOffset:socketOffset+4], darwinXsocketPIDEnd-1)
				return data
			},
		},
		{
			name: "unexpected_xsocket_kind",
			mutate: func(data []byte) []byte {
				binary.NativeEndian.PutUint32(data[socketOffset+4:socketOffset+8], 0)
				return data
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := test.mutate(append([]byte(nil), valid...))
			if _, err := parseDarwinTCPPCBList(data, layout); err == nil {
				t.Fatal("parseDarwinTCPPCBList unexpectedly succeeded")
			}
		})
	}
}

func TestParseDarwinTCPPCBListSkipsUnsupportedAddressFamily(t *testing.T) {
	t.Parallel()

	layout := mustDarwinPCBLayout(t, "25.5.0")
	data := makeDarwinTCPPCBFixture(t, layout, darwinPCBFixtureRow{
		local:        netip.MustParseAddrPort("127.0.0.1:43123"),
		remote:       netip.MustParseAddrPort("127.0.0.1:8080"),
		effectivePID: 7171,
	})
	data[darwinXinpgenSize+darwinPCBAddressFlagOffset] = 0

	rows, err := parseDarwinTCPPCBList(data, layout)
	if err != nil {
		t.Fatalf("parseDarwinTCPPCBList: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("unsupported-family rows = %+v", rows)
	}
}

func mustDarwinPCBLayout(t *testing.T, release string) darwinPCBLayout {
	t.Helper()
	layout, err := darwinPCBLayoutForRelease(release)
	if err != nil {
		t.Fatalf("darwinPCBLayoutForRelease(%q): %v", release, err)
	}
	return layout
}

func makeDarwinTCPPCBFixture(
	t *testing.T,
	layout darwinPCBLayout,
	rows ...darwinPCBFixtureRow,
) []byte {
	t.Helper()
	data := make([]byte, darwinXinpgenSize+len(rows)*layout.itemSize+darwinXinpgenSize)
	binary.NativeEndian.PutUint32(data[0:4], darwinXinpgenSize)
	binary.NativeEndian.PutUint32(data[4:8], uint32(len(rows)))

	for index, row := range rows {
		recordStart := darwinXinpgenSize + index*layout.itemSize
		record := data[recordStart : recordStart+layout.itemSize]
		binary.NativeEndian.PutUint32(record[0:4], darwinXinpcbSize)
		binary.NativeEndian.PutUint32(record[4:8], darwinXsoInpcbKind)
		binary.BigEndian.PutUint16(record[darwinPCBForeignPortOffset:darwinPCBForeignPortOffset+2], row.remote.Port())
		binary.BigEndian.PutUint16(record[darwinPCBLocalPortOffset:darwinPCBLocalPortOffset+2], row.local.Port())

		if row.local.Addr().Is4() && row.remote.Addr().Is4() {
			record[darwinPCBAddressFlagOffset] = darwinIPv4Flag
			local := row.local.Addr().As4()
			remote := row.remote.Addr().As4()
			copy(record[darwinPCBForeignIPv4Offset:darwinPCBForeignIPv4Offset+4], remote[:])
			copy(record[darwinPCBLocalIPv4Offset:darwinPCBLocalIPv4Offset+4], local[:])
		} else if row.local.Addr().Is6() && row.remote.Addr().Is6() {
			record[darwinPCBAddressFlagOffset] = darwinIPv6Flag
			local := row.local.Addr().As16()
			remote := row.remote.Addr().As16()
			copy(record[darwinPCBForeignIPv6Offset:darwinPCBForeignIPv6Offset+16], remote[:])
			copy(record[darwinPCBLocalIPv6Offset:darwinPCBLocalIPv6Offset+16], local[:])
		} else {
			t.Fatalf("fixture address families differ: %s -> %s", row.local, row.remote)
		}

		socket := record[darwinXsocketOffset:]
		binary.NativeEndian.PutUint32(socket[0:4], darwinXsocketSize)
		binary.NativeEndian.PutUint32(socket[4:8], darwinXsoSocketKind)
		binary.NativeEndian.PutUint32(
			socket[darwinXsocketLastPIDOffset:darwinXsocketLastPIDOffset+4],
			row.lastPID,
		)
		binary.NativeEndian.PutUint32(
			socket[darwinXsocketEffectivePIDOffset:darwinXsocketEffectivePIDOffset+4],
			row.effectivePID,
		)
	}

	trailer := data[len(data)-darwinXinpgenSize:]
	binary.NativeEndian.PutUint32(trailer[0:4], darwinXinpgenSize)
	binary.NativeEndian.PutUint32(trailer[4:8], uint32(len(rows)))
	return data
}
