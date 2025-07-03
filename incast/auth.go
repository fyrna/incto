package incast

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/fyrna/incto"
)

func BasicAuth(credentials string) incto.MiddlewareFunc {
	parts := strings.SplitN(credentials, ":", 2)
	if len(parts) != 2 {
		panic("BasicAuth credentials should be in format 'username:password'")
	}

	expectedAuth := base64.StdEncoding.EncodeToString([]byte(credentials))

	return func(next incto.HandlerFunc) incto.HandlerFunc {
		return func(c incto.Ctx) error {
			auth := c.Header("Authorization")

			if !strings.HasPrefix(auth, "Basic ") {
				c.ResponseWriter().Header().Set("WWW-Authenticate", "Basic realm=\"Restricted\"")
				return c.String(401, "Unauthorized")
			}

			providedAuth := strings.TrimPrefix(auth, "Basic ")
			if providedAuth != expectedAuth {
				return c.String(401, "Unauthorized")
			}

			return next(c)
		}
	}
}

func RateLimit(requestsPerMinute int) incto.MiddlewareFunc {
	type client struct {
		requests  int
		lastReset time.Time
	}

	clients := make(map[string]*client)

	return func(next incto.HandlerFunc) incto.HandlerFunc {
		return func(c incto.Ctx) error {
			clientIP := c.Request().RemoteAddr

			now := time.Now()

			if cl, exists := clients[clientIP]; exists {
				if now.Sub(cl.lastReset) > time.Minute {
					cl.requests = 0
					cl.lastReset = now
				}

				if cl.requests >= requestsPerMinute {
					return c.String(429, "Rate limit exceeded")
				}

				cl.requests++
			} else {
				clients[clientIP] = &client{
					requests:  1,
					lastReset: now,
				}
			}

			return next(c)
		}
	}
}
