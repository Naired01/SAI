//go:build windows

package main

import (
	"context"
	"log/slog"

	"golang.org/x/sys/windows/svc"
)

// saiService implementa svc.Service para que el agente pueda correr
// como servicio nativo de Windows. El Service Control Manager (SCM)
// nos llama con Execute() cuando arranca el servicio; nosotros debemos
// reportar SERVICE_RUNNING apenas arranquemos el loop principal y
// responder a las solicitudes de stop/pause/shutdown.
//
// Estrategia: en una goroutine corremos runMainLoop (el loop de reconexión).
// Reportamos StartPending → Running apenas runMainLoop está en marcha.
// Cuando llega Stop/Shutdown del SCM, cancelamos el contexto y runMainLoop
// termina; eso desbloquea svc.Run y el servicio sale.
type saiService struct {
	ctx      context.Context
	cancel   context.CancelFunc
	logger   *slog.Logger
	cfg      *Config
	hostname string
	jwtPath  string
}

func (s *saiService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPauseAndContinue
	changes <- svc.Status{State: svc.StartPending}

	// Goroutine con el loop principal. Reporta Running apenas arranca,
	// y StopPending cuando termina (por cancelación del contexto o error fatal).
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
		runMainLoop(s.ctx, s.logger, s.cfg, s.hostname, s.jwtPath)
		changes <- svc.Status{State: svc.StopPending}
	}()

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				s.cancel()
				<-loopDone
				return false, 0
			case svc.Pause:
				changes <- svc.Status{State: svc.Paused, Accepts: cmdsAccepted}
			case svc.Continue:
				changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
			}
		case <-loopDone:
			// runMainLoop salió sin un Stop del SCM (ej. fatal error).
			return false, 0
		}
	}
}

// isWindowsService devuelve true si el proceso está corriendo bajo el
// Service Control Manager (no hay sesión interactiva).
func isWindowsService() bool {
	ok, _ := svc.IsWindowsService()
	return ok
}

// runAsService arranca el agente bajo el control del SCM. Devuelve true
// si se ejecutó como servicio (en cuyo caso el binario debe salir tras
// svc.Run retornar), false si no era un servicio y el caller debe
// continuar con el loop normal.
func runAsService(name string, ctx context.Context, cancel context.CancelFunc, logger *slog.Logger, cfg *Config, hostname, jwtPath string) (bool, error) {
	if !isWindowsService() {
		return false, nil
	}
	logger.Info("running as Windows service", "name", name)
	return true, svc.Run(name, &saiService{ctx: ctx, cancel: cancel, logger: logger, cfg: cfg, hostname: hostname, jwtPath: jwtPath})
}