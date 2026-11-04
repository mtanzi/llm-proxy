package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mtanzi/llm-proxy/internal/recorder"
)

// Bounds for the steps of Shutdown that have no config setting. They only
// guard against a hung step; a normal stop takes milliseconds.
const (
	adminStopTimeout     = 5 * time.Second
	retentionStopTimeout = 10 * time.Second
)

// ErrForcedExit is returned by Shutdown.Run when a second signal cut the
// graceful shutdown short.
var ErrForcedExit = errors.New("shutdown forced by second signal")

// RecorderCloser is the recorder as shutdown sees it. *recorder.Recorder
// implements it.
type RecorderCloser interface {
	Close(ctx context.Context) error
	CallsDropped(reason string) uint64
}

// RetentionStopper stops the retention job. *postgres.Store implements it.
type RetentionStopper interface {
	StopRetention(ctx context.Context) error
}

// StoreCloser closes the store. *postgres.Store implements it.
type StoreCloser interface {
	Close()
}

// Shutdown is the graceful stop of ADR 0027. The composition root (ADR 0028)
// fills it and calls Run; tests call Do. Recorder, Retention and Store may be
// nil, which skips that step.
type Shutdown struct {
	Servers   *Servers
	Recorder  RecorderCloser
	Retention RetentionStopper
	Store     StoreCloser

	// DrainTimeout is server.drain_timeout: how long Do waits for in-flight
	// streams. FlushTimeout is the recorder flush deadline; zero uses the
	// recorder default (30 seconds).
	DrainTimeout time.Duration
	FlushTimeout time.Duration

	Log *slog.Logger
	// Exit ends the process on a second signal. Nil uses os.Exit.
	Exit func(code int)
}

// Run waits for the first signal on sigs and then runs Do. A second signal
// while Do runs is a forced exit: Run logs it, calls Exit(1) and returns
// ErrForcedExit. Otherwise Run returns the error of Do.
func (s *Shutdown) Run(sigs <-chan os.Signal) error {
	sig, ok := <-sigs
	if !ok {
		return errors.New("shutdown: signal channel closed")
	}
	s.Log.Info("shutdown: signal received, draining", "signal", sig.String(), "drain_timeout", s.DrainTimeout)
	done := make(chan error, 1)
	go func() { done <- s.Do(context.Background()) }()
	select {
	case err := <-done:
		return err
	case sig := <-sigs:
		s.Log.Error("shutdown: second signal, forcing exit without draining", "signal", sig.String())
		exit := s.Exit
		if exit == nil {
			exit = os.Exit
		}
		exit(1)
		return ErrForcedExit
	}
}

// Do runs the shutdown steps in order and returns the joined errors of the
// steps that failed. A failed step does not stop the later ones.
//
//  1. Both /readyz endpoints report not-ready.
//  2. The data listener stops accepting and in-flight streams finish, up to
//     DrainTimeout; streams still open after that are cut. The admin plane
//     stays up, so probes and /metrics keep answering during the drain.
//  3. The recorder closes and the writer flushes the queue within FlushTimeout.
//     Records still queued then are dropped (reason shutdown) and counted.
//  4. The admin listener stops.
//  5. The retention job stops and the store closes.
func (s *Shutdown) Do(ctx context.Context) error {
	var errs []error

	s.Servers.Drain.Start()
	s.Log.Info("shutdown: readiness off, both /readyz report not-ready")

	if err := s.drain(ctx); err != nil {
		errs = append(errs, err)
	}

	if s.Recorder != nil {
		if err := s.closeRecorder(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	adminCtx, cancel := context.WithTimeout(ctx, adminStopTimeout)
	if err := s.Servers.Admin.Shutdown(adminCtx); err != nil {
		_ = s.Servers.Admin.Close()
		errs = append(errs, fmt.Errorf("shutdown: admin listener: %w", err))
	}
	cancel()

	if s.Retention != nil {
		retCtx, cancel := context.WithTimeout(ctx, retentionStopTimeout)
		if err := s.Retention.StopRetention(retCtx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown: retention: %w", err))
		}
		cancel()
	}
	if s.Store != nil {
		s.Store.Close()
		s.Log.Info("shutdown: store closed")
	}
	s.Log.Info("shutdown: done")
	return errors.Join(errs...)
}

// drain stops the data listener and waits for in-flight requests.
func (s *Shutdown) drain(ctx context.Context) error {
	drainCtx, cancel := context.WithTimeout(ctx, s.DrainTimeout)
	defer cancel()
	s.Log.Info("shutdown: data listener closed, waiting for in-flight streams", "inflight_streams", s.inflight())
	err := s.Servers.Data.Shutdown(drainCtx)
	if err == nil {
		s.Log.Info("shutdown: in-flight streams finished")
		return nil
	}
	n := s.inflight()
	s.Log.Error("shutdown: drain deadline passed, cutting streams", "inflight_streams", n, "drain_timeout", s.DrainTimeout)
	_ = s.Servers.Data.Close()
	return fmt.Errorf("shutdown: drain: %w (%d streams cut)", err, n)
}

func (s *Shutdown) inflight() int64 {
	if s.Servers.Inflight == nil {
		return 0
	}
	return s.Servers.Inflight()
}

// closeRecorder closes the recorder queue and logs how many records the flush
// deadline dropped.
func (s *Shutdown) closeRecorder(ctx context.Context) error {
	if s.FlushTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.FlushTimeout)
		defer cancel()
	}
	before := s.Recorder.CallsDropped(recorder.ReasonShutdown)
	err := s.Recorder.Close(ctx)
	dropped := s.Recorder.CallsDropped(recorder.ReasonShutdown) - before
	if err != nil {
		s.Log.Error("shutdown: recorder flush deadline passed", "dropped", dropped, "reason", recorder.ReasonShutdown)
		return fmt.Errorf("shutdown: recorder: %w (%d records dropped)", err, dropped)
	}
	s.Log.Info("shutdown: recorder flushed", "dropped", dropped)
	return nil
}
