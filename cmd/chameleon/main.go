package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"chameleonnet/internal/config"
	"chameleonnet/internal/metrics"
	"chameleonnet/internal/pool"
	"chameleonnet/internal/proxy"
	"chameleonnet/internal/tunnel"
)

var version = "0.1.0"

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Printf("ChameleonNet v%s starting (Go %s, %d CPUs)", version, runtime.Version(), runtime.NumCPU())

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	log.Printf("mode=%s listen=%s target=%s profile=%s maxconn=%d bufsize=%d",
		cfg.Mode.String(),
		cfg.ListenAddr,
		cfg.RemoteAddr,
		cfg.Profile.String(),
		cfg.MaxConnections,
		cfg.BufferSize,
	)

	runtime.GOMAXPROCS(runtime.NumCPU())

	prev := debug.SetGCPercent(200)
	_ = prev

	bp := pool.NewBufferPool()
	met := metrics.NewProxyMetrics()

	if err := printVersionBanner(); err != nil {
	}

	var p proxy.Server
	switch cfg.Mode {
	case config.ModeClient:
		p = proxy.NewProxy(cfg, bp, met)
	case config.ModeServer:
		p = tunnel.NewServer(cfg)
	default:
		log.Fatalf("unknown mode: %v", cfg.Mode)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("received signal %v, initiating graceful shutdown", sig)
		cancel()
		go func() {
			time.Sleep(5 * time.Second)
			log.Printf("shutdown timeout reached, forcing exit")
			os.Exit(1)
		}()
	}()

	go metricsLoop(ctx, cfg, met, bp)

	if cfg.Mode == config.ModeClient {
		log.Printf("client proxy listening on %s → relay %s", cfg.ListenAddr, cfg.RemoteAddr)
	} else {
		log.Printf("server relay listening on %s", cfg.ListenAddr)
	}

	if err := p.ListenAndServe(ctx); err != nil {
		if err == context.Canceled {
			log.Println("server stopped gracefully")
		} else {
			log.Printf("server error: %v", err)
		}
	}

	log.Println("draining active connections...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := p.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}

	log.Println("ChameleonNet stopped")

	snap := met.Snapshot()
	fmt.Fprintf(os.Stderr, "\n=== Final Metrics ===\n")
	fmt.Fprintf(os.Stderr, "Uptime:        %s\n", snap.Uptime.Round(time.Second))
	fmt.Fprintf(os.Stderr, "Bytes Up:      %s\n", formatBytes(snap.BytesUp))
	fmt.Fprintf(os.Stderr, "Bytes Down:    %s\n", formatBytes(snap.BytesDown))
	fmt.Fprintf(os.Stderr, "Total Conn:    %d\n", snap.TotalConns)
	fmt.Fprintf(os.Stderr, "=====================\n")
}

func metricsLoop(ctx context.Context, cfg *config.Config, met *metrics.ProxyMetrics, bp *pool.BufferPool) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	var prevTotalAlloc uint64
	var prevPauseTotalNs uint64
	var poolTracker metrics.PoolAllocationTracker

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			allocDelta := m.TotalAlloc - prevTotalAlloc
			gcPauseDelta := m.PauseTotalNs - prevPauseTotalNs
			prevTotalAlloc = m.TotalAlloc
			prevPauseTotalNs = m.PauseTotalNs

			snap := met.Snapshot()

			gcFraction := float64(gcPauseDelta) / float64(60e9) * 100

			poolAllocs := bp.TotalAllocated()
			poolInUse := bp.TotalInUse()
			poolTracker.Observe(poolAllocs)

			fmt.Fprintf(os.Stderr, "\n=== ChameleonNet Metrics [%s] ===\n",
				time.Now().Format("15:04:05"))
			fmt.Fprintf(os.Stderr, "Traffic:\n")
			fmt.Fprintf(os.Stderr, "  Bytes Up:      %s (%d pkts)\n",
				formatBytes(snap.BytesUp), snap.PacketsUp)
			fmt.Fprintf(os.Stderr, "  Bytes Down:    %s (%d pkts)\n",
				formatBytes(snap.BytesDown), snap.PacketsDown)
			fmt.Fprintf(os.Stderr, "  Chaff Sent:    %s\n",
				formatBytes(snap.ChaffSent))
			fmt.Fprintf(os.Stderr, "  Chaff Filtered: %d pkts\n",
				snap.PacketsChaff)
			fmt.Fprintf(os.Stderr, "  Total:         %s (%d pkts)\n",
				formatBytes(snap.BytesTotal), snap.PacketsTotal)

			fmt.Fprintf(os.Stderr, "Connections:\n")
			fmt.Fprintf(os.Stderr, "  Active:        %d\n", snap.ActiveConns)
			fmt.Fprintf(os.Stderr, "  Total:         %d\n", snap.TotalConns)

			fmt.Fprintf(os.Stderr, "Memory:\n")
			fmt.Fprintf(os.Stderr, "  HeapInuse:     %s\n",
				formatBytes(int64(m.HeapInuse)))
			fmt.Fprintf(os.Stderr, "  HeapAlloc:     %s\n",
				formatBytes(int64(m.HeapAlloc)))
			fmt.Fprintf(os.Stderr, "  Sys:           %s\n",
				formatBytes(int64(m.Sys)))
			fmt.Fprintf(os.Stderr, "  AllocDelta:    %s/60s\n",
				formatBytes(int64(allocDelta)))
			fmt.Fprintf(os.Stderr, "  NumGC:         %d\n", m.NumGC)
			fmt.Fprintf(os.Stderr, "  GC Pause:      %.1f%% (%.2fms/60s)\n",
				gcFraction, float64(gcPauseDelta)/float64(time.Millisecond))

			fmt.Fprintf(os.Stderr, "Buffer Pool:\n")
			fmt.Fprintf(os.Stderr, "  Total Allocs:  %d\n", poolAllocs)
			fmt.Fprintf(os.Stderr, "  Currently InUse: %d\n", poolInUse)
			fmt.Fprintf(os.Stderr, "  PeakDelta:     %d\n", poolTracker.PeakDelta())
			fmt.Fprintf(os.Stderr, "  Spikes:        %d\n", poolTracker.SpikeCount())

			fmt.Fprintf(os.Stderr, "System:\n")
			fmt.Fprintf(os.Stderr, "  Goroutines:    %d\n", runtime.NumGoroutine())
			fmt.Fprintf(os.Stderr, "  CGO Calls:     %d\n", runtime.NumCgoCall())
			fmt.Fprintf(os.Stderr, "  Errors:        %d\n", snap.Errors)
			fmt.Fprintf(os.Stderr, "  Uptime:        %s\n",
				snap.Uptime.Round(time.Second))
			fmt.Fprintf(os.Stderr, "============================\n\n")
		}
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GiB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.2f KiB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func printVersionBanner() error {
	fmt.Fprintf(os.Stderr, `ChameleonNet v%s
  Go:      %s
  Arch:    %s/%s
  PID:     %d
`, version, runtime.Version(), runtime.GOOS, runtime.GOARCH, os.Getpid())
	return nil
}
