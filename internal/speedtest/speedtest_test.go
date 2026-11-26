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
	res := Run(context.Background(), Options{
		BaseURL:          srv.URL,
		PingCount:        3,
		DownloadDuration: 450 * time.Millisecond,
		UploadDuration:   450 * time.Millisecond,
		Streams:          2,
		DownloadBytes:    int64(len(payload)),
		UploadChunkBytes: 128 * 1024,
		Warmup:           100 * time.Millisecond,
		Client:           srv.Client(),
	}, func(p Progress) {
		phases = append(phases, p.Phase)
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
	if len(phases) < 4 {
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
