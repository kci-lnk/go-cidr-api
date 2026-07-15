package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-cidr-api/internal/cidrdata"
)

func TestLoadStoreSupportsLegacyDataset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.json")
	content := `{"河北省":{"石家庄":{"4":["1.0.0.0/24"],"6":[]}},"中国大陆":{"中国大陆":{"4":["1.0.0.0/24"]}}}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.GetCIDRs("河北", "石家庄市", "", "4")
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 1 || result.CIDRs[0] != "1.0.0.0/24" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestStoreOperatorAndSelectorQueries(t *testing.T) {
	store := loadTestStore(t)

	province, city, operator, err := store.ResolveSelector("石家庄移动")
	if err != nil {
		t.Fatal(err)
	}
	if province != "河北省" || city != "石家庄" || operator != "移动" {
		t.Fatalf("unexpected selector resolution: %q %q %q", province, city, operator)
	}
	province, city, operator, err = store.ResolveSelector("北京")
	if err != nil {
		t.Fatal(err)
	}
	if province != "北京市" || city != "北京市" || operator != "" {
		t.Fatalf("unexpected Beijing resolution: %q %q %q", province, city, operator)
	}
	if _, _, _, err := store.ResolveSelector("同名"); !errors.Is(err, ErrSelectorAmbiguous) {
		t.Fatalf("ResolveSelector() error = %v, want ErrSelectorAmbiguous", err)
	}

	result, err := store.GetCIDRs("河北", "石家庄", "移动", "4")
	if err != nil {
		t.Fatal(err)
	}
	if result.Operator != "移动" || len(result.CIDRs) != 1 || result.CIDRs[0] != "1.0.0.0/25" {
		t.Fatalf("unexpected operator result: %#v", result)
	}
	result, err = store.GetCIDRs("河北", "石家庄", "电信", "4")
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 0 || result.CIDRs == nil {
		t.Fatalf("empty operator result must contain an empty slice: %#v", result)
	}
}

func TestAppSelectorAndOperatorRoutes(t *testing.T) {
	app := NewApp(loadTestStore(t))

	status, payload := app.route(http.MethodGet, "/api/v1/cidrs", url.Values{
		"selector":   {"石家庄移动"},
		"ip_version": {"4"},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, payload = %#v", status, payload)
	}
	result := payload.Data.(CIDRQueryResult)
	if result.City != "石家庄" || result.Operator != "移动" {
		t.Fatalf("unexpected selector result: %#v", result)
	}

	status, _ = app.route(http.MethodGet, "/api/v1/cidrs", url.Values{
		"selector": {"石家庄"},
		"province": {"河北"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("selector conflict status = %d", status)
	}
	status, _ = app.route(http.MethodGet, "/api/v1/cidrs", url.Values{"selector": {"同名"}})
	if status != http.StatusConflict {
		t.Fatalf("ambiguous selector status = %d", status)
	}
	status, _ = app.route(http.MethodGet, "/api/v1/cidrs", url.Values{
		"province": {"河北"}, "city": {"石家庄"}, "operator": {"广电"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("invalid operator status = %d", status)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/cidrs?province=河北&city=石家庄&operator=电信&ip_version=4", nil)
	recorder := httptest.NewRecorder()
	app.serveHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"cidrs":[]`) {
		t.Fatalf("empty CIDR response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func loadTestStore(t *testing.T) *Store {
	t.Helper()
	dataset := cidrdata.Dataset{
		"河北省": {
			"石家庄": {
				VersionData: cidrdata.VersionData{IPv4: []string{"1.0.0.0/24"}},
				Operators: map[string]*cidrdata.VersionData{
					"移动": {IPv4: []string{"1.0.0.0/25"}},
				},
			},
			"同名": {VersionData: cidrdata.VersionData{IPv4: []string{"1.0.1.0/24"}}},
		},
		"河南省": {
			"同名": {VersionData: cidrdata.VersionData{IPv4: []string{"1.0.2.0/24"}}},
		},
		"北京市": {
			"北京市": {
				VersionData: cidrdata.VersionData{IPv4: []string{"2.0.0.0/24"}},
				Operators: map[string]*cidrdata.VersionData{
					"电信": {IPv4: []string{"2.0.0.0/25"}},
				},
			},
		},
		cidrdata.NationalProvince: {
			cidrdata.NationalCity: {VersionData: cidrdata.VersionData{IPv4: []string{"1.0.0.0/8"}}},
		},
	}
	path := filepath.Join(t.TempDir(), "data.json")
	content, err := json.Marshal(dataset)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
