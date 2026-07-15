package cidrgen

import (
	"encoding/binary"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go-cidr-api/internal/cidrdata"
)

type fixtureSegment struct {
	start  string
	end    string
	region string
}

func TestGenerateFromSyntheticXDB(t *testing.T) {
	directory := t.TempDir()
	v4 := filepath.Join(directory, "v4.xdb")
	v6 := filepath.Join(directory, "v6.xdb")
	writeFixtureXDB(t, v4, 4, []fixtureSegment{
		{"1.0.0.0", "1.0.0.3", "亚洲|中国|河北|石家庄||移动"},
		{"1.0.0.4", "1.0.0.7", "亚洲|中国|河北|石家庄||鹏博士/联通"},
		{"1.0.0.8", "1.0.0.9", "亚洲|中国|河北|石家庄||中国电信/CN2"},
		{"1.0.0.10", "1.0.0.11", "亚洲|中国|河北|||电信"},
		{"1.0.0.12", "1.0.0.15", "亚洲|中国|台湾|台北||移动"},
	})
	writeFixtureXDB(t, v6, 6, []fixtureSegment{
		{"2001:db8::", "2001:db8::3", "亚洲|中国|上海|上海||电信"},
	})

	dataset, _, err := Generate(Options{IPv4File: v4, IPv6File: v6})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := dataset["台湾省"]; ok {
		t.Fatal("Taiwan must not be present")
	}
	city := dataset["河北省"]["石家庄"]
	if got, want := city.IPv4, []string{"1.0.0.0/29", "1.0.0.8/31"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("city IPv4 = %#v, want %#v", got, want)
	}
	if got, want := city.Operators["移动"].IPv4, []string{"1.0.0.0/30"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mobile IPv4 = %#v, want %#v", got, want)
	}
	if got, want := city.Operators["联通"].IPv4, []string{"1.0.0.4/30"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unicom IPv4 = %#v, want %#v", got, want)
	}
	if got, want := city.Operators["电信"].IPv4, []string{"1.0.0.8/31"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("telecom IPv4 = %#v, want %#v", got, want)
	}
	national := dataset[cidrdata.NationalProvince][cidrdata.NationalCity]
	if got, want := national.IPv4, []string{"1.0.0.0/29", "1.0.0.8/30"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("national IPv4 = %#v, want %#v", got, want)
	}
	if got, want := national.Operators["电信"].IPv4, []string{"1.0.0.8/30"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("national telecom IPv4 = %#v, want %#v", got, want)
	}
	if got, want := dataset["上海市"]["上海市"].IPv6, []string{"2001:db8::/126"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Shanghai IPv6 = %#v, want %#v", got, want)
	}
}

func TestGenerateRejectsMalformedXDB(t *testing.T) {
	directory := t.TempDir()
	bad := filepath.Join(directory, "bad.xdb")
	if err := os.WriteFile(bad, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err := Generate(Options{IPv4File: bad, IPv6File: bad})
	if err == nil {
		t.Fatal("Generate() unexpectedly accepted a malformed XDB")
	}
}

func writeFixtureXDB(t *testing.T, path string, version int, segments []fixtureSegment) {
	t.Helper()
	content := make([]byte, 256)
	pointers := make([]dataKey, len(segments))
	for index, segment := range segments {
		pointers[index] = dataKey{pointer: uint32(len(content)), length: uint16(len(segment.region))}
		content = append(content, segment.region...)
	}
	recordSize := 14
	if version == 6 {
		recordSize = 38
	}
	startIndex := len(content)
	for index, segment := range segments {
		record := make([]byte, recordSize)
		start := netip.MustParseAddr(segment.start)
		end := netip.MustParseAddr(segment.end)
		if version == 4 {
			start4, end4 := start.As4(), end.As4()
			reverse4(record[:4], start4)
			reverse4(record[4:8], end4)
			binary.LittleEndian.PutUint16(record[8:10], pointers[index].length)
			binary.LittleEndian.PutUint32(record[10:14], pointers[index].pointer)
		} else {
			start16, end16 := start.As16(), end.As16()
			copy(record[:16], start16[:])
			copy(record[16:32], end16[:])
			binary.LittleEndian.PutUint16(record[32:34], pointers[index].length)
			binary.LittleEndian.PutUint32(record[34:38], pointers[index].pointer)
		}
		content = append(content, record...)
	}
	binary.LittleEndian.PutUint16(content[0:2], 3)
	binary.LittleEndian.PutUint16(content[2:4], 1)
	binary.LittleEndian.PutUint32(content[8:12], uint32(startIndex))
	binary.LittleEndian.PutUint32(content[12:16], uint32(startIndex+(len(segments)-1)*recordSize))
	binary.LittleEndian.PutUint16(content[16:18], uint16(version))
	binary.LittleEndian.PutUint16(content[18:20], 4)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
}

func reverse4(destination []byte, source [4]byte) {
	destination[0] = source[3]
	destination[1] = source[2]
	destination[2] = source[1]
	destination[3] = source[0]
}
