package speedtest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunAgainstLocalServer(t *testing.T) {
	payload := make([]byte, 512*1024)
	var upTotal atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("CF-RAY", "8abc123-BOM")
		switch r.URL.Path {
		case "/__down":
			if r.URL.Query().Get("bytes") == "0" {
				time.Sleep(5 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
		case "/__up":
			n, _ := io.Copy(io.Discard, r.Body)
			upTotal.Add(n)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var phases []Phase
	var pauseCount int
	res := Run(context.Background(), Options{
		BaseURL:          srv.URL,
		PingCount:        3,
		DownloadDuration: 450 * time.Millisecond,
		UploadDuration:   450 * time.Millisecond,
		Streams:          2,
		DownloadBytes:    int64(len(payload)),
		UploadChunkBytes: 128 * 1024,
		Warmup:           100 * time.Millisecond,
		PhaseGap:         60 * time.Millisecond,
		Client:           srv.Client(),
	}, func(p Progress) {
		phases = append(phases, p.Phase)
		if p.Phase == PhasePause {
			pauseCount++
		}
	})

	if res.Phase != PhaseDone {
		t.Fatalf("expected PhaseDone, got %s (err=%q)", res.Phase, res.Error)
	}
	if res.ServerLabel != "Cloudflare (BOM)" {
		t.Fatalf("unexpected server label: %q", res.ServerLabel)
	}
	if res.PingMs <= 0 {
		t.Fatalf("expected positive PingMs, got %v", res.PingMs)
	}
	if res.DownloadMbps <= 0 {
		t.Fatalf("expected positive DownloadMbps, got %v", res.DownloadMbps)
	}
	if res.UploadMbps <= 0 || upTotal.Load() == 0 {
		t.Fatalf("expected positive UploadMbps and uploaded bytes, got %v (%d bytes)", res.UploadMbps, upTotal.Load())
	}
	if res.Percent != 100 {
		t.Fatalf("expected 100%% completion, got %d%%", res.Percent)
	}
	if pauseCount < 2 {
		t.Fatalf("expected PhasePause transitions between stages, got %d (phases=%v)", pauseCount, phases)
	}
	if len(res.Samples) == 0 {
		t.Fatal("expected non-empty throughput samples for live waveform")
	}
	if len(phases) < 6 {
		t.Fatalf("expected multiple progress updates, got %v", phases)
	}
}

func TestRunCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()

	res := Run(ctx, Options{
		BaseURL:          srv.URL,
		PingCount:        5,
		DownloadDuration: 2 * time.Second,
		UploadDuration:   2 * time.Second,
		Client:           srv.Client(),
	}, nil)

	if res.Phase != PhaseCancelled {
		t.Fatalf("expected PhaseCancelled, got %s (err=%q)", res.Phase, res.Error)
	}
}

func TestDefaultOptionsIndustryStandards(t *testing.T) {
	opts := DefaultOptions().withDefaults()
	if opts.PingCount < 8 {
		t.Fatalf("expected PingCount >= 8, got %d", opts.PingCount)
	}
	if opts.DownloadDuration < 8*time.Second {
		t.Fatalf("expected DownloadDuration >= 8s, got %v", opts.DownloadDuration)
	}
	if opts.UploadDuration < 8*time.Second {
		t.Fatalf("expected UploadDuration >= 8s, got %v", opts.UploadDuration)
	}
	if opts.Warmup < 1500*time.Millisecond || opts.Warmup >= opts.DownloadDuration {
		t.Fatalf("unexpected Warmup %v for duration %v", opts.Warmup, opts.DownloadDuration)
	}
	if opts.Streams < 4 {
		t.Fatalf("expected Streams >= 4, got %d", opts.Streams)
	}
	if opts.PhaseGap != 1500*time.Millisecond {
		t.Fatalf("expected PhaseGap == 1.5s, got %v", opts.PhaseGap)
	}
	if opts.DownloadBytes >= 100_000_000 {
		t.Fatalf("DownloadBytes %d exceeds Cloudflare <100MB request limit", opts.DownloadBytes)
	}
	if opts.UploadChunkBytes < 4_000_000 {
		t.Fatalf("expected UploadChunkBytes >= 4MB to avoid per-MB RTT stalls, got %d", opts.UploadChunkBytes)
	}
	tr, ok := opts.Client.Transport.(*http.Transport)
	if !ok || tr.TLSNextProto == nil {
		t.Fatal("expected HTTP/2 disabled (non-nil empty TLSNextProto) for true parallel TCP streams")
	}
}

func TestMeasureDownloadAbortsEarlyWhenAllStreamsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bytes") == "0" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	start := time.Now()
	res := Run(context.Background(), Options{
		BaseURL:          srv.URL,
		PingCount:        2,
		DownloadDuration: 6 * time.Second,
		UploadDuration:   2 * time.Second,
		Streams:          2,
		PhaseGap:         -1,
		Client:           srv.Client(),
	}, nil)
	elapsed := time.Since(start)

	if res.Phase != PhaseError {
		t.Fatalf("expected PhaseError when all download streams fail, got %s", res.Phase)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("expected early abort (<2s), took %v", elapsed)
	}
}
