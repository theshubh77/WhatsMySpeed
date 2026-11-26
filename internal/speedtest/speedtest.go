// Package speedtest implements a multi-stream HTTP latency, download, and upload speed test.
package speedtest

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
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
	PhasePause     Phase = "pause"
	PhaseDownload  Phase = "download"
	PhaseUpload    Phase = "upload"
	PhaseDone      Phase = "done"
	PhaseCancelled Phase = "cancelled"
	PhaseError     Phase = "error"
)

// Progress carries live metrics and completion percentage during a test run.
type Progress struct {
	Phase          Phase
	CompletedPhase Phase
	NextPhase      Phase
	Percent        int
	PingMs         float64
	JitterMs       float64
	DownloadMbps   float64
	UploadMbps     float64
	Samples        []float64
	ServerLabel    string
	StatusText     string
	Error          string
}

// Options configures endpoints, stream concurrency, durations, and phase pauses.
type Options struct {
	BaseURL          string
	PingCount        int
	DownloadDuration time.Duration
	UploadDuration   time.Duration
	Streams          int
	DownloadBytes    int64
	UploadChunkBytes int64
	Warmup           time.Duration
	PhaseGap         time.Duration
	Client           *http.Client
}

// DefaultOptions returns industry-standard settings (8 pings, 8s down/up windows
// with a 2s TCP slow-start warmup exclusion, 1.5s inter-phase breather gaps,
// across 4 parallel streams) against Cloudflare Edge.
func DefaultOptions() Options {
	return Options{
		BaseURL:          "https://speed.cloudflare.com",
		PingCount:        8,
		DownloadDuration: 8 * time.Second,
		UploadDuration:   8 * time.Second,
		Streams:          4,
		DownloadBytes:    25_000_000,
		UploadChunkBytes: 4_000_000,
		Warmup:           2 * time.Second,
		PhaseGap:         1500 * time.Millisecond,
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
	if o.PhaseGap < 0 {
		o.PhaseGap = 0
	} else if o.PhaseGap == 0 {
		o.PhaseGap = d.PhaseGap
	}
	if o.Client == nil {
		// Disable HTTP/2 multiplexing so concurrent worker streams use independent
		// parallel TCP connections instead of sharing one HTTP/2 flow-controlled socket.
		tr := &http.Transport{
			MaxIdleConns:        o.Streams * 2,
			MaxIdleConnsPerHost: o.Streams * 2,
			IdleConnTimeout:     30 * time.Second,
			WriteBufferSize:     32 * 1024,
			ReadBufferSize:      32 * 1024,
			TLSNextProto:        make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
		}
		clientTimeout := 30 * time.Second
		if maxPhase := maxDuration(o.DownloadDuration, o.UploadDuration) + 15*time.Second; maxPhase > clientTimeout {
			clientTimeout = maxPhase
		}
		o.Client = &http.Client{
			Transport: tr,
			Timeout:   clientTimeout,
		}
	}
	return o
}

func maxDuration(a, b time.Duration) time.Duration {
	if a >= b {
		return a
	}
	return b
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

	pingMs, jitterMs, pop, err := measurePing(ctx, opts, func(step int, curMedian, curJitter float64) {
		state.Percent = 2 + (step*8)/opts.PingCount
		if curMedian > 0 {
			state.PingMs = curMedian
			state.JitterMs = curJitter
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
	state.JitterMs = jitterMs
	state.Percent = 10

	if opts.PhaseGap > 0 {
		state.Phase = PhasePause
		state.CompletedPhase = PhasePing
		state.NextPhase = PhaseDownload
		state.StatusText = "Starting download..."
		emit(state)
		if err := waitPhaseGap(ctx, opts.PhaseGap); err != nil {
			state.Phase = PhaseCancelled
			state.CompletedPhase = ""
			state.NextPhase = ""
			state.StatusText = "Speed test cancelled"
			emit(state)
			return state
		}
	}

	state.CompletedPhase = ""
	state.NextPhase = ""
	state.Phase = PhaseDownload
	state.StatusText = fmt.Sprintf("Testing download (%d streams)...", opts.Streams)
	emit(state)

	dlMbps, dlSamples, err := measureDownload(ctx, opts, func(frac float64, curMbps float64, samples []float64) {
		state.Percent = 10 + int(frac*45.0)
		state.DownloadMbps = curMbps
		state.Samples = samples
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
	state.Samples = dlSamples
	state.Percent = 55

	if opts.PhaseGap > 0 {
		state.Phase = PhasePause
		state.CompletedPhase = PhaseDownload
		state.NextPhase = PhaseUpload
		state.StatusText = "Preparing upload..."
		emit(state)
		if err := waitPhaseGap(ctx, opts.PhaseGap); err != nil {
			state.Phase = PhaseCancelled
			state.CompletedPhase = ""
			state.NextPhase = ""
			state.StatusText = "Speed test cancelled"
			emit(state)
			return state
		}
	}

	state.CompletedPhase = ""
	state.NextPhase = ""
	state.Samples = nil
	state.Phase = PhaseUpload
	state.StatusText = fmt.Sprintf("Testing upload (%d streams)...", opts.Streams)
	emit(state)

	ulMbps, _, err := measureUpload(ctx, opts, func(frac float64, curMbps float64, samples []float64) {
		state.Percent = 55 + int(frac*44.0)
		state.UploadMbps = curMbps
		state.Samples = samples
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
	if opts.PhaseGap > 0 {
		state.Phase = PhasePause
		state.CompletedPhase = PhaseUpload
		state.NextPhase = PhaseDone
		state.Percent = 99
		state.StatusText = "Finalizing results..."
		emit(state)
		if err := waitPhaseGap(ctx, opts.PhaseGap); err != nil {
			state.Phase = PhaseCancelled
			state.CompletedPhase = ""
			state.NextPhase = ""
			state.StatusText = "Speed test cancelled"
			emit(state)
			return state
		}
	}

	state.CompletedPhase = ""
	state.NextPhase = ""
	state.Samples = dlSamples
	state.Percent = 100
	state.Phase = PhaseDone
	state.StatusText = "Completed"
	emit(state)
	return state
}

func waitPhaseGap(ctx context.Context, gap time.Duration) error {
	if gap <= 0 {
		return nil
	}
	t := time.NewTimer(gap)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
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

func applyRequestHeaders(req *http.Request, baseURL string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Cache-Control", "no-store, no-cache")
	if strings.Contains(strings.ToLower(baseURL), "cloudflare.com") {
		req.Header.Set("Origin", "https://speed.cloudflare.com")
		req.Header.Set("Referer", "https://speed.cloudflare.com/")
	}
}

func measurePing(ctx context.Context, opts Options, onStep func(int, float64, float64)) (float64, float64, string, error) {
	url := fmt.Sprintf("%s/__down?bytes=0", opts.BaseURL)

	// Warm-up request establishes DNS + TCP + TLS session so pings measure true RTT.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, "", err
	}
	applyRequestHeaders(req, opts.BaseURL)
	resp, err := opts.Client.Do(req)
	if err != nil {
		return 0, 0, "", err
	}
	pop := parseCloudflarePOP(resp.Header)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, 0, pop, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	samples := make([]float64, 0, opts.PingCount)
	coldRetries := 0
	for len(samples) < opts.PingCount {
		if ctx.Err() != nil {
			return 0, 0, pop, ctx.Err()
		}
		var connReused atomic.Bool
		trace := &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) {
				connReused.Store(info.Reused)
			},
		}
		reqCtx := httptrace.WithClientTrace(ctx, trace)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
		if err != nil {
			return 0, 0, pop, err
		}
		applyRequestHeaders(req, opts.BaseURL)
		t0 := time.Now()
		resp, err := opts.Client.Do(req)
		if err != nil {
			return 0, 0, pop, err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return 0, 0, pop, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
		}
		// Skip occasional TCP+TLS reconnect handshake spikes when measuring RTT.
		if !connReused.Load() && coldRetries < 2 {
			coldRetries++
			continue
		}
		rttMs := float64(time.Since(t0).Microseconds()) / 1000.0
		samples = append(samples, rttMs)
		if onStep != nil {
			onStep(len(samples), medianFloat64(samples), computeJitterMs(samples))
		}
	}

	return medianFloat64(samples), computeJitterMs(samples), pop, nil
}

func computeJitterMs(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	var sumDiff float64
	for i := 1; i < len(vals); i++ {
		d := vals[i] - vals[i-1]
		if d < 0 {
			d = -d
		}
		sumDiff += d
	}
	return sumDiff / float64(len(vals)-1)
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

func measureDownload(ctx context.Context, opts Options, onUpdate func(frac, mbps float64, samples []float64)) (float64, []float64, error) {
	phaseCtx, cancel := context.WithTimeout(ctx, opts.DownloadDuration)
	defer cancel()

	var totalBytes atomic.Int64
	var activeWorkers atomic.Int32
	var firstErr atomic.Value
	url := fmt.Sprintf("%s/__down?bytes=%d", opts.BaseURL, opts.DownloadBytes)

	var wg sync.WaitGroup
	activeWorkers.Store(int32(opts.Streams))
	for i := 0; i < opts.Streams; i++ {
		wg.Add(1)
		go func(idx int) {
			defer func() {
				if activeWorkers.Add(-1) == 0 && totalBytes.Load() == 0 {
					cancel()
				}
				wg.Done()
			}()
			if idx > 0 {
				if err := waitPhaseGap(phaseCtx, time.Duration(idx)*40*time.Millisecond); err != nil {
					return
				}
			}
			buf := make([]byte, 32*1024)
			rateRetries := 0
			for phaseCtx.Err() == nil {
				req, err := http.NewRequestWithContext(phaseCtx, http.MethodGet, url, nil)
				if err != nil {
					firstErr.CompareAndSwap(nil, err)
					return
				}
				applyRequestHeaders(req, opts.BaseURL)
				resp, err := opts.Client.Do(req)
				if err != nil {
					if phaseCtx.Err() == nil {
						firstErr.CompareAndSwap(nil, err)
					}
					return
				}
				if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
					_ = resp.Body.Close()
					if rateRetries < 2 {
						rateRetries++
						if err := waitPhaseGap(phaseCtx, time.Duration(rateRetries)*400*time.Millisecond); err != nil {
							return
						}
						continue
					}
					firstErr.CompareAndSwap(nil, fmt.Errorf("download HTTP %d", resp.StatusCode))
					return
				}
				if resp.StatusCode >= 400 {
					_ = resp.Body.Close()
					firstErr.CompareAndSwap(nil, fmt.Errorf("download HTTP %d", resp.StatusCode))
					return
				}
				rateRetries = 0
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
		}(i)
	}

	finalMbps, samples := trackThroughput(phaseCtx, opts.DownloadDuration, opts.Warmup, &totalBytes, onUpdate)
	wg.Wait()

	if ctx.Err() != nil {
		return finalMbps, samples, ctx.Err()
	}
	if totalBytes.Load() == 0 {
		if v := firstErr.Load(); v != nil {
			return 0, nil, v.(error)
		}
		return 0, nil, errors.New("no download bytes received")
	}
	return finalMbps, samples, nil
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

func measureUpload(ctx context.Context, opts Options, onUpdate func(frac, mbps float64, samples []float64)) (float64, []float64, error) {
	phaseCtx, cancel := context.WithTimeout(ctx, opts.UploadDuration)
	defer cancel()

	var totalBytes atomic.Int64
	var activeWorkers atomic.Int32
	var firstErr atomic.Value
	url := fmt.Sprintf("%s/__up", opts.BaseURL)

	var wg sync.WaitGroup
	activeWorkers.Store(int32(opts.Streams))
	for i := 0; i < opts.Streams; i++ {
		wg.Add(1)
		go func(idx int) {
			defer func() {
				if activeWorkers.Add(-1) == 0 && totalBytes.Load() == 0 {
					cancel()
				}
				wg.Done()
			}()
			if idx > 0 {
				if err := waitPhaseGap(phaseCtx, time.Duration(idx)*40*time.Millisecond); err != nil {
					return
				}
			}
			rateRetries := 0
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
				applyRequestHeaders(req, opts.BaseURL)
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
				if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
					if rateRetries < 2 {
						rateRetries++
						if err := waitPhaseGap(phaseCtx, time.Duration(rateRetries)*400*time.Millisecond); err != nil {
							return
						}
						continue
					}
					firstErr.CompareAndSwap(nil, fmt.Errorf("upload HTTP %d", resp.StatusCode))
					return
				}
				if resp.StatusCode >= 400 {
					firstErr.CompareAndSwap(nil, fmt.Errorf("upload HTTP %d", resp.StatusCode))
					return
				}
				rateRetries = 0
			}
		}(i)
	}

	finalMbps, samples := trackThroughput(phaseCtx, opts.UploadDuration, opts.Warmup, &totalBytes, onUpdate)
	wg.Wait()

	if ctx.Err() != nil {
		return finalMbps, samples, ctx.Err()
	}
	if totalBytes.Load() == 0 {
		if v := firstErr.Load(); v != nil {
			return 0, nil, v.(error)
		}
		return 0, nil, errors.New("no upload bytes transmitted")
	}
	return finalMbps, samples, nil
}

func trimmedSteadyMbps(slices []float64, fallbackAvg float64) float64 {
	if len(slices) < 6 {
		return fallbackAvg
	}
	cp := append([]float64(nil), slices...)
	sort.Float64s(cp)
	// Ookla Speedtest-style trimmed steady band: drop lowest 30% slow/stall intervals
	// and highest 10% transient buffer bursts.
	lo := (len(cp) * 30) / 100
	hi := (len(cp) * 90) / 100
	if hi <= lo {
		return fallbackAvg
	}
	var sum float64
	for i := lo; i < hi; i++ {
		sum += cp[i]
	}
	return sum / float64(hi-lo)
}

func trackThroughput(
	phaseCtx context.Context,
	totalDur time.Duration,
	warmup time.Duration,
	totalBytes *atomic.Int64,
	onUpdate func(frac, mbps float64, samples []float64),
) (float64, []float64) {
	start := time.Now()
	prevTickTime := start
	var prevTickBytes int64
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	var warmupDone bool
	var warmupBytes int64
	var warmupTime time.Time
	var emaMbps float64
	var postWarmupSlices []float64
	var waveform []float64

	minPostWarmupDt := 0.50
	if rem := totalDur.Seconds() - warmup.Seconds(); rem > 0 && rem*0.4 < minPostWarmupDt {
		minPostWarmupDt = rem * 0.4
		if minPostWarmupDt < 0.10 {
			minPostWarmupDt = 0.10
		}
	}

	avgMbps := func(now time.Time, curBytes int64) float64 {
		elapsed := now.Sub(start)
		if warmup > 0 && !warmupDone && elapsed >= warmup {
			warmupDone = true
			warmupBytes = curBytes
			warmupTime = now
		}
		if warmupDone {
			dt := now.Sub(warmupTime).Seconds()
			if dt >= minPostWarmupDt {
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
			curBytes := totalBytes.Load()
			finalVal := avgMbps(now, curBytes)
			if finalVal <= 0 && len(postWarmupSlices) >= 6 {
				finalVal = trimmedSteadyMbps(postWarmupSlices, finalVal)
			}
			wfCopy := append([]float64(nil), waveform...)
			if onUpdate != nil {
				onUpdate(1.0, finalVal, wfCopy)
			}
			return finalVal, wfCopy
		case now := <-ticker.C:
			curBytes := totalBytes.Load()
			stepDt := now.Sub(prevTickTime).Seconds()
			var instMbps float64
			if stepDt > 0.01 {
				dBytes := curBytes - prevTickBytes
				if dBytes < 0 {
					dBytes = 0
				}
				instMbps = float64(dBytes) * 8.0 / 1_000_000.0 / stepDt
				prevTickTime = now
				prevTickBytes = curBytes

				if emaMbps == 0 {
					emaMbps = instMbps
				} else {
					emaMbps = 0.25*instMbps + 0.75*emaMbps
				}
				waveform = append(waveform, emaMbps)
				if len(waveform) > 48 {
					waveform = waveform[len(waveform)-48:]
				}
			}

			steadyAvg := avgMbps(now, curBytes)
			if warmupDone && stepDt > 0.01 && instMbps > 0 {
				postWarmupSlices = append(postWarmupSlices, instMbps)
			}

			displayMbps := steadyAvg
			if emaMbps > 0 && now.Sub(start) < warmup {
				displayMbps = emaMbps
			} else if steadyAvg > 0 && emaMbps > 0 {
				displayMbps = 0.20*emaMbps + 0.80*steadyAvg
			}

			frac := now.Sub(start).Seconds() / totalDur.Seconds()
			if frac > 1.0 {
				frac = 1.0
			}
			if onUpdate != nil {
				wfCopy := append([]float64(nil), waveform...)
				onUpdate(frac, displayMbps, wfCopy)
			}
		}
	}
}
