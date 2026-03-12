package server

import (
	"context"
	"net/http"
	"opentela/internal/common"
	"opentela/internal/common/process"
	"opentela/internal/metrics"
	"opentela/internal/protocol"
	solanaclient "opentela/internal/solana"
	"opentela/internal/wallet"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	p2phttp "github.com/libp2p/go-libp2p-http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/viper"
)

func StartServer() {
	// walletPubkey is passed to InitializeMyself so Peer.Owner is always the
	// raw wallet public key (used for access-control comparisons).
	// The human-readable ProviderID is derived inside InitializeMyself via wm.
	walletPubkey := ""
	var walletManager *wallet.WalletManager

	if viper.GetString("wallet.account") == "" {
		common.Logger.Debug("Wallet account not set, skipping wallet init")
	} else {
		var err error
		walletManager, err = wallet.InitializeWallet()
		if err != nil {
			common.Logger.Warn("Failed to initialize wallet: %v", err)
		} else {
			walletPublicKey := walletManager.GetPublicKey()
			providerID := walletManager.GetProviderID()
			common.Logger.Debugf("Wallet initialized: pubkey=%s provider=%s", walletPublicKey, providerID)

			if walletPublicKey == "" {
				common.Logger.Warn("No wallet public key available; ensure an account is created with `otela wallet create`")
			}

			if viper.GetString("wallet.account") == "" {
				viper.Set("wallet.account", walletPublicKey)
			}
			if walletPath := walletManager.GetWalletPath(); walletPath != "" && viper.GetString("account.wallet") == "" {
				viper.Set("account.wallet", walletPath)
			}

			walletType := walletManager.GetWalletType()
			if walletType == wallet.WalletTypeSolana {
				common.Logger.Debug("Wallet type: solana")
			} else {
				common.Logger.Debug("Wallet type: ocf")
			}

			configuredAccount := viper.GetString("wallet.account")
			if configuredAccount != "" && configuredAccount != walletPublicKey {
				common.Logger.Warn("Configured wallet.account (%s) does not match local wallet public key (%s)", configuredAccount, walletPublicKey)
			}
			if configuredAccount != "" {
				common.Logger.Debug("Configured wallet.account matches local wallet")
			}

			// Owner must always be the wallet public key so that access-control
			// decisions (which compare against wallet.account) are like-for-like.
			// The ProviderID ("otela-...") is stored separately in Peer.ProviderID
			// by InitializeMyself via wm.GetProviderID().
			if configuredAccount != "" {
				walletPubkey = configuredAccount
			} else {
				walletPubkey = walletPublicKey
			}

			if walletType == wallet.WalletTypeSolana {
				mint := viper.GetString("solana.mint")
				skipVerification := viper.GetBool("solana.skip_verification")
				if mint != "" && !skipVerification {
					rpcEndpoint := viper.GetString("solana.rpc")
					client := solanaclient.NewClient(rpcEndpoint)
					verifyCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					// Use the raw public key for on-chain verification,
					// not the provider ID.
					verifyAddr := walletPublicKey
					if configuredAccount != "" {
						verifyAddr = configuredAccount
					}
					hasToken, err := client.HasSPLToken(verifyCtx, verifyAddr, mint)
					cancel()
					if err != nil {
						common.Logger.Warn("Failed to verify SPL token ownership: %v", err)
					} else if !hasToken {
						common.Logger.Warn("Solana wallet %s does not hold SPL mint %s", verifyAddr, mint)
					} else {
						common.Logger.Debugf("SPL token ownership verified: mint=%s", mint)
					}
				} else if mint != "" && skipVerification {
					common.Logger.Warn("Skipping Solana token ownership verification as requested")
				}
			}
		}
	}

	protocol.InitializeMyself(walletPubkey, walletManager)
	_, cancelCtx := protocol.GetCRDTStore()
	defer cancelCtx()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	defer stop()

	// Metrics aggregation: scrape worker /metrics via libp2p and serve aggregated
	if viper.GetBool("metrics.aggregation_enabled") {
		node, _ := protocol.GetP2PNode(nil)
		scrapeTransport := &http.Transport{
			ResponseHeaderTimeout: time.Duration(viper.GetInt("metrics.scrape_timeout_seconds")) * time.Second,
			IdleConnTimeout:       30 * time.Second,
			MaxIdleConns:          50,
			MaxIdleConnsPerHost:   2,
		}
		scrapeTransport.RegisterProtocol("libp2p", p2phttp.NewTransport(node))

		cfg := metrics.ScraperConfig{
			ScrapeInterval: time.Duration(viper.GetInt("metrics.scrape_interval_seconds")) * time.Second,
			ScrapeTimeout:  time.Duration(viper.GetInt("metrics.scrape_timeout_seconds")) * time.Second,
			MetricsPath:    viper.GetString("metrics.worker_metrics_path"),
			MaxConcurrent:  viper.GetInt("metrics.max_concurrent_scrapes"),
		}
		provider := &metrics.NodeTablePeerProvider{}
		scraper := metrics.NewMetricsScraper(cfg, provider, scrapeTransport)
		metricsCollector := metrics.NewAggregatedCollector(scraper)
		prometheus.MustRegister(metricsCollector)
		for _, c := range scraper.GetSelfMetrics() {
			prometheus.MustRegister(c)
		}
		scraper.Start(cfg.ScrapeInterval)

		// Periodically update network stats gauges
		go func() {
			ticker := time.NewTicker(cfg.ScrapeInterval)
			defer ticker.Stop()
			for range ticker.C {
				connected := protocol.GetConnectedPeers()
				all := protocol.GetAllPeers()
				if connected != nil && all != nil {
					metricsCollector.SetNetworkStats(len(*connected), len(*all))
				}
				metricsCollector.SetScraperTargets(len(provider.GetScrapablePeers()))
			}
		}()

		common.Logger.Infof("Metrics aggregation enabled: scraping workers every %ds", viper.GetInt("metrics.scrape_interval_seconds"))
	}

	initTracer()
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.Use(corsHeader())
	r.Use(rateLimitMiddleware())
	r.Use(gin.Recovery())
	// Initialize OpenAPI/Swagger documentation
	r.GET("/openapi.yaml", func(c *gin.Context) {
		c.Header("Content-Type", "application/yaml")
		c.File("./internal/server/openapi.yaml")
	})
	r.GET("/swagger", func(c *gin.Context) {
		c.HTML(http.StatusOK, "swagger.html", gin.H{
			"openapiUrl": "/openapi.yaml",
		})
	})

	// Prometheus metrics
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	go protocol.StartTicker()
	subProcess := viper.GetString("subprocess")
	if subProcess != "" {
		go process.StartCriticalProcess(subProcess)
	}
	v1 := r.Group("/v1")
	{
		v1.GET("/health", healthStatusCheck)
		systemGroup := v1.Group("/system")
		{
			systemGroup.GET("/stats", getIngestStats)
		}
		crdtGroup := v1.Group("/dnt")
		{
			crdtGroup.GET("/table", getDNT)
			crdtGroup.GET("/peers", listPeers)
			crdtGroup.GET("/peers_status", listPeersWithStatus)
			crdtGroup.GET("/bootstraps", listBootstraps)
			crdtGroup.GET("/stats", getResourceStats) // Add resource manager stats endpoint
			crdtGroup.POST("/_node", updateLocal)
			crdtGroup.DELETE("/_node", deleteLocal)
		}
		p2pGroup := v1.Group("/p2p")
		{
			p2pGroup.PATCH("/:peerId/*path", P2PForwardHandler)
			p2pGroup.POST("/:peerId/*path", P2PForwardHandler)
			p2pGroup.PUT("/:peerId/*path", P2PForwardHandler)
			p2pGroup.GET("/:peerId/*path", P2PForwardHandler)
			p2pGroup.DELETE("/:peerId/*path", P2PForwardHandler)
		}
		globalServiceGroup := v1.Group("/service")
		{
			globalServiceGroup.GET("/:service/*path", GlobalServiceForwardHandler)
			globalServiceGroup.POST("/:service/*path", GlobalServiceForwardHandler)
			globalServiceGroup.PUT("/:service/*path", GlobalServiceForwardHandler)
			globalServiceGroup.PATCH("/:service/*path", GlobalServiceForwardHandler)
			globalServiceGroup.DELETE("/:service/*path", GlobalServiceForwardHandler)
		}
		serviceGroup := v1.Group("/_service")
		serviceGroup.Use(accessControlMiddleware())
		{
			serviceGroup.GET("/:service/*path", ServiceForwardHandler)
			serviceGroup.POST("/:service/*path", ServiceForwardHandler)
			serviceGroup.PUT("/:service/*path", ServiceForwardHandler)
			serviceGroup.PATCH("/:service/*path", ServiceForwardHandler)
			serviceGroup.DELETE("/:service/*path", ServiceForwardHandler)
		}
	}
	p2plistener := P2PListener()
	srv := &http.Server{
		Addr:    "0.0.0.0:" + viper.GetString("port"),
		Handler: r,
	}
	go func() {
		err := http.Serve(p2plistener, r)
		if err != nil {
			common.Logger.Errorf("http.Serve: %s", err)
		}
	}()
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			common.ReportError(err, "Server failed to start")
		}
	}()
	go func() {
		protocol.RegisterLocalServices()
	}()

	// Startup banner
	hasBootstrap := len(protocol.ConnectedPeers()) > 0
	common.Logger.Infof("Server started: id=%s bootstrap_connected=%v", protocol.MyID, hasBootstrap)

	<-ctx.Done()
	// shutting down...
	protocol.AnnounceLeave()
	protocol.ClearCRDTStore()
	time.Sleep(5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	common.Logger.Info("Shutting down")
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		common.ReportError(err, "Server shutdown failed")
	}
	common.Logger.Info("Server exiting")
}
