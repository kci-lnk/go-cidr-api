package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go-cidr-api/internal/cidrdata"
)

var (
	ErrProvinceNotFound  = errors.New("province not found")
	ErrCityNotFound      = errors.New("city not found")
	ErrInvalidIPVersion  = errors.New("invalid ip version")
	ErrInvalidOperator   = errors.New("invalid operator")
	ErrSelectorConflict  = errors.New("selector conflicts with location parameters")
	ErrSelectorAmbiguous = errors.New("selector is ambiguous")
)

type rawDataset = cidrdata.Dataset

type cityReference struct {
	province string
	city     string
}

type Store struct {
	data          rawDataset
	provinceNames []string
	cityIndex     map[string][]cityReference
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
	Operator   string              `json:"operator,omitempty"`
	IPVersion  string              `json:"ip_version,omitempty"`
	Count      int                 `json:"count,omitempty"`
	CIDRs      []string            `json:"cidrs,omitempty"`
	CIDRGroups map[string][]string `json:"cidr_groups,omitempty"`
	Counts     map[string]int      `json:"counts,omitempty"`
}

func (r CIDRQueryResult) MarshalJSON() ([]byte, error) {
	type resultAlias CIDRQueryResult
	if r.IPVersion == "" {
		return json.Marshal(resultAlias(r))
	}
	type versionResult struct {
		Province  string   `json:"province"`
		City      string   `json:"city,omitempty"`
		Operator  string   `json:"operator,omitempty"`
		IPVersion string   `json:"ip_version"`
		Count     int      `json:"count"`
		CIDRs     []string `json:"cidrs"`
	}
	return json.Marshal(versionResult{
		Province:  r.Province,
		City:      r.City,
		Operator:  r.Operator,
		IPVersion: r.IPVersion,
		Count:     r.Count,
		CIDRs:     r.CIDRs,
	})
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
	cityIndex := make(map[string][]cityReference)
	for province := range data {
		provinceNames = append(provinceNames, province)
		for city := range data[province] {
			key := normalizeLookupKey(city)
			cityIndex[key] = append(cityIndex[key], cityReference{province: province, city: city})
		}
	}
	sort.Strings(provinceNames)
	for key := range cityIndex {
		sort.Slice(cityIndex[key], func(i, j int) bool {
			if cityIndex[key][i].province == cityIndex[key][j].province {
				return cityIndex[key][i].city < cityIndex[key][j].city
			}
			return cityIndex[key][i].province < cityIndex[key][j].province
		})
	}

	return &Store{
		data:          data,
		provinceNames: provinceNames,
		cityIndex:     cityIndex,
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
		cityData := cities[city]
		items = append(items, CityItem{
			Name:      displayName(city),
			IPv4Count: len(cityData.IPv4),
			IPv6Count: len(cityData.IPv6),
		})
	}

	return displayName(resolvedProvince), items, nil
}

func (s *Store) GetCIDRs(province, city, operator, ipVersion string) (CIDRQueryResult, error) {
	resolvedProvince, cities, err := s.lookupProvince(province)
	if err != nil {
		return CIDRQueryResult{}, err
	}

	version, err := normalizeIPVersion(ipVersion)
	if err != nil {
		return CIDRQueryResult{}, err
	}

	normalizedOperator, err := normalizeOperator(operator)
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
			Operator: normalizedOperator,
		}
		versionData := cityVersionData(cityData, normalizedOperator)

		if version == "" {
			result.CIDRGroups = map[string][]string{
				"4": cloneCIDRs(versionData.IPv4),
				"6": cloneCIDRs(versionData.IPv6),
			}
			result.Counts = map[string]int{
				"4": len(versionData.IPv4),
				"6": len(versionData.IPv6),
			}
			return result, nil
		}

		result.IPVersion = version
		result.CIDRs = cloneCIDRs(versionData.CIDRs(version))
		result.Count = len(result.CIDRs)
		return result, nil
	}

	result := CIDRQueryResult{
		Province: displayName(resolvedProvince),
		Operator: normalizedOperator,
	}

	if version == "" {
		result.CIDRGroups = map[string][]string{
			"4": aggregateCIDRs(cities, "4", normalizedOperator),
			"6": aggregateCIDRs(cities, "6", normalizedOperator),
		}
		result.Counts = map[string]int{
			"4": len(result.CIDRGroups["4"]),
			"6": len(result.CIDRGroups["6"]),
		}
		return result, nil
	}

	result.IPVersion = version
	result.CIDRs = aggregateCIDRs(cities, version, normalizedOperator)
	result.Count = len(result.CIDRs)
	return result, nil
}

func (s *Store) ResolveSelector(selector string) (string, string, string, error) {
	query := strings.TrimSpace(selector)
	if query == "" {
		return "", "", "", fmt.Errorf("%w: empty selector", ErrCityNotFound)
	}

	if ref, err := s.resolveIndexedCity(query); err == nil {
		return ref.province, ref.city, "", nil
	} else if errors.Is(err, ErrSelectorAmbiguous) {
		return "", "", "", err
	}

	for _, operator := range cidrdata.Operators {
		if !strings.HasSuffix(query, operator) {
			continue
		}
		cityQuery := strings.TrimSpace(strings.TrimSuffix(query, operator))
		if cityQuery == "" {
			break
		}
		ref, err := s.resolveIndexedCity(cityQuery)
		if err != nil {
			if errors.Is(err, ErrSelectorAmbiguous) {
				return "", "", "", err
			}
			continue
		}
		return ref.province, ref.city, operator, nil
	}
	return "", "", "", fmt.Errorf("%w: %s", ErrCityNotFound, query)
}

func (s *Store) resolveIndexedCity(city string) (cityReference, error) {
	key := normalizeLookupKey(city)
	references := s.cityIndex[key]
	if len(references) == 0 {
		return cityReference{}, fmt.Errorf("%w: %s", ErrCityNotFound, city)
	}
	if len(references) > 1 {
		candidates := make([]string, 0, len(references))
		for _, reference := range references {
			candidates = append(candidates, displayName(reference.province)+"/"+displayName(reference.city))
		}
		return cityReference{}, fmt.Errorf("%w: %s (%s)", ErrSelectorAmbiguous, city, strings.Join(candidates, ", "))
	}
	return references[0], nil
}

func (s *Store) lookupProvince(province string) (string, map[string]*cidrdata.CityData, error) {
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

func lookupCity(cities map[string]*cidrdata.CityData, city string) (string, *cidrdata.CityData, error) {
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

func normalizeOperator(value string) (string, error) {
	operator, err := cidrdata.NormalizeOperator(value)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidOperator, value)
	}
	return operator, nil
}

func normalizeLookupKey(value string) string {
	return displayName(value)
}

func displayName(value string) string {
	return cidrdata.DisplayName(value)
}

func aggregateCIDRs(cities map[string]*cidrdata.CityData, version, operator string) []string {
	seen := make(map[string]struct{})
	aggregated := make([]string, 0)

	for _, cityData := range cities {
		for _, cidr := range cityVersionData(cityData, operator).CIDRs(version) {
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

func cityVersionData(cityData *cidrdata.CityData, operator string) cidrdata.VersionData {
	if cityData == nil {
		return cidrdata.VersionData{}
	}
	if operator == "" {
		return cityData.VersionData
	}
	if data := cityData.Operators[operator]; data != nil {
		return *data
	}
	return cidrdata.VersionData{}
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
