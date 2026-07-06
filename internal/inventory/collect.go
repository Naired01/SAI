package inventory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// CollectTimeout marca la cota superior del Collect. Si Collect la supera,
// devolvemos un Snapshot parcial con `Error` poblado y bloques vacíos en lo
// que no se haya podido leer (así la UI sabe que falló la recolección).
const CollectTimeout = 8 * time.Second

// CollectorOpts permite inyectar recolectores para tests. Si nil, usa gopsutil.
type CollectorOpts struct {
	CPU     func(ctx context.Context) ([]CPUSlot, error)
	Memory  func(ctx context.Context) (Memory, error)
	Disks   func(ctx context.Context) ([]Disk, error)
	Network func(ctx context.Context) ([]NetIface, error)
	Host    func(ctx context.Context) (Host, error)
}

// Collect recolecta el inventario HW del equipo actual y devuelve un Snapshot
// listo para enviar al server. Nunca retorna error: si algo falla (gopsutil
// no disponible, timeout, etc.) devuelve un Snapshot con Error poblado.
func Collect(ctx context.Context, agentVersion string) Snapshot {
	if deadline, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, CollectTimeout)
		defer cancel()
		_ = deadline
	}
	opts := CollectorOpts{
		CPU:     defaultCPU,
		Memory:  defaultMemory,
		Disks:   defaultDisks,
		Network: defaultNetwork,
		Host:    defaultHost,
	}
	return collectWith(ctx, opts, agentVersion)
}

func collectWith(ctx context.Context, opts CollectorOpts, agentVersion string) Snapshot {
	snap := Snapshot{
		SchemaVer:    SchemaVer,
		AgentVersion: agentVersion,
		Software:     Software{},
		CollectedAt:  time.Now().UTC(),
	}
	var errs []string

	if h, err := opts.Host(ctx); err != nil {
		errs = append(errs, "host:"+err.Error())
	} else {
		snap.Hardware.Host = h
	}
	if c, err := opts.CPU(ctx); err != nil {
		errs = append(errs, "cpu:"+err.Error())
	} else {
		snap.Hardware.CPU = c
	}
	if m, err := opts.Memory(ctx); err != nil {
		errs = append(errs, "mem:"+err.Error())
	} else {
		snap.Hardware.Memory = m
	}
	if d, err := opts.Disks(ctx); err != nil {
		errs = append(errs, "disk:"+err.Error())
	} else {
		snap.Hardware.Disks = d
	}
	if n, err := opts.Network(ctx); err != nil {
		errs = append(errs, "net:"+err.Error())
	} else {
		snap.Hardware.Network = n
	}

	// Software: per-OS collector. Corre con su propio contexto acotado para
	// no consumir el SLA del collector HW. Si el OS no tiene un package manager
	// accesible (caso legítimo: Alpine sin apk, sandbox, etc.) o falla por
	// permisos, lo tratamos como snapshot parcial pero NO marcamos Error.
	sw, swErr := collectSoftwareWithTimeout(ctx, 6*time.Second)
	snap.Software = sw
	if swErr != "" {
		// Sólo marcamos Error si encontramos ALGO (paquetes o servicios) pero
		// algo falló a medias. Si no encontramos nada, es un sistema sin DB de
		// paquetes — situación esperada.
		if sw.ContainsSoftware() {
			errs = append(errs, swErr)
		}
	}

	if len(errs) > 0 {
		snap.Error = strings.Join(errs, "; ")
	}
	return snap
}

// collectSoftwareWithTimeout corre el software collector con timeout duro para
// evitar que un dpkg-query o Get-Package lento tumbe el snapshot completo.
// Devuelve el Software populado (best-effort) y un mensaje de error si algo
// falló.
func collectSoftwareWithTimeout(parent context.Context, d time.Duration) (Software, string) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	sw := CollectSoftware(ctx, nil)
	if !sw.ContainsSoftware() {
		return sw, "no software data collected"
	}
	return sw, ""
}

// -----------------------------------------------------------------------------
// Implementaciones default (gopsutil). Cada función tolera el ctx con un
// guard: si venció, devuelve ctx.Err() envuelto.
// -----------------------------------------------------------------------------

func defaultHost(ctx context.Context) (Host, error) {
	if err := ctx.Err(); err != nil {
		return Host{}, err
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	hi, err := host.InfoWithContext(ctx)
	if err != nil {
		// Best-effort: devolvemos lo que tengamos.
		return Host{
			Hostname:   hostname,
			OS:         runtime.GOOS,
			Platform:   runtime.GOOS,
			KernelArch: runtime.GOARCH,
		}, err
	}
	return Host{
		Hostname:   hostname,
		OS:         runtime.GOOS,
		Platform:   hi.Platform,
		KernelArch: runtime.GOARCH,
		KernelVer:  hi.KernelVersion,
		UptimeSecs: hi.Uptime,
		BootTime:   time.Unix(int64(hi.BootTime), 0).UTC().Format(time.RFC3339),
	}, nil
}

func defaultCPU(ctx context.Context) ([]CPUSlot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	infos, err := cpu.InfoWithContext(ctx)
	if err != nil {
		return nil, err
	}
	logical, _ := cpu.CountsWithContext(ctx, true)
	physical, _ := cpu.CountsWithContext(ctx, false)
	if len(infos) == 0 {
		// Algunos sistemas (CI mínimo) no exponen cpu.Info. Devolvemos
		// un slot "unknown" para que el panel pueda mostrar "N cores".
		return []CPUSlot{{
			ModelName: "unknown",
			Cores:     int32(logical),
		}}, nil
	}
	out := make([]CPUSlot, 0, len(infos))
	for _, ci := range infos {
		cores := ci.Cores
		if cores == 0 && logical > 0 {
			cores = int32(logical / len(infos))
		}
		out = append(out, CPUSlot{
			ModelName: ci.ModelName,
			Vendor:    ci.VendorID,
			Family:    ci.Family,
			Model:     ci.Model,
			Cores:     cores,
			MHz:       ci.Mhz,
		})
	}
	// Anota cores lógicos como un campo aparte si quieres; en una versión
	// futura podemos extender CPUSlot con LogicalCount.
	_ = physical
	return out, nil
}

func defaultMemory(ctx context.Context) (Memory, error) {
	if err := ctx.Err(); err != nil {
		return Memory{}, err
	}
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return Memory{}, err
	}
	return Memory{
		TotalBytes:    vm.Total,
		AvailableByte: vm.Available,
		UsedBytes:     vm.Used,
	}, nil
}

func defaultDisks(ctx context.Context) ([]Disk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, err
	}
	out := make([]Disk, 0, len(parts))
	for _, p := range parts {
		usage, uerr := disk.UsageWithContext(ctx, p.Mountpoint)
		if uerr != nil {
			continue // ignorar una partición no medible
		}
		out = append(out, Disk{
			Device:     p.Device,
			Mountpoint: p.Mountpoint,
			FSType:     p.Fstype,
			TotalBytes: usage.Total,
			UsedBytes:  usage.Used,
		})
	}
	if len(out) == 0 {
		return out, errors.New("no partitions reported")
	}
	return out, nil
}

func defaultNetwork(ctx context.Context) ([]NetIface, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ifaces, err := net.InterfacesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]NetIface, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs := make([]string, 0, len(iface.Addrs))
		for _, a := range iface.Addrs {
			addrs = append(addrs, a.Addr)
		}
		flagsJoined := strings.Join(iface.Flags, ",")
		state := "down"
		if iface.Flags != nil {
			state = "unknown"
			for _, f := range iface.Flags {
				if f == "up" {
					state = "up"
					break
				}
			}
		}
		out = append(out, NetIface{
			Name:         iface.Name,
			HardwareAddr: iface.HardwareAddr,
			MTU:          int(iface.MTU),
			Flags:        flagsJoined,
			Addrs:        addrs,
			State:        state,
		})
	}
	if len(out) == 0 {
		return out, fmt.Errorf("no interfaces reported")
	}
	return out, nil
}
