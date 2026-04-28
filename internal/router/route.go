package router

import (
	"bluebell/internal/controller"
	"bluebell/internal/logger"
	"bluebell/internal/middlewares"
	"net/http"
	"strings"

	_ "bluebell/docs"

	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/swaggo/gin-swagger/swaggerFiles"
)

// SetupRouter 初始化 Gin 路由并注册全部接口与静态资源。
func SetupRouter(mode string) *gin.Engine {
	if mode == gin.ReleaseMode {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(logger.GinLogger(), logger.GinRecovery(true))

	r.LoadHTMLFiles("./templates/index.html")
	r.Static("/static", "./static")

	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	v1 := r.Group("/api/v1")
	v1.POST("/signup", controller.SignUpHandler)
	v1.POST("/login", controller.LoginHandler)

	v1.GET("/posts2", controller.GetPostListHandler2)
	v1.GET("/posts", controller.GetPostListHandler)
	v1.GET("/community", controller.CommunityHandler)
	v1.GET("/community/:id", controller.CommunityDetailHandler)
	v1.GET("/post/:id", controller.GetPostDetailHandler)
	v1.GET("/post/:id/comments", controller.GetCommentListHandler)
	v1.GET("/rag/search", controller.RAGSearchHandler)
	v1.GET("/rag/chat/stream", controller.RAGChatStreamHandler)
	v1.POST("/rag/chat/stream", controller.RAGChatStreamHandler)

	v1.Use(middlewares.JWTAuthMiddleware())
	{
		v1.POST("/post", controller.CreatePostHandler)
		v1.DELETE("/post/:id", controller.DeletePostHandler)
		v1.POST("/comment", controller.CreateCommentHandler)
		v1.POST("/vote", controller.PostVoteController)
		v1.POST("/rag/reindex", controller.RAGReindexHandler)
	}

	pprof.Register(r)

	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method == http.MethodGet &&
			!strings.HasPrefix(c.Request.URL.Path, "/api/") &&
			!strings.HasPrefix(c.Request.URL.Path, "/static/") &&
			!strings.HasPrefix(c.Request.URL.Path, "/swagger/") {
			c.HTML(http.StatusOK, "index.html", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"msg": "404"})
	})
	return r
}
