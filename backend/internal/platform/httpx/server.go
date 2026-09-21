// Package httpx — обёртка над gin с graceful shutdown.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

const (
	defaultPort            = 8080
	defaultReadTimeout     = 10 * time.Second
	defaultWriteTimeout    = 10 * time.Second
	defaultShutdownTimeout = 5 * time.Second
)

type Server struct {
	eg              *errgroup.Group
	engine          *gin.Engine
	server          *http.Server
	notify          chan error
	port            int
	readTimeout     time.Duration
	writeTimeout    time.Duration
	shutdownTimeout time.Duration
	log             *slog.Logger
}

type Option func(*Server)

func Port(port int) Option                   { return func(s *Server) { s.port = port } }
func ReadTimeout(d time.Duration) Option     { return func(s *Server) { s.readTimeout = d } }
func WriteTimeout(d time.Duration) Option    { return func(s *Server) { s.writeTimeout = d } }
func ShutdownTimeout(d time.Duration) Option { return func(s *Server) { s.shutdownTimeout = d } }

// New создаёт HTTP-сервер поверх gin.Engine.
func New(log *slog.Logger, opts ...Option) *Server {
	group, _ := errgroup.WithContext(context.Background())
	group.SetLimit(1)

	gin.SetMode(gin.ReleaseMode)

	s := &Server{
		eg:              group,
		notify:          make(chan error, 1),
		port:            defaultPort,
		readTimeout:     defaultReadTimeout,
		writeTimeout:    defaultWriteTimeout,
		shutdownTimeout: defaultShutdownTimeout,
		log:             log,
	}

	for _, opt := range opts {
		opt(s)
	}

	s.engine = gin.New()
	s.server = &http.Server{
		Addr:           fmt.Sprintf(":%d", s.port),
		Handler:        s.engine,
		ReadTimeout:    s.readTimeout,
		WriteTimeout:   s.writeTimeout,
		MaxHeaderBytes: 1 << 20,
	}

	return s
}

func (s *Server) Engine() *gin.Engine {
	return s.engine
}

func (s *Server) Start() {
	s.eg.Go(func() error {
		s.log.Info("starting http server", "port", s.port)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.notify <- err
			close(s.notify)
			s.log.Error("http server failed", "error", err)
			return err
		}
		return nil
	})
}

func (s *Server) Notify() <-chan error {
	return s.notify
}

func (s *Server) Shutdown() error {
	var errs []error

	ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil && !errors.Is(err, context.Canceled) {
		errs = append(errs, err)
	}
	if err := s.eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		errs = append(errs, err)
	}

	s.log.Info("http server stopped")
	return errors.Join(errs...)
}
