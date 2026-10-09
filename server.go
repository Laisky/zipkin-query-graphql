package zipkin_graphql

import (
	"errors"
	"net/http"

	ginMiddlewares "github.com/Laisky/gin-middlewares"

	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"

	utils "github.com/Laisky/go-utils"
	"github.com/Laisky/zap"
)

var (
	server = gin.New()
	auth   *ginMiddlewares.Auth
)

func setupAuth() (err error) {
	secret := utils.Settings.GetString("settings.secret")
	if secret == "" {
		auth = nil
		return errors.New("settings.secret must not be empty")
	}
	auth, err = ginMiddlewares.NewAuth([]byte(secret))
	return
}

// newGraphQLHandler preserves the legacy JSON-only POST contract regardless of Content-Type.
func newGraphQLHandler() http.HandlerFunc {
	graphQLServer := handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: &Resolver{}}))
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			r = r.Clone(r.Context())
			r.Header.Set("Content-Type", "application/json")
		}
		graphQLServer.ServeHTTP(w, r)
	}
}

func RunServer(addr string) {
	if !utils.Settings.GetBool("debug") {
		gin.SetMode(gin.ReleaseMode)
	}
	if err := setupAuth(); err != nil {
		utils.Logger.Panic("try to setup auth got error", zap.Error(err))
	}
	server.Use(LoggerMiddleware)

	server.Any("/health", func(ctx *gin.Context) {
		ctx.String(http.StatusOK, "hello, world")
	})

	// supported action:
	// cmdline, profile, symbol, goroutine, heap, threadcreate, block
	pprof.Register(server, "pprof")

	server.Any("/ui/", ginMiddlewares.FromStd(playground.Handler("GraphQL playground", "/query/")))
	server.Any("/query/", ginMiddlewares.FromStd(newGraphQLHandler()))

	utils.Logger.Info("listening on http", zap.String("addr", addr))
	utils.Logger.Panic("httpServer exit", zap.Error(server.Run(addr)))
}

func LoggerMiddleware(ctx *gin.Context) {
	utils.Logger.Debug("request",
		zap.String("path", ctx.Request.RequestURI),
		zap.String("method", ctx.Request.Method),
	)
	ctx.Next()
}
