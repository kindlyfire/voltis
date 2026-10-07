package routes

import (
	"net/http"

	"voltis/config"
	"voltis/settings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

// apiVersion lets native clients reject a server too old for them.
const apiVersion = 1

type infoDTO struct {
	Version              string `json:"version"`
	APIVersion           int    `json:"api_version"`
	ServerID             string `json:"server_id"`
	RegistrationEnabled  bool   `json:"registration_enabled"`
	FirstUserFlow        bool   `json:"first_user_flow"`
	PasswordLoginEnabled bool   `json:"password_login_enabled"`
	OIDCEnabled          bool   `json:"oidc_enabled"`
	OIDCButtonLabel      string `json:"oidc_button_label"`
	OIDCAutoRedirect     bool   `json:"oidc_auto_redirect"`
	WebBuild             string `json:"web_build"`
}

func infoHandler(pool *pgxpool.Pool, st *settings.Store, webBuild string) echo.HandlerFunc {
	return func(c echo.Context) error {
		first, err := isFirstUserFlow(reqCtx(c), pool, st)
		if err != nil {
			return err
		}
		oidcOn := oidcConfigOf(st).usable()
		return c.JSON(http.StatusOK, infoDTO{
			Version:              config.AppVersion,
			APIVersion:           apiVersion,
			ServerID:             st.String(settings.InstallationID),
			RegistrationEnabled:  st.Bool(settings.AuthRegistrationEnabled),
			FirstUserFlow:        first,
			PasswordLoginEnabled: st.Bool(settings.AuthPasswordLoginEnabled),
			OIDCEnabled:          oidcOn,
			OIDCButtonLabel:      st.String(settings.OIDCButtonLabel),
			OIDCAutoRedirect:     oidcOn && st.Bool(settings.OIDCAutoRedirect),
			WebBuild:             webBuild,
		})
	}
}
