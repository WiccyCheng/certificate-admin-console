package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strings"

	"certadmin/internal/service"

	"github.com/gin-gonic/gin"
)

const actorKey = "actor"
const requestIDKey = "requestId"

func requestID(c *gin.Context) string {
	if value, ok := c.Get(requestIDKey); ok {
		return value.(string)
	}
	return ""
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		var raw [8]byte
		if _, err := rand.Read(raw[:]); err != nil {
			raw = [8]byte{0, 0, 0, 0, 0, 0, 0, 1}
		}
		id := "req-" + hex.EncodeToString(raw[:])
		c.Set(requestIDKey, id)
		c.Header("X-Request-Id", id)
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
}

// recoveryMiddleware converts panics to JSON 500s but lets
// http.ErrAbortHandler pass through so a broken CSV stream closes the
// connection instead of looking like a successful download.
func recoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				log.Printf("request %s panic: %v", requestID(c), recovered)
				if !c.Writer.Written() {
					s := &Server{}
					s.writeError(c, &apiError{
						Status:  http.StatusInternalServerError,
						Code:    "INTERNAL_ERROR",
						Message: "服务器内部错误",
					})
				} else {
					c.Abort()
				}
			}
		}()
		c.Next()
	}
}

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		values := c.Request.Header.Values("Authorization")
		if len(values) != 1 {
			s.writeError(c, unauthorized("身份凭证无效或缺失"))
			return
		}
		parts := strings.SplitN(values[0], " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
			s.writeError(c, unauthorized("身份凭证无效或缺失"))
			return
		}
		actor, err := s.svc.Authenticate(c.Request.Context(), strings.TrimSpace(parts[1]))
		if err != nil {
			if errors.Is(err, service.ErrUnauthenticated) {
				s.writeError(c, unauthorized("身份凭证无效或缺失"))
				return
			}
			s.fail(c, err)
			return
		}
		c.Set(actorKey, actor)
		c.Next()
	}
}

func actorFrom(c *gin.Context) *service.Actor {
	value, _ := c.Get(actorKey)
	actor, _ := value.(*service.Actor)
	return actor
}
