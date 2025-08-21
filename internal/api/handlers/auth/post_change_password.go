package auth

import (
	"net/http"

	"github.com/go-openapi/swag"
	"github.com/hzbay/chain-bridge/internal/api"
	"github.com/hzbay/chain-bridge/internal/auth"
	"github.com/hzbay/chain-bridge/internal/data/dto"
	"github.com/hzbay/chain-bridge/internal/types"
	"github.com/hzbay/chain-bridge/internal/util"
	"github.com/labstack/echo/v4"
)

func PostChangePasswordRoute(s *api.Server) *echo.Route {
	return s.Router.APIV1Auth.POST("/change-password", postChangePasswordHandler(s))
}

func postChangePasswordHandler(s *api.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()
		user := auth.UserFromEchoContext(c)
		log := util.LogFromContext(ctx)

		var body types.PostChangePasswordPayload
		if err := util.BindAndValidateBody(c, &body); err != nil {
			return err
		}

		result, err := s.Auth.UpdatePassword(ctx, dto.UpdatePasswordRequest{
			User:            *user,
			CurrentPassword: swag.StringValue(body.CurrentPassword),
			NewPassword:     swag.StringValue(body.NewPassword),
		})
		if err != nil {
			log.Debug().Err(err).Msg("Failed to update password")
			return err
		}

		return util.ValidateAndReturn(c, http.StatusOK, result.ToTypes())
	}
}
