package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dropbox/godropbox/time2"
	"github.com/hzbay/chain-bridge/internal/auth"
	"github.com/hzbay/chain-bridge/internal/config"
	"github.com/hzbay/chain-bridge/internal/data/dto"
	"github.com/hzbay/chain-bridge/internal/data/local"
	"github.com/hzbay/chain-bridge/internal/events"
	"github.com/hzbay/chain-bridge/internal/i18n"
	"github.com/hzbay/chain-bridge/internal/mailer"
	"github.com/hzbay/chain-bridge/internal/mailer/transport"
	"github.com/hzbay/chain-bridge/internal/metrics"
	"github.com/hzbay/chain-bridge/internal/push"
	"github.com/hzbay/chain-bridge/internal/push/provider"
	"github.com/hzbay/chain-bridge/internal/queue"
	"github.com/hzbay/chain-bridge/internal/services/account"
	"github.com/hzbay/chain-bridge/internal/services/chains"
	"github.com/hzbay/chain-bridge/internal/services/nft"
	"github.com/hzbay/chain-bridge/internal/services/tokens"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"

	// Import postgres driver for database/sql package
	_ "github.com/lib/pq"
)

type Router struct {
	Routes       []*echo.Route
	Root         *echo.Group
	Management   *echo.Group
	APIV1Auth    *echo.Group
	APIV1Push    *echo.Group
	APIV1Account *echo.Group
	APIV1Assets  *echo.Group
}

type Server struct {
	Config              config.Server
	DB                  *sql.DB
	Echo                *echo.Echo
	Router              *Router
	Mailer              *mailer.Mailer
	Push                *push.Service
	I18n                *i18n.Service
	Clock               time2.Clock
	Auth                AuthService
	Local               *local.Service
	Metrics             *metrics.Service
	AccountService      account.Service
	BatchProcessor      queue.BatchProcessor
	QueueMonitor        *queue.Monitor
	BatchOptimizer      *queue.BatchOptimizer
	ChainsService       chains.Service
	TokensService       tokens.Service
	NFTService          nft.Service
	PaymentEventService *events.PaymentEventService
	RabbitMQClient      *queue.RabbitMQClient
}

type AuthService interface {
	GetAppUserProfileIfExists(context.Context, string) (*dto.AppUserProfile, error)
	InitPasswordReset(context.Context, dto.InitPasswordResetRequest) (dto.InitPasswordResetResult, error)
	Login(context.Context, dto.LoginRequest) (dto.LoginResult, error)
	Logout(context.Context, dto.LogoutRequest) error
	Refresh(context.Context, dto.RefreshRequest) (dto.LoginResult, error)
	Register(context.Context, dto.RegisterRequest) (dto.LoginResult, error)
	DeleteUserAccount(context.Context, dto.DeleteUserAccountRequest) error
	ResetPassword(context.Context, dto.ResetPasswordRequest) (dto.LoginResult, error)
	UpdatePassword(context.Context, dto.UpdatePasswordRequest) (dto.LoginResult, error)
}

func NewServer(config config.Server) *Server {
	s := &Server{
		Config:         config,
		DB:             nil,
		Echo:           nil,
		Router:         nil,
		Mailer:         nil,
		Push:           nil,
		I18n:           nil,
		Clock:          nil,
		Auth:           nil,
		Local:          nil,
		AccountService: nil,
		BatchProcessor: nil,
	}

	return s
}

func (s *Server) Ready() bool {
	return s.DB != nil &&
		s.Echo != nil &&
		s.Router != nil &&
		s.Mailer != nil &&
		s.Push != nil &&
		s.I18n != nil &&
		s.Clock != nil &&
		s.Auth != nil &&
		s.Local != nil &&
		s.AccountService != nil &&
		s.NFTService != nil &&
		s.BatchProcessor != nil &&
		s.PaymentEventService != nil &&
		s.BatchOptimizer != nil &&
		s.QueueMonitor != nil
}

func (s *Server) InitCmd() *Server {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := s.InitDB(ctx); err != nil {
		cancel()
		log.Fatal().Err(err).Msg("Failed to initialize database")
	}
	cancel()

	if err := s.InitClock(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize clock")
	}

	if err := s.InitMailer(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize mailer")
	}

	if err := s.InitPush(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize push service")
	}

	if err := s.InitI18n(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize i18n service")
	}

	if err := s.InitAuthService(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize auth service")
	}

	if err := s.InitLocalService(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize local service")
	}

	if err := s.InitMetricsService(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize metrics service")
	}

	if err := s.InitChainsService(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize chains service")
	}

	if err := s.InitTokensService(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize tokens service")
	}

	if err := s.InitNFTService(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize NFT service")
	}

	if err := s.InitBatchProcessor(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize batch processor")
	}

	if err := s.InitAccountService(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize account service")
	}

	if err := s.InitPaymentEventService(); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize payment event service")
	}

	return s
}

func (s *Server) InitAuthService() error {
	s.Auth = auth.NewService(s.Config, s.DB, s.Clock)

	return nil
}

func (s *Server) InitLocalService() error {
	s.Local = local.NewService(s.Config, s.DB, s.Clock)

	return nil
}

func (s *Server) InitMetricsService() error {
	var err error
	s.Metrics, err = metrics.New(s.Config, s.DB)
	if err != nil {
		return err
	}

	return nil
}

func (s *Server) InitChainsService() error {
	s.ChainsService = chains.NewService(s.DB, s.Config.Blockchain)

	log.Info().Msg("Chains service initialized with blockchain configuration")
	return nil
}

func (s *Server) InitTokensService() error {
	s.TokensService = tokens.New(s.DB)

	log.Info().Msg("Tokens service initialized")
	return nil
}

func (s *Server) InitNFTService() error {
	s.NFTService = nft.NewService(s.DB, s.BatchProcessor)

	log.Info().Msg("NFT service initialized")
	return nil
}

func (s *Server) InitBatchProcessor() error {
	var err error

	// Initialize queue monitor first (will be passed to optimizer)
	s.QueueMonitor = queue.NewMonitor(nil) // Will set processor later

	// Initialize batch optimizer with chains service
	s.BatchOptimizer = queue.NewBatchOptimizer(s.QueueMonitor, s.ChainsService)

	// Initialize RabbitMQ client for payment event service
	if s.Config.RabbitMQ.Enabled {
		s.RabbitMQClient, err = queue.NewRabbitMQClient(s.Config.RabbitMQ)
		if err != nil {
			return fmt.Errorf("failed to create RabbitMQ client: %w", err)
		}

		s.BatchProcessor, err = queue.NewBatchProcessor(s.RabbitMQClient, s.DB, s.BatchOptimizer, s.Config)
		if err != nil {
			return fmt.Errorf("failed to create batch processor: %w", err)
		}

		// Update monitor with the actual processor
		s.QueueMonitor = queue.NewMonitor(s.BatchProcessor)
	}

	log.Info().
		Bool("rabbitmq_enabled", s.Config.RabbitMQ.Enabled).
		Bool("optimizer_enabled", s.BatchOptimizer != nil).
		Bool("monitor_enabled", s.QueueMonitor != nil).
		Msg("Batch processor with monitoring and optimization initialized")

	return nil
}

func (s *Server) InitAccountService() error {
	// Initialize account service with chains service dependency
	var err error
	s.AccountService, err = account.NewService(s.DB, s.Config.Blockchain, s.ChainsService)
	if err != nil {
		return fmt.Errorf("failed to create account service: %w", err)
	}

	// Load validated chains for logging
	validChains, err := s.ChainsService.GetValidatedChains(context.Background())
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load chains for logging")
		validChains = make(map[int64]*chains.ChainConfig) // empty map for logging
	}

	log.Info().Int("chains", len(validChains)).Msg("Account service initialized with chains service integration")

	return nil
}

func (s *Server) InitPaymentEventService() error {
	// Only initialize payment event service if RabbitMQ is enabled
	if !s.Config.RabbitMQ.Enabled {
		log.Info().Msg("RabbitMQ disabled, payment event service will not be initialized")
		// Create a dummy service to satisfy Ready() check
		s.PaymentEventService = &events.PaymentEventService{}
		return nil
	}

	// Initialize payment event service
	s.PaymentEventService = events.NewPaymentEventService(s.DB, s.RabbitMQClient)

	log.Info().Msg("Payment event service initialized")
	return nil
}

func (s *Server) InitDB(ctx context.Context) error {
	db, err := sql.Open("postgres", s.Config.Database.ConnectionString())
	if err != nil {
		return err
	}

	if s.Config.Database.MaxOpenConns > 0 {
		db.SetMaxOpenConns(s.Config.Database.MaxOpenConns)
	}
	if s.Config.Database.MaxIdleConns > 0 {
		db.SetMaxIdleConns(s.Config.Database.MaxIdleConns)
	}
	if s.Config.Database.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(s.Config.Database.ConnMaxLifetime)
	}

	if err := db.PingContext(ctx); err != nil {
		return err
	}

	s.DB = db

	return nil
}

func (s *Server) InitClock() error {
	s.Clock = time2.DefaultClock
	return nil
}

func (s *Server) InitMailer() error {
	switch config.MailerTransporter(s.Config.Mailer.Transporter) {
	case config.MailerTransporterMock:
		log.Warn().Msg("Initializing mock mailer")
		s.Mailer = mailer.New(s.Config.Mailer, transport.NewMock())
	case config.MailerTransporterSMTP:
		s.Mailer = mailer.New(s.Config.Mailer, transport.NewSMTP(s.Config.SMTP))
	default:
		return fmt.Errorf("Unsupported mail transporter: %s", s.Config.Mailer.Transporter)
	}

	return s.Mailer.ParseTemplates()
}

func (s *Server) InitPush() error {
	s.Push = push.New(s.DB)

	if s.Config.Push.UseFCMProvider {
		fcmProvider, err := provider.NewFCM(s.Config.FCMConfig)
		if err != nil {
			return err
		}
		s.Push.RegisterProvider(fcmProvider)
	}

	if s.Config.Push.UseMockProvider {
		log.Warn().Msg("Initializing mock push provider")
		mockProvider := provider.NewMock(push.ProviderTypeFCM)
		s.Push.RegisterProvider(mockProvider)
	}

	if s.Push.GetProviderCount() < 1 {
		log.Warn().Msg("No providers registered for push service")
	}

	return nil
}

func (s *Server) InitI18n() error {
	i18nService, err := i18n.New(s.Config.I18n)

	if err != nil {
		return err
	}

	s.I18n = i18nService

	return nil
}

func (s *Server) Start() error {
	if !s.Ready() {
		return errors.New("server is not ready")
	}

	// Start payment event service if enabled
	if s.PaymentEventService != nil && s.Config.RabbitMQ.Enabled {
		ctx := context.Background()
		if err := s.PaymentEventService.Start(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to start payment event service")
			// Continue with server startup even if payment service fails
		} else {
			log.Info().Msg("Payment event service started")
		}
	}

	if s.BatchProcessor != nil && s.Config.RabbitMQ.Enabled {
		if err := s.BatchProcessor.StartBatchConsumer(context.Background()); err != nil {
			log.Error().Err(err).Msg("Failed to start batch processor")
		} else {
			log.Info().Msg("Batch processor started")
		}
	}

	// Start adaptive batch optimization if optimizer is available
	if s.BatchOptimizer != nil {
		go func() {
			ctx := context.Background()
			// Run optimization every 5 minutes
			s.BatchOptimizer.StartAdaptiveOptimization(ctx, 5*time.Minute)
		}()
		log.Info().Msg("Adaptive batch optimization started")
	}

	// Start queue monitoring if monitor is available
	if s.QueueMonitor != nil {
		go func() {
			ctx := context.Background()
			// Run health checks every 2 minutes
			s.QueueMonitor.StartPeriodicHealthCheck(ctx, 2*time.Minute)
		}()
		log.Info().Msg("Queue monitoring started")
	}

	return s.Echo.Start(s.Config.Echo.ListenAddress)
}

func (s *Server) Shutdown(ctx context.Context) []error {
	log.Warn().Msg("Shutting down server")

	var errs []error

	if s.BatchProcessor != nil {
		if err := s.BatchProcessor.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close batch processor")
			errs = append(errs, err)
		} else {
			log.Info().Msg("Batch processor closed")
		}
	}

	// Stop payment event service first
	if s.PaymentEventService != nil && s.PaymentEventService.IsStarted() {
		log.Debug().Msg("Stopping payment event service")
		if err := s.PaymentEventService.Stop(); err != nil {
			log.Error().Err(err).Msg("Failed to stop payment event service")
			errs = append(errs, err)
		} else {
			log.Info().Msg("Payment event service stopped")
		}
	}

	// Close RabbitMQ client
	if s.RabbitMQClient != nil {
		log.Debug().Msg("Closing RabbitMQ client")
		if err := s.RabbitMQClient.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close RabbitMQ client")
			errs = append(errs, err)
		}
	}

	if s.DB != nil {
		log.Debug().Msg("Closing database connection")

		if err := s.DB.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			log.Error().Err(err).Msg("Failed to close database connection")
			errs = append(errs, err)
		}
	}

	if s.Echo != nil {
		log.Debug().Msg("Shutting down echo server")

		if err := s.Echo.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("Failed to shutdown echo server")
			errs = append(errs, err)
		}

	}

	return errs
}
