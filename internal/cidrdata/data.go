package cidrdata

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

const (
	NationalProvince = "中国大陆"
	NationalCity     = "中国大陆"
	OperatorTelecom  = "电信"
	OperatorUnicom   = "联通"
	OperatorMobile   = "移动"
)

var Operators = []string{OperatorTelecom, OperatorUnicom, OperatorMobile}

type VersionData struct {
	IPv4 []string `json:"4,omitempty"`
	IPv6 []string `json:"6,omitempty"`
}

func (d VersionData) CIDRs(version string) []string {
	if version == "6" {
		return d.IPv6
	}
	return d.IPv4
}

func (d *VersionData) Append(version string, cidrs ...string) {
	if version == "6" {
		d.IPv6 = append(d.IPv6, cidrs...)
		return
	}
	d.IPv4 = append(d.IPv4, cidrs...)
}

type CityData struct {
	VersionData
	Operators map[string]*VersionData `json:"operators,omitempty"`
}

type Dataset map[string]map[string]*CityData

var provinceByLookupName = map[string]string{
	"北京":  NationalMunicipality("北京"),
	"天津":  NationalMunicipality("天津"),
	"上海":  NationalMunicipality("上海"),
	"重庆":  NationalMunicipality("重庆"),
	"河北":  "河北省",
	"山西":  "山西省",
	"辽宁":  "辽宁省",
	"吉林":  "吉林省",
	"黑龙江": "黑龙江省",
	"江苏":  "江苏省",
	"浙江":  "浙江省",
	"安徽":  "安徽省",
	"福建":  "福建省",
	"江西":  "江西省",
	"山东":  "山东省",
	"河南":  "河南省",
	"湖北":  "湖北省",
	"湖南":  "湖南省",
	"广东":  "广东省",
	"海南":  "海南省",
	"四川":  "四川省",
	"贵州":  "贵州省",
	"云南":  "云南省",
	"陕西":  "陕西省",
	"甘肃":  "甘肃省",
	"青海":  "青海省",
	"内蒙古": "内蒙古自治区",
	"广西":  "广西壮族自治区",
	"西藏":  "西藏自治区",
	"宁夏":  "宁夏回族自治区",
	"新疆":  "新疆维吾尔自治区",
}

var (
	municipalities = map[string]struct{}{
		"北京市": {}, "天津市": {}, "上海市": {}, "重庆市": {},
	}
	autonomousPrefectureSuffixPattern = regexp.MustCompile(`(?:蒙古(?:族)?|回族|藏族|维吾尔(?:族)?|苗族|彝族|壮族|布依族|朝鲜族|满族|侗族|瑶族|白族|土家族|哈尼族|哈萨克(?:族)?|傣族|黎族|傈僳族|佤族|畲族|高山族|拉祜族|水族|东乡族|纳西族|景颇族|柯尔克孜(?:族)?|土族|达斡尔族|仫佬族|羌族|布朗族|撒拉族|毛南族|仡佬族|锡伯族|阿昌族|普米族|塔吉克(?:族)?|怒族|乌孜别克族|俄罗斯族|鄂温克族|德昂族|保安族|裕固族|京族|塔塔尔族|独龙族|鄂伦春族|赫哲族|门巴族|珞巴族|基诺族)+自治州$`)
)

func NationalMunicipality(name string) string {
	return strings.TrimSuffix(name, "市") + "市"
}

func CanonicalProvince(value string) (string, bool) {
	province, ok := provinceByLookupName[DisplayName(value)]
	return province, ok
}

func CanonicalCity(province, value string) string {
	if _, ok := municipalities[province]; ok && strings.TrimSpace(value) != "" {
		return province
	}
	return DisplayName(value)
}

func MainlandProvinces() []string {
	items := make([]string, 0, len(provinceByLookupName))
	seen := make(map[string]struct{}, len(provinceByLookupName))
	for _, province := range provinceByLookupName {
		if _, ok := seen[province]; ok {
			continue
		}
		seen[province] = struct{}{}
		items = append(items, province)
	}
	sort.Strings(items)
	return items
}

func IsMainlandProvince(value string) bool {
	_, ok := provinceByLookupName[DisplayName(value)]
	return ok
}

func DisplayName(value string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(value, "　", ""))
	if normalized == "" {
		return ""
	}

	normalized = autonomousPrefectureSuffixPattern.ReplaceAllString(normalized, "")
	for _, suffix := range []string{
		"维吾尔自治区", "回族自治区", "壮族自治区", "特别行政区",
		"自治区", "自治州", "地区", "盟", "省", "市",
	} {
		if strings.HasSuffix(normalized, suffix) {
			return strings.TrimSuffix(normalized, suffix)
		}
	}
	return normalized
}

func NormalizeOperator(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "":
		return "", nil
	case OperatorTelecom:
		return OperatorTelecom, nil
	case OperatorUnicom:
		return OperatorUnicom, nil
	case OperatorMobile:
		return OperatorMobile, nil
	default:
		return "", fmt.Errorf("invalid operator: %s", value)
	}
}

func OperatorsForISP(value string) []string {
	matched := make(map[string]struct{}, len(Operators))
	for _, token := range strings.Split(value, "/") {
		switch strings.TrimSpace(token) {
		case "电信", "中国电信":
			matched[OperatorTelecom] = struct{}{}
		case "联通", "中国联通":
			matched[OperatorUnicom] = struct{}{}
		case "移动", "中国移动", "铁通", "中移铁通":
			matched[OperatorMobile] = struct{}{}
		}
	}

	result := make([]string, 0, len(matched))
	for _, operator := range Operators {
		if _, ok := matched[operator]; ok {
			result = append(result, operator)
		}
	}
	return result
}

func Validate(dataset Dataset) error {
	allowed := make(map[string]struct{}, len(provinceByLookupName)+1)
	for _, province := range provinceByLookupName {
		allowed[province] = struct{}{}
	}
	allowed[NationalProvince] = struct{}{}

	for province, cities := range dataset {
		if _, ok := allowed[province]; !ok {
			return fmt.Errorf("non-mainland province in dataset: %s", province)
		}
		for city, data := range cities {
			if strings.TrimSpace(city) == "" || data == nil {
				return fmt.Errorf("invalid city entry in province %s", province)
			}
			if err := validateVersionData(data.VersionData); err != nil {
				return fmt.Errorf("validate %s/%s: %w", province, city, err)
			}
			for operator, versionData := range data.Operators {
				if _, err := NormalizeOperator(operator); err != nil {
					return fmt.Errorf("validate %s/%s: %w", province, city, err)
				}
				if versionData == nil {
					return fmt.Errorf("validate %s/%s/%s: nil data", province, city, operator)
				}
				if err := validateVersionData(*versionData); err != nil {
					return fmt.Errorf("validate %s/%s/%s: %w", province, city, operator, err)
				}
			}
		}
	}

	if dataset[NationalProvince] == nil || dataset[NationalProvince][NationalCity] == nil {
		return fmt.Errorf("national aggregate is missing")
	}
	return nil
}

func validateVersionData(data VersionData) error {
	for version, cidrs := range map[string][]string{"4": data.IPv4, "6": data.IPv6} {
		for _, cidr := range cidrs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil {
				return fmt.Errorf("parse CIDR %q: %w", cidr, err)
			}
			if prefix != prefix.Masked() {
				return fmt.Errorf("CIDR is not masked: %s", cidr)
			}
			if version == "4" && !prefix.Addr().Is4() {
				return fmt.Errorf("IPv6 CIDR in IPv4 list: %s", cidr)
			}
			if version == "6" && !prefix.Addr().Is6() {
				return fmt.Errorf("IPv4 CIDR in IPv6 list: %s", cidr)
			}
		}
	}
	return nil
}
