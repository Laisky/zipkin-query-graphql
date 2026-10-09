package zipkin_graphql

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ginMiddlewares "github.com/Laisky/gin-middlewares"
	utils "github.com/Laisky/go-utils"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

// TestConsumerAuthContextContract verifies configured signing and Gin context survive package extraction.
func TestConsumerAuthContextContract(t *testing.T) {
	const fixtureSecret = "synthetic-consumer-signing-key"
	previousSecret := utils.Settings.Get("settings.secret")
	previousAuth := auth
	t.Cleanup(func() { utils.Settings.Set("settings.secret", previousSecret); auth = previousAuth })
	utils.Settings.Set("settings.secret", fixtureSecret)
	if err := setupAuth(); err != nil {
		t.Fatal("auth initialization failed")
	}
	makeToken := func(key string, expires time.Time) string {
		t.Helper()
		value, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "fixture", "exp": expires.Unix()}).SignedString([]byte(key))
		if err != nil {
			t.Fatal("synthetic token signing failed")
		}
		return value
	}
	for _, tc := range []struct {
		name, token string
		status      int
	}{
		{"valid", makeToken(fixtureSecret, time.Now().Add(time.Hour)), http.StatusOK},
		{"wrong key", makeToken("different-synthetic-key", time.Now().Add(time.Hour)), http.StatusUnauthorized},
		{"expired", makeToken(fixtureSecret, time.Now().Add(-time.Hour)), http.StatusUnauthorized},
		{"missing", "", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.GET("/fixture", ginMiddlewares.FromStd(func(w http.ResponseWriter, r *http.Request) {
				ginContext := ginMiddlewares.GetGinCtxFromStdCtx(r.Context())
				if ginContext.Request.URL.Path != "/fixture" || ginContext.Writer != w {
					t.Error("Gin request and writer did not survive adapter")
				}
				claims := jwt.MapClaims{}
				if err := auth.GetUserClaims(r.Context(), &claims); err != nil {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if claims["sub"] != "fixture" {
					t.Error("authenticated identity changed")
				}
				w.WriteHeader(http.StatusOK)
			}))
			request := httptest.NewRequest(http.MethodGet, "/fixture", nil)
			if tc.token != "" {
				request.AddCookie(&http.Cookie{Name: "token", Value: tc.token})
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d", response.Code, tc.status)
			}
		})
	}
}

// TestConsumerAuthRejectsEmptySecret preserves the legacy startup validation.
func TestConsumerAuthRejectsEmptySecret(t *testing.T) {
	previousSecret := utils.Settings.Get("settings.secret")
	previousAuth := auth
	t.Cleanup(func() { utils.Settings.Set("settings.secret", previousSecret); auth = previousAuth })
	utils.Settings.Set("settings.secret", "")
	if err := setupAuth(); err == nil {
		t.Fatal("empty signing secret accepted during startup")
	}
}
