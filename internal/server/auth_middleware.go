package server

import (
	"github.com/gin-gonic/gin"
)

// WithAuthMiddleware sets custom auth middlewares for WebUI and Model API endpoints
// This allows TBE to inject its own JWT auth middleware instead of using
// tingly-box's default UserAuthMiddleware and ModelAuthMiddleware
//
// Usage in TBE:
//
//	server := NewServer(cfg,
//	    WithAuthMiddleware(tbeUserAuth, tbeModelAuth),
//	)
func WithAuthMiddleware(userAuth, modelAuth gin.HandlerFunc) ServerOption {
	return func(s *Server) {
		s.customUserAuthMiddleware = userAuth
		s.customModelAuthMiddleware = modelAuth
	}
}

// WithUserAuthMiddleware sets a custom user auth middleware for WebUI endpoints
// Use this if you only want to replace UserAuthMiddleware but keep ModelAuthMiddleware
func WithUserAuthMiddleware(userAuth gin.HandlerFunc) ServerOption {
	return func(s *Server) {
		s.customUserAuthMiddleware = userAuth
	}
}

// WithModelAuthMiddleware sets a custom model auth middleware for Model API endpoints
// Use this if you only want to replace ModelAuthMiddleware but keep UserAuthMiddleware
func WithModelAuthMiddleware(modelAuth gin.HandlerFunc) ServerOption {
	return func(s *Server) {
		s.customModelAuthMiddleware = modelAuth
	}
}

// Helper methods to get the appropriate auth middleware
// These methods return custom middleware if provided by TBE, otherwise default middleware

// getUserAuthMiddleware returns the user auth middleware to use
func (s *Server) getUserAuthMiddleware() gin.HandlerFunc {
	if s.customUserAuthMiddleware != nil {
		return s.customUserAuthMiddleware
	}
	return s.authMW.UserAuthMiddleware()
}

// getModelAuthMiddleware returns the model auth middleware to use
func (s *Server) getModelAuthMiddleware() gin.HandlerFunc {
	if s.customModelAuthMiddleware != nil {
		return s.customModelAuthMiddleware
	}
	return s.authMW.ModelAuthMiddleware()
}
