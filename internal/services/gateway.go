package services

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/gateway"
)

// GatewayService is the bound service for the gateway lifecycle seam: state
// queries plus pause/resume over the gateway Manager. State changes are
// forwarded to the emitter as EventGatewayState events.
type GatewayService struct {
	gateway gateway.Manager
	emitter Emitter
}

// NewGatewayService returns the gateway service over gw, forwarding every
// state transition through emitter.
func NewGatewayService(gw gateway.Manager, emitter Emitter) *GatewayService {
	s := &GatewayService{gateway: gw, emitter: emitter}
	gw.Subscribe(s.onState)
	return s
}

// Start transitions the gateway from stopped to running.
func (s *GatewayService) Start(ctx context.Context) error {
	if err := s.gateway.Start(ctx); err != nil {
		return fmt.Errorf("start gateway: %w", err)
	}
	return nil
}

// State returns the gateway's current state: "stopped", "running", or
// "paused".
func (s *GatewayService) State(_ context.Context) (string, error) {
	return s.gateway.State().String(), nil
}

// Pause transitions the gateway from running to paused: triggers would be
// held until resumed.
func (s *GatewayService) Pause(_ context.Context) error {
	if err := s.gateway.Pause(); err != nil {
		return fmt.Errorf("pause gateway: %w", err)
	}
	return nil
}

// Resume transitions the gateway from paused back to running.
func (s *GatewayService) Resume(_ context.Context) error {
	if err := s.gateway.Resume(); err != nil {
		return fmt.Errorf("resume gateway: %w", err)
	}
	return nil
}

// onState forwards one transition to the emitter. The gateway invokes
// listeners synchronously and requires them not to block, so the emit runs
// on its own goroutine.
func (s *GatewayService) onState(_, to gateway.State) {
	go s.emitter.Emit(EventGatewayState, GatewayStateEvent{State: to.String()})
}
