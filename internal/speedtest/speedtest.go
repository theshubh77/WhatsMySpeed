// Package speedtest implements a multi-stream HTTP latency, download, and upload speed test.
package speedtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Phase represents the current stage of a speed test run.
type Phase string

const (
	PhaseIdle      Phase = "idle"
	PhasePing      Phase = "ping"
	PhaseDownload  Phase = "download"
	PhaseUpload    Phase = "upload"
	PhaseDone      Phase = "done"
	PhaseCancelled Phase = "cancelled"
	PhaseError     Phase = "error"
)

// Progress carries live metrics and completion percentage during a test run.
type Progress struct {
	Phase        Phase
	Percent      int
	PingMs       float64
	DownloadMbps float64
	UploadMbps   float64
	ServerLabel  string
	StatusText   string
	Error        string
}

// Options configures endpoints, stream concurrency, and durations.
type Options struct {
	BaseURL          string
	PingCount        int
	DownloadDuration time.Duration
	UploadDuration   time.Duration
	Streams          int
	DownloadBytes    int64
	UploadChunkBytes int64
	Warmup           time.Duration
	Client           *http.Client
}

// DefaultOptions returns production settings targeting Cloudflare Edge speed endpoints.
func DefaultOptions() Options {
	return Options{
		BaseURL:          "https://speed.cloudflare.com",
		PingCount:        5,
		DownloadDuration: 6 * time.Second,
		UploadDuration:   6 * time.Second,
		Streams:          4,
		DownloadBytes:    25_000_000,
		UploadChunkBytes: 1_000_000,
		Warmup:           600 * time.Millisecond,
	}
}

func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.BaseURL == "" {
		o.BaseURL = d.BaseURL
	}
	o.BaseURL = strings.TrimRight(o.BaseURL, "/")
	if o.PingCount <= 0 {
		o.PingCount = d.PingCount
	}
	if o.DownloadDuration <= 0 {
		o.DownloadDuration = d.DownloadDuration
	}
	if o.UploadDuration <= 0 {
		o.UploadDuration = d.UploadDuration
	}
	if o.Streams <= 0 {
		o.Streams = d.Streams
	}
	if o.DownloadBytes <= 0 {
		o.DownloadBytes = d.DownloadBytes
	}
	if o.UploadChunkBytes <= 0 {
		o.UploadChunkBytes = d.UploadChunkBytes
	}
	if o.Warmup < 0 {
		o.Warmup = 0
	}
	if o.Client == nil {
		tr := &http.Transport{
			MaxIdleConns:        o.Streams * 2,
			MaxIdleConnsPerHost: o.Streams * 2,
			IdleConnTimeout:     30 * time.Second,
		}
		o.Client = &http.Client{
			Transport: tr,
			Timeout:   30 * time.Second,
		}
	}
	return o
}

// Run executes Ping, multi-stream Download, and multi-stream Upload sequentially.
func Run(ctx context.Context, opts Options, onProgress func(Progress)) Progress {
	opts = opts.withDefaults()
	emit := func(p Progress) {
		if onProgress != nil {
			onProgress(p)
		}
	}

	state := Progress{
		Phase:       PhasePing,
		Percent:     2,
		ServerLabel: "Cloudflare Edge",
		StatusText:  "Connecting & measuring latency...",
	}
	emit(state)

	pingMs, pop, err := measurePing(ctx, opts, func(step int, curMedian float64) {
		state.Percent = 2 + (step*13)/opts.PingCount
		if curMedian > 0 {
			state.PingMs = curMedian
		}
		emit(state)
	})
	if pop != "" {
		state.ServerLabel = fmt.Sprintf("Cloudflare (%s)", pop)
	}
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			state.Phase = PhaseCancelled
			state.StatusText = "Speed test cancelled"
			emit(state)
			return state
		}
		state.Phase = PhaseError
		state.Error = err.Error()
		state.StatusText = "Speed test failed"
		emit(state)
		return state
	}
	state.PingMs = pingMs
	state.Percent = 15
	state.Phase = PhaseDownload
	state.StatusText = fmt.Sprintf("Testing download (%d streams)...", opts.Streams)
	emit(state)

	dlMbps, err := measureDownload(ctx, opts, func(frac float64, curMbps float64) {
		state.Percent = 15 + int(frac*42.0)
		state.DownloadMbps = curMbps
		emit(state)
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			state.Phase = PhaseCancelled
			state.StatusText = "Speed test cancelled"
			emit(state)
			return state
		}
		state.Phase = PhaseError
		state.Error = err.Error()
		state.StatusText = "Download test failed"
		emit(state)
		return state
	}
	state.DownloadMbps = dlMbps
	state.Percent = 57
	state.Phase = PhaseUpload
	state.StatusText = fmt.Sprintf("Testing upload (%d streams)...", opts.Streams)
	emit(state)

	ulMbps, err := measureUpload(ctx, opts, func(frac float64, curMbps float64) {
		state.Percent = 57 + int(frac*42.0)
		state.UploadMbps = curMbps
		emit(state)
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			state.Phase = PhaseCancelled
			state.StatusText = "Speed test cancelled"
			emit(state)
			return state
		}
		state.Phase = PhaseError
		state.Error = err.Error()
		state.StatusText = "Upload test failed"
		emit(state)
		return state
	}

	state.UploadMbps = ulMbps
	state.Percent = 100
	state.Phase = PhaseDone
	state.StatusText = "Completed"
	emit(state)
	return state
}

func parseCloudflarePOP(hdr http.Header) string {
	ray := hdr.Get("CF-RAY")
	if ray == "" {
		return ""
	}
	idx := strings.LastIndex(ray, "-")
	if idx < 0 || idx+1 >= len(ray) {
		return ""
	}
	return strings.TrimSpace(ray[idx+1:])
}

func measurePing(ctx context.Context, opts Options, onStep func(int, float64)) (float64, string, error) {
	url := fmt.Sprintf("%s/__down?bytes=0", opts.BaseURL)

	// Warm-up request establishes DNS + TCP + TLS session so pings measure true RTT.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := opts.Client.Do(req)
	if err != nil {
		return 0, "", err
	}
	pop := parseCloudflarePOP(resp.Header)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, pop, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	samples := make([]float64, 0, opts.PingCount)
	for i := 0; i < opts.PingCount; i++ {
		if ctx.Err() != nil {
			return 0, pop, ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, pop, err
		}
		t0 := time.Now()
		resp, err := opts.Client.Do(req)
		if err != nil {
			return 0, pop, err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		rttMs := float64(time.Since(t0).Microseconds()) / 1000.0
		samples = append(samples, rttMs)
		if onStep != nil {
			onStep(i+1, medianFloat64(samples))
		}
	}

	return medianFloat64(samples), pop, nil
}

func medianFloat64(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	cp := append([]float64(nil), vals...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2.0
}

func measureDownload(ctx context.Context, opts Options, onUpdate func(frac, mbps float64)) (float64, error) {
	phaseCtx, cancel := context.WithTimeout(ctx, opts.DownloadDuration)
	defer cancel()

	var totalBytes atomic.Int64
	var firstErr atomic.Value
	url := fmt.Sprintf("%s/__down?bytes=%d", opts.BaseURL, opts.DownloadBytes)

	var wg sync.WaitGroup
	for i := 0; i < opts.Streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 32*1024)
			for phaseCtx.Err() == nil {
				req, err := http.NewRequestWithContext(phaseCtx, http.MethodGet, url, nil)
				if err != nil {
					firstErr.CompareAndSwap(nil, err)
					return
				}
				resp, err := opts.Client.Do(req)
				if err != nil {
					if phaseCtx.Err() == nil {
						firstErr.CompareAndSwap(nil, err)
					}
					return
				}
				if resp.StatusCode >= 400 {
					_ = resp.Body.Close()
					firstErr.CompareAndSwap(nil, fmt.Errorf("download HTTP %d", resp.StatusCode))
					return
				}
				for {
					n, readErr := resp.Body.Read(buf)
					if n > 0 {
						totalBytes.Add(int64(n))
					}
					if readErr != nil {
						break
					}
				}
				_ = resp.Body.Close()
			}
		}()
	}

	finalMbps := trackThroughput(phaseCtx, opts.DownloadDuration, opts.Warmup, &totalBytes, onUpdate)
	wg.Wait()

	if ctx.Err() != nil {
		return finalMbps, ctx.Err()
	}
	if totalBytes.Load() == 0 {
		if v := firstErr.Load(); v != nil {
			return 0, v.(error)
		}
		return 0, errors.New("no download bytes received")
	}
	return finalMbps, nil
}

type zeroChunkReader struct {
	remaining int64
	counter   *atomic.Int64
}

func (r *zeroChunkReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	clear(p[:n])
	r.remaining -= int64(n)
	r.counter.Add(int64(n))
	return n, nil
}

func measureUpload(ctx context.Context, opts Options, onUpdate func(frac, mbps float64)) (float64, error) {
	phaseCtx, cancel := context.WithTimeout(ctx, opts.UploadDuration)
	defer cancel()

	var totalBytes atomic.Int64
	var firstErr atomic.Value
	url := fmt.Sprintf("%s/__up", opts.BaseURL)

	var wg sync.WaitGroup
	for i := 0; i < opts.Streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for phaseCtx.Err() == nil {
				body := &zeroChunkReader{
					remaining: opts.UploadChunkBytes,
					counter:   &totalBytes,
				}
				req, err := http.NewRequestWithContext(phaseCtx, http.MethodPost, url, body)
				if err != nil {
					firstErr.CompareAndSwap(nil, err)
					return
				}
				req.ContentLength = opts.UploadChunkBytes
				req.Header.Set("Content-Type", "application/octet-stream")
				resp, err := opts.Client.Do(req)
				if err != nil {
					if phaseCtx.Err() == nil {
						firstErr.CompareAndSwap(nil, err)
					}
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode >= 400 {
					firstErr.CompareAndSwap(nil, fmt.Errorf("upload HTTP %d", resp.StatusCode))
					return
				}
			}
		}()
	}

	finalMbps := trackThroughput(phaseCtx, opts.UploadDuration, opts.Warmup, &totalBytes, onUpdate)
	wg.Wait()

	if ctx.Err() != nil {
		return finalMbps, ctx.Err()
	}
	if totalBytes.Load() == 0 {
		if v := firstErr.Load(); v != nil {
			return 0, v.(error)
		}
		return 0, errors.New("no upload bytes transmitted")
	}
	return finalMbps, nil
}

func trackThroughput(
	phaseCtx context.Context,
	totalDur time.Duration,
	warmup time.Duration,
	totalBytes *atomic.Int64,
	onUpdate func(frac, mbps float64),
) float64 {
	start := time.Now()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	var warmupDone bool
	var warmupBytes int64
	var warmupTime time.Time

	calcMbps := func(now time.Time) float64 {
		curBytes := totalBytes.Load()
		elapsed := now.Sub(start)
		if !warmupDone && elapsed >= warmup {
			warmupDone = true
			warmupBytes = curBytes
			warmupTime = now
		}
		if warmupDone {
			dt := now.Sub(warmupTime).Seconds()
			if dt > 0.15 {
				delta := curBytes - warmupBytes
				if delta < 0 {
					delta = 0
				}
				return float64(delta) * 8.0 / 1_000_000.0 / dt
			}
		}
		dt := elapsed.Seconds()
		if dt <= 0 {
			return 0
		}
		return float64(curBytes) * 8.0 / 1_000_000.0 / dt
	}

	for {
		select {
		case <-phaseCtx.Done():
			now := time.Now()
			mbps := calcMbps(now)
			if onUpdate != nil {
				onUpdate(1.0, mbps)
			}
			return mbps
		case now := <-ticker.C:
			frac := now.Sub(start).Seconds() / totalDur.Seconds()
			if frac > 1.0 {
				frac = 1.0
			}
			if onUpdate != nil {
				onUpdate(frac, calcMbps(now))
			}
		}
	}
}
