//go:build windows

package winservice

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

type serviceHandler struct {
	name string
	run  func(context.Context) error
}

func Run(name string, run func(context.Context) error) error {
	log, err := eventlog.Open(name)
	if err == nil {
		defer log.Close()
		_ = log.Info(1, name+" starting")
	}
	err = svc.Run(name, &serviceHandler{name: name, run: run})
	if err != nil {
		if log != nil {
			_ = log.Error(1, err.Error())
		}
		return fmt.Errorf("run Windows service: %w", err)
	}
	if log != nil {
		_ = log.Info(1, name+" stopped")
	}
	return nil
}

func (h *serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	statuses <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- h.run(ctx) }()
	statuses <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				statuses <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				statuses <- svc.Status{State: svc.StopPending}
				cancel()
				if err := <-errCh; err != nil {
					return false, 1
				}
				return false, 0
			default:
				continue
			}
		case err := <-errCh:
			if err != nil {
				return false, 1
			}
			return false, 0
		}
	}
}
