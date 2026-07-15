package cidrgen

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go-cidr-api/internal/cidrdata"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

type Options struct {
	IPv4File string
	IPv6File string
	Output   string
}

type Summary struct {
	Provinces int
	Cities    int
	IPv4CIDRs int
	IPv6CIDRs int
}

type location struct {
	province string
	city     string
}

type parsedRegion struct {
	province string
	city     string
	isp      string
}

type dataKey struct {
	pointer uint32
	length  uint16
}

type segment struct {
	start  ipValue
	end    ipValue
	region parsedRegion
}

type runTracker struct {
	active bool
	key    location
	start  ipValue
	end    ipValue
	flush  func(location, ipValue, ipValue)
}

func (t *runTracker) advance(key *location, start, end ipValue) {
	if t.active && key != nil && t.key == *key {
		if next, ok := t.end.next(); ok && next.compare(start) == 0 {
			t.end = end
			return
		}
	}
	t.finish()
	if key != nil {
		t.active = true
		t.key = *key
		t.start = start
		t.end = end
	}
}

func (t *runTracker) finish() {
	if !t.active {
		return
	}
	t.flush(t.key, t.start, t.end)
	t.active = false
}

type builder struct {
	dataset cidrdata.Dataset
	version string
}

func newBuilder(version string, dataset cidrdata.Dataset) *builder {
	return &builder{dataset: dataset, version: version}
}

func (b *builder) cityData(key location) *cidrdata.CityData {
	cities := b.dataset[key.province]
	if cities == nil {
		cities = make(map[string]*cidrdata.CityData)
		b.dataset[key.province] = cities
	}
	data := cities[key.city]
	if data == nil {
		data = &cidrdata.CityData{}
		cities[key.city] = data
	}
	return data
}

func (b *builder) appendCity(key location, start, end ipValue) {
	b.cityData(key).VersionData.Append(b.version, rangeToPrefixes(start, end)...)
}

func (b *builder) appendOperator(operator string) func(location, ipValue, ipValue) {
	return func(key location, start, end ipValue) {
		data := b.cityData(key)
		if data.Operators == nil {
			data.Operators = make(map[string]*cidrdata.VersionData)
		}
		versionData := data.Operators[operator]
		if versionData == nil {
			versionData = &cidrdata.VersionData{}
			data.Operators[operator] = versionData
		}
		versionData.Append(b.version, rangeToPrefixes(start, end)...)
	}
}

func Generate(options Options) (cidrdata.Dataset, Summary, error) {
	if strings.TrimSpace(options.IPv4File) == "" || strings.TrimSpace(options.IPv6File) == "" {
		return nil, Summary{}, fmt.Errorf("both IPv4 and IPv6 XDB files are required")
	}

	dataset := make(cidrdata.Dataset)
	for _, input := range []struct {
		path    string
		version string
	}{
		{path: options.IPv4File, version: "4"},
		{path: options.IPv6File, version: "6"},
	} {
		if err := scanXDB(input.path, input.version, dataset); err != nil {
			return nil, Summary{}, err
		}
	}

	if err := cidrdata.Validate(dataset); err != nil {
		return nil, Summary{}, fmt.Errorf("validate generated dataset: %w", err)
	}
	summary := summarize(dataset)
	if strings.TrimSpace(options.Output) != "" {
		if err := writeAtomic(options.Output, dataset); err != nil {
			return nil, Summary{}, err
		}
	}
	return dataset, summary, nil
}

func scanXDB(path, expectedVersion string, dataset cidrdata.Dataset) error {
	if err := xdb.VerifyFromFile(path); err != nil {
		return fmt.Errorf("verify XDB %q: %w", path, err)
	}
	header, err := xdb.LoadHeaderFromFile(path)
	if err != nil {
		return fmt.Errorf("load XDB header %q: %w", path, err)
	}
	version, err := xdb.VersionFromHeader(header)
	if err != nil {
		return fmt.Errorf("resolve XDB version %q: %w", path, err)
	}
	if fmt.Sprint(version.Id) != expectedVersion {
		return fmt.Errorf("XDB %q is IPv%d, expected IPv%s", path, version.Id, expectedVersion)
	}
	if header.EndIndexPtr < header.StartIndexPtr {
		return fmt.Errorf("XDB %q has invalid index pointers", path)
	}
	span := uint64(header.EndIndexPtr-header.StartIndexPtr) + uint64(version.SegmentIndexSize)
	if span%uint64(version.SegmentIndexSize) != 0 {
		return fmt.Errorf("XDB %q has an unaligned segment index", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open XDB %q: %w", path, err)
	}
	defer file.Close()

	b := newBuilder(expectedVersion, dataset)
	national := location{province: cidrdata.NationalProvince, city: cidrdata.NationalCity}
	cityTracker := runTracker{flush: b.appendCity}
	nationalTracker := runTracker{flush: b.appendCity}
	cityOperatorTrackers := make(map[string]*runTracker, len(cidrdata.Operators))
	nationalOperatorTrackers := make(map[string]*runTracker, len(cidrdata.Operators))
	for _, operator := range cidrdata.Operators {
		cityOperatorTrackers[operator] = &runTracker{flush: b.appendOperator(operator)}
		nationalOperatorTrackers[operator] = &runTracker{flush: b.appendOperator(operator)}
	}

	cache := make(map[dataKey]parsedRegion)
	section := io.NewSectionReader(file, int64(header.StartIndexPtr), int64(span))
	reader := bufio.NewReaderSize(section, 1024*1024)
	record := make([]byte, version.SegmentIndexSize)
	for {
		_, err := io.ReadFull(reader, record)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read XDB segment from %q: %w", path, err)
		}
		seg, err := decodeSegment(file, record, version.Id, cache)
		if err != nil {
			return fmt.Errorf("decode XDB segment from %q: %w", path, err)
		}

		province, mainland := cidrdata.CanonicalProvince(seg.region.province)
		if !mainland {
			cityTracker.advance(nil, seg.start, seg.end)
			nationalTracker.advance(nil, seg.start, seg.end)
			for _, operator := range cidrdata.Operators {
				cityOperatorTrackers[operator].advance(nil, seg.start, seg.end)
				nationalOperatorTrackers[operator].advance(nil, seg.start, seg.end)
			}
			continue
		}

		nationalTracker.advance(&national, seg.start, seg.end)
		cityName := cidrdata.CanonicalCity(province, seg.region.city)
		var city *location
		if cityName != "" {
			value := location{province: province, city: cityName}
			city = &value
		}
		cityTracker.advance(city, seg.start, seg.end)

		matched := make(map[string]struct{}, len(cidrdata.Operators))
		for _, operator := range cidrdata.OperatorsForISP(seg.region.isp) {
			matched[operator] = struct{}{}
		}
		for _, operator := range cidrdata.Operators {
			if _, ok := matched[operator]; ok {
				nationalOperatorTrackers[operator].advance(&national, seg.start, seg.end)
				cityOperatorTrackers[operator].advance(city, seg.start, seg.end)
			} else {
				nationalOperatorTrackers[operator].advance(nil, seg.start, seg.end)
				cityOperatorTrackers[operator].advance(nil, seg.start, seg.end)
			}
		}
	}

	cityTracker.finish()
	nationalTracker.finish()
	for _, operator := range cidrdata.Operators {
		cityOperatorTrackers[operator].finish()
		nationalOperatorTrackers[operator].finish()
	}
	return nil
}

func decodeSegment(file *os.File, record []byte, version int, cache map[dataKey]parsedRegion) (segment, error) {
	var result segment
	var length uint16
	var pointer uint32
	if version == 4 {
		result.start = ipv4Value(record[:4])
		result.end = ipv4Value(record[4:8])
		length = binary.LittleEndian.Uint16(record[8:10])
		pointer = binary.LittleEndian.Uint32(record[10:14])
	} else {
		result.start = ipv6Value(record[:16])
		result.end = ipv6Value(record[16:32])
		length = binary.LittleEndian.Uint16(record[32:34])
		pointer = binary.LittleEndian.Uint32(record[34:38])
	}
	if result.start.compare(result.end) > 0 {
		return segment{}, fmt.Errorf("segment start is after end")
	}

	key := dataKey{pointer: pointer, length: length}
	if region, ok := cache[key]; ok {
		result.region = region
		return result, nil
	}
	if length == 0 || pointer == 0 {
		cache[key] = parsedRegion{}
		return result, nil
	}
	raw := make([]byte, length)
	if _, err := file.ReadAt(raw, int64(pointer)); err != nil {
		return segment{}, fmt.Errorf("read region at %d: %w", pointer, err)
	}
	parts := strings.Split(string(raw), "|")
	region := parsedRegion{province: field(parts, 2), city: field(parts, 3), isp: field(parts, 5)}
	cache[key] = region
	result.region = region
	return result, nil
}

func field(parts []string, index int) string {
	if index >= len(parts) || parts[index] == "0" {
		return ""
	}
	return strings.TrimSpace(parts[index])
}

func writeAtomic(output string, dataset cidrdata.Dataset) error {
	directory := filepath.Dir(output)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".cidr-data-*.json")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)

	encoder := json.NewEncoder(temporary)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(dataset); err != nil {
		temporary.Close()
		return fmt.Errorf("encode generated dataset: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync generated dataset: %w", err)
	}
	if err := temporary.Chmod(0644); err != nil {
		temporary.Close()
		return fmt.Errorf("set generated dataset permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close generated dataset: %w", err)
	}
	if err := os.Rename(temporaryName, output); err != nil {
		return fmt.Errorf("replace output %q: %w", output, err)
	}
	return nil
}

func summarize(dataset cidrdata.Dataset) Summary {
	summary := Summary{Provinces: len(dataset)}
	for _, cities := range dataset {
		summary.Cities += len(cities)
		for _, data := range cities {
			summary.IPv4CIDRs += len(data.IPv4)
			summary.IPv6CIDRs += len(data.IPv6)
			for _, operatorData := range data.Operators {
				summary.IPv4CIDRs += len(operatorData.IPv4)
				summary.IPv6CIDRs += len(operatorData.IPv6)
			}
		}
	}
	return summary
}
