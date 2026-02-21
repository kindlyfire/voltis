package routes

import (
	"net/http"

	"voltis/config"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

const appVersion = "dev"

type infoDTO struct {
	Version             string `json:"version"`
	RegistrationEnabled bool   `json:"registration_enabled"`
	FirstUserFlow       bool   `json:"first_user_flow"`
}

func infoHandler(pool *pgxpool.Pool) echo.HandlerFunc {
	return func(c echo.Context) error {
		cfg := config.Get()
		first, err := isFirstUserFlow(reqCtx(c), pool)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, infoDTO{
			Version:             appVersion,
			RegistrationEnabled: cfg.RegistrationEnabled,
			FirstUserFlow:       first,
		})
	}
}
