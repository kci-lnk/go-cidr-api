package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	ErrProvinceNotFound = errors.New("province not found")
	ErrCityNotFound     = errors.New("city not found")
	ErrInvalidIPVersion = errors.New("invalid ip version")

	autonomousPrefectureSuffixPattern = regexp.MustCompile(`(?:蒙古(?:族)?|回族|藏族|维吾尔(?:族)?|苗族|彝族|壮族|布依族|朝鲜族|满族|侗族|瑶族|白族|土家族|哈尼族|哈萨克(?:族)?|傣族|黎族|傈僳族|佤族|畲族|高山族|拉祜族|水族|东乡族|纳西族|景颇族|柯尔克孜(?:族)?|土族|达斡尔族|仫佬族|羌族|布朗族|撒拉族|毛南族|仡佬族|锡伯族|阿昌族|普米族|塔吉克(?:族)?|怒族|乌孜别克族|俄罗斯族|鄂温克族|德昂族|保安族|裕固族|京族|塔塔尔族|独龙族|鄂伦春族|赫哲族|门巴族|珞巴族|基诺族)+自治州$`)
)

type rawDataset map[string]map[string]map[string][]string

type Store struct {
	data          rawDataset
	provinceNames []string
}

type ProvinceItem struct {
	Name      string `json:"name"`
	CityCount int    `json:"city_count"`
}

type CityItem struct {
	Name      string `json:"name"`
	IPv4Count int    `json:"ipv4_count"`
	IPv6Count int    `json:"ipv6_count"`
}

type CIDRQueryResult struct {
	Province   string              `json:"province"`
	City       string              `json:"city,omitempty"`
	IPVersion  string              `json:"ip_version,omitempty"`
	Count      int                 `json:"count,omitempty"`
	CIDRs      []string            `json:"cidrs,omitempty"`
	CIDRGroups map[string][]string `json:"cidr_groups,omitempty"`
	Counts     map[string]int      `json:"counts,omitempty"`
}

func LoadStore(dataFile string) (*Store, error) {
	resolvedFile, err := resolveDataFile(dataFile)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(resolvedFile)
	if err != nil {
		return nil, fmt.Errorf("read data file %q: %w", resolvedFile, err)
	}

	var data rawDataset
	if err := json.Unmarshal(content, &data); err != nil {
		return nil, fmt.Errorf("decode data file %q: %w", resolvedFile, err)
	}

	provinceNames := make([]string, 0, len(data))
	for province := range data {
		provinceNames = append(provinceNames, province)
	}
	sort.Strings(provinceNames)

	return &Store{
		data:          data,
		provinceNames: provinceNames,
	}, nil
}

func (s *Store) ListProvinces() []ProvinceItem {
	items := make([]ProvinceItem, 0, len(s.provinceNames))
	for _, province := range s.provinceNames {
		items = append(items, ProvinceItem{
			Name:      displayName(province),
			CityCount: len(s.data[province]),
		})
	}
	return items
}

func (s *Store) ListCities(province string) (string, []CityItem, error) {
	resolvedProvince, cities, err := s.lookupProvince(province)
	if err != nil {
		return "", nil, err
	}

	cityNames := make([]string, 0, len(cities))
	for city := range cities {
		cityNames = append(cityNames, city)
	}
	sort.Strings(cityNames)

	items := make([]CityItem, 0, len(cityNames))
	for _, city := range cityNames {
		items = append(items, CityItem{
			Name:      displayName(city),
			IPv4Count: len(cities[city]["4"]),
			IPv6Count: len(cities[city]["6"]),
		})
	}

	return displayName(resolvedProvince), items, nil
}

func (s *Store) GetCIDRs(province, city, ipVersion string) (CIDRQueryResult, error) {
	resolvedProvince, cities, err := s.lookupProvince(province)
	if err != nil {
		return CIDRQueryResult{}, err
	}

	version, err := normalizeIPVersion(ipVersion)
	if err != nil {
		return CIDRQueryResult{}, err
	}

	queryCity := strings.TrimSpace(city)
	if queryCity != "" {
		resolvedCity, cityData, err := lookupCity(cities, queryCity)
		if err != nil {
			return CIDRQueryResult{}, err
		}

		result := CIDRQueryResult{
			Province: displayName(resolvedProvince),
			City:     displayName(resolvedCity),
		}

		if version == "" {
			result.CIDRGroups = map[string][]string{
				"4": cloneCIDRs(cityData["4"]),
				"6": cloneCIDRs(cityData["6"]),
			}
			result.Counts = map[string]int{
				"4": len(cityData["4"]),
				"6": len(cityData["6"]),
			}
			return result, nil
		}

		result.IPVersion = version
		result.CIDRs = cloneCIDRs(cityData[version])
		result.Count = len(result.CIDRs)
		return result, nil
	}

	result := CIDRQueryResult{
		Province: displayName(resolvedProvince),
	}

	if version == "" {
		result.CIDRGroups = map[string][]string{
			"4": aggregateCIDRs(cities, "4"),
			"6": aggregateCIDRs(cities, "6"),
		}
		result.Counts = map[string]int{
			"4": len(result.CIDRGroups["4"]),
			"6": len(result.CIDRGroups["6"]),
		}
		return result, nil
	}

	result.IPVersion = version
	result.CIDRs = aggregateCIDRs(cities, version)
	result.Count = len(result.CIDRs)
	return result, nil
}

func (s *Store) lookupProvince(province string) (string, map[string]map[string][]string, error) {
	query := strings.TrimSpace(province)
	if query == "" {
		return "", nil, ErrProvinceNotFound
	}

	if cities, ok := s.data[query]; ok {
		return query, cities, nil
	}

	normalized := normalizeLookupKey(query)
	for name, cities := range s.data {
		if normalizeLookupKey(name) == normalized {
			return name, cities, nil
		}
	}

	return "", nil, fmt.Errorf("%w: %s", ErrProvinceNotFound, query)
}

func lookupCity(cities map[string]map[string][]string, city string) (string, map[string][]string, error) {
	query := strings.TrimSpace(city)
	if query == "" {
		return "", nil, ErrCityNotFound
	}

	if cityData, ok := cities[query]; ok {
		return query, cityData, nil
	}

	normalized := normalizeLookupKey(query)
	for name, cityData := range cities {
		if normalizeLookupKey(name) == normalized {
			return name, cityData, nil
		}
	}

	return "", nil, fmt.Errorf("%w: %s", ErrCityNotFound, query)
}

func normalizeIPVersion(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all":
		return "", nil
	case "4", "ipv4", "ip4":
		return "4", nil
	case "6", "ipv6", "ip6":
		return "6", nil
	default:
		return "", fmt.Errorf("%w: %s", ErrInvalidIPVersion, value)
	}
}

func normalizeLookupKey(value string) string {
	return displayName(value)
}

func displayName(value string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(value, "　", ""))
	if normalized == "" {
		return ""
	}

	normalized = autonomousPrefectureSuffixPattern.ReplaceAllString(normalized, "")

	suffixes := []string{
		"维吾尔自治区",
		"回族自治区",
		"壮族自治区",
		"特别行政区",
		"自治区",
		"自治州",
		"地区",
		"盟",
		"省",
		"市",
	}

	for _, suffix := range suffixes {
		if strings.HasSuffix(normalized, suffix) {
			return strings.TrimSuffix(normalized, suffix)
		}
	}

	return normalized
}

func aggregateCIDRs(cities map[string]map[string][]string, version string) []string {
	seen := make(map[string]struct{})
	aggregated := make([]string, 0)

	for _, cityData := range cities {
		for _, cidr := range cityData[version] {
			if _, ok := seen[cidr]; ok {
				continue
			}
			seen[cidr] = struct{}{}
			aggregated = append(aggregated, cidr)
		}
	}

	sort.Strings(aggregated)
	return aggregated
}

func cloneCIDRs(cidrs []string) []string {
	cloned := make([]string, len(cidrs))
	copy(cloned, cidrs)
	return cloned
}

func resolveDataFile(dataFile string) (string, error) {
	candidates := []string{dataFile}

	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		candidates = append(candidates, filepath.Join(execDir, dataFile))
		candidates = append(candidates, filepath.Join(execDir, filepath.Base(dataFile)))
	}

	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}

		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("data file %q not found", dataFile)
}
