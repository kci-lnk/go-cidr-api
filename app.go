package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	pathpkg "path"
	"strings"
)

type App struct {
	store *Store
}

type responseEnvelope struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func NewApp(store *Store) *App {
	return &App{store: store}
}

func (a *App) HTTPHandler() http.Handler {
	return compressionMiddleware(http.HandlerFunc(a.serveHTTP))
}

func (a *App) serveHTTP(w http.ResponseWriter, r *http.Request) {
	status, payload := a.route(r.Method, r.URL.Path, r.URL.Query())

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	encoder := json.NewEncoder(w)
	if err := encoder.Encode(payload); err != nil {
		http.Error(w, `{"code":500,"message":"failed to encode response"}`, http.StatusInternalServerError)
	}
}

func (a *App) route(method, rawPath string, query url.Values) (int, responseEnvelope) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet {
		return errorResponse(http.StatusMethodNotAllowed, "only GET is supported")
	}

	cleanPath := pathpkg.Clean("/" + strings.TrimSpace(rawPath))
	switch cleanPath {
	case "/":
		return successResponse(map[string]interface{}{
			"name":    "go-cidr-api-api",
			"version": "v1",
			"routes": []string{
				"GET /healthz",
				"GET /api/v1/provinces",
				"GET /api/v1/provinces/{province}/cities",
				"GET /api/v1/provinces/{province}/cidrs?operator=移动&ip_version=4|6",
				"GET /api/v1/provinces/{province}/cities/{city}/cidrs?operator=移动&ip_version=4|6",
				"GET /api/v1/cidrs?province=广东&city=深圳&operator=电信&ip_version=4|6",
				"GET /api/v1/cidrs?selector=石家庄移动&ip_version=4|6",
			},
		})
	case "/healthz":
		return successResponse(map[string]string{"status": "ok"})
	case "/api/v1/provinces":
		items := a.store.ListProvinces()
		return successResponse(map[string]interface{}{
			"items": items,
			"total": len(items),
		})
	case "/api/v1/cidrs":
		return a.handleCIDRQuery(query)
	}

	segments := splitPath(cleanPath)
	if len(segments) == 5 &&
		segments[0] == "api" &&
		segments[1] == "v1" &&
		segments[2] == "provinces" &&
		segments[4] == "cidrs" {
		return a.handleCIDRs(segments[3], "", query.Get("operator"), query.Get("ip_version"))
	}

	if len(segments) == 5 &&
		segments[0] == "api" &&
		segments[1] == "v1" &&
		segments[2] == "provinces" &&
		segments[4] == "cities" {
		return a.handleCities(segments[3])
	}

	if len(segments) == 7 &&
		segments[0] == "api" &&
		segments[1] == "v1" &&
		segments[2] == "provinces" &&
		segments[4] == "cities" &&
		segments[6] == "cidrs" {
		return a.handleCIDRs(segments[3], segments[5], query.Get("operator"), query.Get("ip_version"))
	}

	return errorResponse(http.StatusNotFound, "route not found")
}

func (a *App) handleCities(province string) (int, responseEnvelope) {
	if strings.TrimSpace(province) == "" {
		return errorResponse(http.StatusBadRequest, "province is required")
	}

	resolvedProvince, items, err := a.store.ListCities(province)
	if err != nil {
		return storeErrorResponse(err)
	}

	return successResponse(map[string]interface{}{
		"province": resolvedProvince,
		"items":    items,
		"total":    len(items),
	})
}

func (a *App) handleCIDRQuery(query url.Values) (int, responseEnvelope) {
	selector := strings.TrimSpace(query.Get("selector"))
	if selector == "" {
		return a.handleCIDRs(query.Get("province"), query.Get("city"), query.Get("operator"), query.Get("ip_version"))
	}
	if strings.TrimSpace(query.Get("province")) != "" ||
		strings.TrimSpace(query.Get("city")) != "" ||
		strings.TrimSpace(query.Get("operator")) != "" {
		return errorResponse(http.StatusBadRequest, ErrSelectorConflict.Error())
	}

	province, city, operator, err := a.store.ResolveSelector(selector)
	if err != nil {
		return storeErrorResponse(err)
	}
	return a.handleCIDRs(province, city, operator, query.Get("ip_version"))
}

func (a *App) handleCIDRs(province, city, operator, ipVersion string) (int, responseEnvelope) {
	if strings.TrimSpace(province) == "" {
		return errorResponse(http.StatusBadRequest, "province is required")
	}

	result, err := a.store.GetCIDRs(province, city, operator, ipVersion)
	if err != nil {
		return storeErrorResponse(err)
	}

	return successResponse(result)
}

func storeErrorResponse(err error) (int, responseEnvelope) {
	switch {
	case errors.Is(err, ErrProvinceNotFound):
		return errorResponse(http.StatusNotFound, err.Error())
	case errors.Is(err, ErrCityNotFound):
		return errorResponse(http.StatusNotFound, err.Error())
	case errors.Is(err, ErrInvalidIPVersion):
		return errorResponse(http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrInvalidOperator), errors.Is(err, ErrSelectorConflict):
		return errorResponse(http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrSelectorAmbiguous):
		return errorResponse(http.StatusConflict, err.Error())
	default:
		return errorResponse(http.StatusInternalServerError, err.Error())
	}
}

func successResponse(data interface{}) (int, responseEnvelope) {
	return http.StatusOK, responseEnvelope{
		Code:    0,
		Message: "ok",
		Data:    data,
	}
}

func errorResponse(status int, message string) (int, responseEnvelope) {
	return status, responseEnvelope{
		Code:    status,
		Message: message,
	}
}

func splitPath(rawPath string) []string {
	trimmed := strings.Trim(rawPath, "/")
	if trimmed == "" {
		return nil
	}

	parts := strings.Split(trimmed, "/")
	for index, part := range parts {
		if decoded, err := url.PathUnescape(part); err == nil {
			parts[index] = decoded
		}
	}

	return parts
}
