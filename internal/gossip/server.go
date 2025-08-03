package gossip

import (
	"context"
	"log"
	"net/http"
	"sync"

	"GossamerDB/internal/security"
	"GossamerDB/pkg/model"

	"github.com/gin-gonic/gin"
)

type Server struct {
	router       *gin.Engine
	srv          *http.Server
	engine       *Engine
	nodeHealthMu sync.RWMutex
}

func NewServer(listenAddress string, engine *Engine) *Server {
	s := &Server{
		router: gin.Default(),
		engine: engine,
	}
	s.setupRoutes()
	s.srv, _ = security.ConfigureSecureServer(listenAddress, s.router)
	return s
}

func (s *Server) setupRoutes() {
	s.router.GET("/health", s.handleHealth)
	s.router.POST("/gossip", s.handleGossip)
	s.router.POST("/join", s.handleJoin)
}

func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, s.engine.GetNodeHealth())
}

func (s *Server) handleGossip(c *gin.Context) {
	var msg model.GossipMessage
	if err := c.ShouldBindJSON(&msg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[RECV] Gossip received from %s", msg.SenderID)

	s.engine.UpdateNodeHealth(msg.NodeHealth)

	c.JSON(http.StatusOK, gin.H{"status": "received"})
}

func (s *Server) handleJoin(c *gin.Context) {
	var peer struct {
		URL string `json:"url"`
	}
	if err := c.ShouldBindJSON(&peer); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.engine.AddPeer(peer.URL)
	log.Printf("[JOIN] Peer added: %s", peer.URL)
	c.JSON(http.StatusOK, gin.H{"message": "Peer added"})
}

func (s *Server) ListenAndServe() error {
	log.Printf("[GOSSIP SERVER] Listening on %s\n", s.srv.Addr)
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
