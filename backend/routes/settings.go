package routes

import (
	"net/http"

	"voltis/config"
	"voltis/settings"

	"github.com/labstack/echo/v4"
)

type SettingsRoutes struct {
	st    *settings.Store
	proxy config.ProxyAuth
}

func (sr *SettingsRoutes) Register(g *echo.Group) {
	g.GET("", adminOnly(sr.list))
	g.POST("", adminOnly(sr.update))
	g.GET("/proxy-auth", adminOnly(sr.proxyAuth))
}

// Read-only: the trust boundary stays in env, out of an admin's reach.
func (sr *SettingsRoutes) proxyAuth(c echo.Context) error {
	cidrs := make([]string, len(sr.proxy.TrustedCIDRs))
	for i, cidr := range sr.proxy.TrustedCIDRs {
		cidrs[i] = cidr.String()
	}
	return c.JSON(http.StatusOK, map[string]any{
		"enabled":       sr.proxy.Enabled(),
		"trusted_cidrs": cidrs,
		"user_header":   sr.proxy.UserHeader,
		"email_header":  sr.proxy.EmailHeader,
		"groups_header": sr.proxy.GroupsHeader,
	})
}

type settingDTO struct {
	Key    string `json:"key"`
	Type   string `json:"type"`
	Value  any    `json:"value"`
	Secret bool   `json:"secret"`
	Set    *bool  `json:"set,omitempty"`
	Help   string `json:"help"`
}

func (sr *SettingsRoutes) list(c echo.Context) error {
	result := []settingDTO{}
	for _, def := range settings.All() {
		if def.Internal {
			continue
		}
		value := sr.st.Get(def.Key)
		dto := settingDTO{Key: def.Key, Type: string(def.Type), Value: value, Secret: def.Secret(), Help: def.Help}
		if def.Secret() {
			stored := value != ""
			dto.Value, dto.Set = nil, &stored
		}
		result = append(result, dto)
	}
	return c.JSON(http.StatusOK, result)
}

func (sr *SettingsRoutes) update(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}

	var req map[string]any
	if err := c.Bind(&req); err != nil {
		return err
	}

	parsed := make(map[string]any, len(req))
	for key, value := range req {
		def, ok := settings.Lookup(key)
		if !ok || def.Internal {
			return echo.NewHTTPError(http.StatusBadRequest, "unknown setting "+key)
		}
		v, err := def.Parse(value)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		parsed[key] = v
	}

	if err := sr.st.SetMany(reqCtx(c), parsed); err != nil {
		return err
	}
	return sr.list(c)
}
