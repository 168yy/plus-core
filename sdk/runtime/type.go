package runtime

import (
	"context"
	"github.com/casbin/casbin/v2"
	"github.com/gogf/gf-jwt/v2"
	"github.com/gogf/gf/v2/container/gvar"
	"github.com/gogf/gf/v2/i18n/gi18n"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcache"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/jxo-me/plus-core/sdk/pkg/ws"
)

type Runtime interface {
	// SetServer Http Server
	SetServer(srv *ghttp.Server)
	GetServer() *ghttp.Server

	// SetCasbin casbin module
	SetCasbin(key string, enforcer *casbin.SyncedEnforcer)
	GetCasbin() map[string]*casbin.SyncedEnforcer
	GetCasbinKey(key string) *casbin.SyncedEnforcer
	// SetJwt jwt module
	SetJwt(key string, jwtIns *jwt.GfJWTMiddleware)
	GetJwt() map[string]*jwt.GfJWTMiddleware
	GetJwtKey(moduleKey string) *jwt.GfJWTMiddleware
	// SetLang gi18n
	SetLang(lang *gi18n.Manager)
	GetLang() *gi18n.Manager
	// SetConfig config
	SetConfig(c *gcfg.Config)
	GetConfig() *gcfg.Config
	Config(ctx context.Context, pattern string) *gvar.Var
	// SetCache cache
	SetCache(c *gcache.Cache)
	GetCache() *gcache.Cache
	Cache() *gcache.Cache
	// SetWebSocket websocket
	SetWebSocket(s *ws.Instance)
	WebSocket() *ws.Instance
	GetWebSocket() *ws.Instance
}
