package middleware

import (
	"net/http"

	nbcontext "github.com/netbirdio/netbird/management/server/context"
	"github.com/netbirdio/netbird/management/server/saas"
	"github.com/netbirdio/netbird/shared/management/http/util"
)

type PlatformAdminMiddleware struct {
	authorizer saas.PlatformAuthorizer
}

func NewPlatformAdminMiddleware(authorizer saas.PlatformAuthorizer) *PlatformAdminMiddleware {
	return &PlatformAdminMiddleware{authorizer: authorizer}
}

func (m *PlatformAdminMiddleware) Handler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
		if err != nil {
			util.WriteError(r.Context(), err, w)
			return
		}
		if _, err := m.authorizer.RequireAdmin(r.Context(), userAuth.UserId); err != nil {
			util.WriteError(r.Context(), err, w)
			return
		}
		h.ServeHTTP(w, r)
	})
}
