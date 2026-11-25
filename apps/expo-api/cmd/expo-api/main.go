package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"factorio-expo/apps/expo-api/internal/accountdeletion"
	"factorio-expo/apps/expo-api/internal/accounts"
	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/auth"
	"factorio-expo/apps/expo-api/internal/banhold"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/config"
	"factorio-expo/apps/expo-api/internal/db"
	dbgen "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/edgecache"
	"factorio-expo/apps/expo-api/internal/httpapi"
	"factorio-expo/apps/expo-api/internal/jobs"
	"factorio-expo/apps/expo-api/internal/maintenance"
	"factorio-expo/apps/expo-api/internal/preview"
	"factorio-expo/apps/expo-api/internal/previewimage"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/ratelimit"
	"factorio-expo/apps/expo-api/internal/runner"
	"factorio-expo/apps/expo-api/internal/search"
	"factorio-expo/apps/expo-api/internal/sessions"
	"factorio-expo/apps/expo-api/internal/staff"
	"factorio-expo/apps/expo-api/internal/staffops"
	"factorio-expo/apps/expo-api/internal/storage"
	"factorio-expo/apps/expo-api/internal/views"

	"github.com/jackc/pgx/v5/pgxpool"
)

// sessionTTL is how long a sign-in lasts (the session row and its cookie).
const sessionTTL = 7 * 24 * time.Hour

type service struct {
	httpServer *http.Server
	// adminServer is the staff API listener; nil when API_ADMIN_ADDR is unset.
	adminServer *http.Server
	pool        *pgxpool.Pool
	runnerURL   string
	previews    *preview.Dispatcher
	search      *search.Keeper
	views       *views.Flusher
	maintenance *maintenance.Registry
	// limiter sweeps its counters in the background; nil with rate limiting off.
	limiter *ratelimit.Limiter
	// stopBackground stops the background loops on shutdown; background
	// waits for every one of them, so none writes after the pool closes.
	stopBackground context.CancelFunc
	background     sync.WaitGroup
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if len(os.Args) > 1 && os.Args[1] == "openapi" {
		os.Exit(writeOpenAPI(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "replay-deletions" {
		os.Exit(replayDeletions())
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		slog.Error("load environment", "error", err)
		os.Exit(1)
	}
	if err := previewimage.Check(); err != nil {
		slog.Error("preview encoder", "error", err)
		os.Exit(1)
	}

	svc, err := createService(context.Background(), cfg)
	if err != nil {
		slog.Error("create service", "error", err)
		os.Exit(1)
	}
	defer svc.Close()

	if err := svc.Start(); err != nil {
		slog.Error("start service", "error", err)
		os.Exit(1)
	}
}

func createService(ctx context.Context, cfg config.Config) (*service, error) {
	sign, err := newSignIn(cfg)
	if err != nil {
		return nil, err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	store, err := storage.NewLocal(cfg.StorageDir)
	if err != nil {
		return nil, fmt.Errorf("open API_STORAGE_DIR: %w", err)
	}
	queries := dbgen.New(pool)
	previewFiles := preview.NewFiles(store, queries)

	// The timeout also bounds a preview PNG download (at most 80 MB, over
	// the internal network).
	runnerClient := runner.NewClient(cfg.RunnerURL, 30*time.Second)
	// One process serves every stream, so progress
	// signals travel in memory from the catalog and the dispatcher to the
	// event streams.
	progressHub := progress.NewHub()
	// The catalog wakes the dispatcher after committing new intents; the
	// dispatcher imports through the catalog, so it is bound once both exist.
	var previews *preview.Dispatcher
	// Ban holds share the IP ban secret (a key derived from it).
	banHolds := banhold.New(cfg.IPHashSecret)
	// Preview images and raw strings sit in the CDN cache; purge them when
	// an entry goes private or away. Off without API_CLOUDFLARE_API_TOKEN and _ZONE_ID.
	edge := edgecache.New(cfg.SiteURL, cfg.CloudflareToken, cfg.CloudflareZone)
	var entryHidden func(string, []string, []string)
	if edge.Enabled() {
		entryHidden = edge.PurgeEntry
	}
	catalogService := catalog.New(pool, catalog.Options{
		PreviewTTL:            cfg.QuickPreviewTTL,
		IPBanDuration:         cfg.IPBanDuration,
		ChallengeScoresTTL:    cfg.ChallengeScoresTTL,
		TagSuggestionMinScore: cfg.TagMinScore,
		Progress:              progressHub,
		WorkQueued:            func() { previews.Nudge() },
		EntryHidden:           entryHidden,
		BanHolds:              banHolds,
		RenderQueue:           cfg.RenderQueue,
	})
	if err := catalogService.LoadTagSuggestionRules(ctx); err != nil {
		return nil, err
	}
	previews = preview.New(pool, runnerClient, previewFiles, catalogService, preview.Options{
		PollInterval: cfg.PollInterval, Progress: progressHub,
		Encoders: cfg.Encoders, Queue: cfg.RenderQueue,
		Budget: preview.Budget{
			MaxBytes: cfg.StorageMaxBytes, MinFreeBytes: cfg.StorageMinFreeBytes, FreeBytes: store.FreeBytes,
		},
	})
	ledger, err := accountdeletion.NewLedger(cfg.DeletedUsersFile)
	if err != nil {
		return nil, fmt.Errorf("open API_DELETED_USERS_FILE: %w", err)
	}
	deletion := accountdeletion.New(catalogService, banHolds).WithLedger(ledger)
	staffOps := staffops.New(catalogService, deletion)
	auditLog := audit.NewLog(queries)
	requeuer := preview.NewRequeuer(queries, catalogService, progressHub, previews.Nudge)
	previewRenders := preview.NewRenders(previewFiles, requeuer, catalogService)

	searchClient := search.NewTypesenseClient(cfg.TypesenseURL, cfg.TypesenseAPIKey, nil)
	syncer := search.NewSyncer(catalogService, queries, searchClient)
	searchDispatcher := search.New(pool, syncer, search.Options{PollInterval: cfg.SearchPollInterval})
	searchKeeper := search.NewKeeper(searchClient, syncer, searchDispatcher, queries, search.KeeperOptions{
		ViewInterval: cfg.SearchViewInterval,
	})
	viewCounter := views.NewCounter(views.Options{})

	var limiter *ratelimit.Limiter
	if cfg.RateLimit {
		limiter = ratelimit.New(200_000)
	} else {
		slog.Warn("rate limiting is disabled (API_RATE_LIMIT=false)")
	}

	sessionService := sessions.New(pool, auth.NewTokenIssuer(cfg.JWTSecret, cfg.JWTSecretPrevious), sessionTTL)
	handler := httpapi.New(httpapi.Dependencies{
		AllowedOrigin:   cfg.AllowedOrigin,
		SiteURL:         cfg.SiteURL,
		CookieSecure:    cfg.CookieSecure,
		ClientIPs:       cfg.ClientIPs,
		Limiter:         limiter,
		Runner:          runnerClient,
		Pool:            pool,
		Sessions:        sessionService,
		Flows:           auth.NewFlowSigner(cfg.JWTSecret, cfg.JWTSecretPrevious),
		Accounts:        accounts.New(pool, banHolds),
		Identities:      sign.identities,
		Passkeys:        sign.passkeys,
		Turnstile:       sign.turnstile,
		Catalog:         catalogService,
		AccountDeletion: deletion,
		PreviewFiles:    previewFiles,
		Renders:         previewRenders,
		Progress:        progressHub,
		Search:          searchClient,
		Importer:        newImporter(cfg),
		Views:           viewCounter,
		IPHashSecret:    cfg.IPHashSecret,
		RenderQueue:     cfg.RenderQueue,
	})

	housekeeping := maintenanceRegistry(maintenanceDeps{
		catalog: catalogService, ops: staffOps, audit: auditLog, deletion: deletion, sessions: sessionService, files: previewFiles, queries: queries, previews: previews,
	})
	var adminServer *http.Server
	if cfg.AdminAddr != "" {
		authenticator, err := staff.NewAuthenticator(cfg.AdminToken)
		if err != nil {
			return nil, fmt.Errorf("staff authenticator: %w", err)
		}
		adminServer = newHTTPServer(cfg.AdminAddr, httpapi.NewAdmin(httpapi.AdminDependencies{
			Staff: authenticator, Pool: pool, Runner: runnerClient, Catalog: catalogService, Ops: staffOps,
			Cleanups: housekeeping,
			Jobs:     jobs.New(queries, requeuer, auditLog, searchDispatcher.Nudge),
		}))
	}

	return &service{
		httpServer:  newHTTPServer(cfg.HTTPAddr, handler),
		adminServer: adminServer,
		pool:        pool,
		runnerURL:   cfg.RunnerURL,
		previews:    previews,
		search:      searchKeeper,
		views:       views.NewFlusher(viewCounter, queries, cfg.ViewFlushInterval),
		maintenance: housekeeping,
		limiter:     limiter,
	}, nil
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
}

// Start begins serving HTTP and runs the background loops alongside it, all
// joined by Close: the preview dispatcher (renders finished by the runner are
// picked up without a client asking), the search keeper (it builds the search
// index in the background, which takes as long as the catalog is big, and
// runs the search dispatcher once the index is live), the view flusher and
// the maintenance schedule.
func (s *service) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	s.stopBackground = cancel
	s.background.Go(func() { s.previews.Run(ctx) })
	s.background.Go(func() { s.search.Run(ctx) })
	s.background.Go(func() { s.views.Run(ctx) })
	s.background.Go(func() { s.maintenance.Schedule(ctx) })
	if s.limiter != nil {
		s.background.Go(func() { s.limiter.Run(time.Minute, ctx.Done()) })
	}
	go s.shutdownOnSignal()
	if s.adminServer != nil {
		go s.serveAdmin()
	}

	slog.Info("api listening", "address", s.httpServer.Addr, "runner_url", s.runnerURL)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve API HTTP: %w", err)
	}
	return nil
}

// serveAdmin serves the staff API until shutdown. It failing to listen
// stops the whole API, so a misconfigured admin port is noticed at once.
func (s *service) serveAdmin() {
	slog.Info("admin api listening", "address", s.adminServer.Addr)
	if err := s.adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("serve admin HTTP", "error", err)
		_ = s.httpServer.Close()
	}
}

// shutdownOnSignal stops serving on SIGTERM or SIGINT, so Start returns and
// Close runs (open event streams get a few seconds, then are cut).
func (s *service) shutdownOnSignal() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	<-ctx.Done()
	slog.Info("api shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if s.adminServer != nil {
		if err := s.adminServer.Shutdown(shutdownCtx); err != nil {
			_ = s.adminServer.Close()
		}
	}
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		_ = s.httpServer.Close()
	}
}

// Close stops the background loops and waits for them, so the last page views
// are written and no task touches the pool, then closes the pool.
func (s *service) Close() {
	if s.stopBackground != nil {
		s.stopBackground()
	}
	s.background.Wait()
	s.pool.Close()
}
