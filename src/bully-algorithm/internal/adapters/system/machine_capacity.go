package system

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/domain/model"
	"github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm/internal/ports"
)

// MachineCapacityReader obtiene CPU, memoria física y velocidad de enlace del
// host. No lee estos valores desde variables de entorno.
type MachineCapacityReader struct {
	cpu       func() int
	memory    func(context.Context) (int64, error)
	bandwidth func(context.Context) int64
}

// NewMachineCapacityReader crea el lector de capacidad del sistema.
func NewMachineCapacityReader() *MachineCapacityReader {
	return &MachineCapacityReader{
		cpu:       runtime.NumCPU,
		memory:    physicalMemoryGB,
		bandwidth: func(ctx context.Context) int64 { return linkBandwidthMbps(ctx) },
	}
}

// Read obtiene la capacidad física detectada en el sistema operativo.
func (r *MachineCapacityReader) Read(ctx context.Context) (model.NodeResources, error) {
	if ctx == nil {
		return model.NodeResources{}, errors.New("context must not be nil")
	}

	memoryGB, err := r.memory(ctx)
	if err != nil {
		return model.NodeResources{}, err
	}

	return model.NodeResources{
		CPUCores:      int64(r.cpu()),
		MemoryGB:      memoryGB,
		BandwidthMbps: r.bandwidth(ctx),
	}, nil
}

func physicalMemoryGB(ctx context.Context) (int64, error) {
	switch runtime.GOOS {
	case "linux":
		file, err := os.Open("/proc/meminfo")
		if err != nil {
			return 0, fmt.Errorf("open /proc/meminfo: %w", err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 2 && fields[0] == "MemTotal:" {
				kilobytes, err := strconv.ParseInt(fields[1], 10, 64)
				if err != nil {
					return 0, fmt.Errorf("parse MemTotal: %w", err)
				}
				return kilobytes / (1024 * 1024), nil
			}
		}
		if err := scanner.Err(); err != nil {
			return 0, fmt.Errorf("read /proc/meminfo: %w", err)
		}
		return 0, errors.New("MemTotal not found in /proc/meminfo")
	case "windows":
		output, err := runPowerShell(ctx, "(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory")
		if err != nil {
			return 0, fmt.Errorf("read Windows physical memory: %w", err)
		}
		bytes, err := strconv.ParseUint(strings.TrimSpace(output), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse Windows physical memory: %w", err)
		}
		return int64(bytes / (1024 * 1024 * 1024)), nil
	default:
		return 0, fmt.Errorf("unsupported operating system for physical memory: %s", runtime.GOOS)
	}
}

func linkBandwidthMbps(ctx context.Context) int64 {
	if runtime.GOOS == "linux" {
		return linuxLinkBandwidthMbps()
	}
	if runtime.GOOS == "windows" {
		return windowsLinkBandwidthMbps(ctx)
	}

	return 0
}

func linuxLinkBandwidthMbps() int64 {
	interfaces, err := net.Interfaces()
	if err != nil {
		return 0
	}

	var maximum int64
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagLoopback != 0 || networkInterface.Name == "lo" {
			continue
		}
		rawSpeed, err := os.ReadFile(filepath.Join("/sys/class/net", networkInterface.Name, "speed"))
		if err != nil {
			continue
		}
		speed, err := strconv.ParseInt(strings.TrimSpace(string(rawSpeed)), 10, 64)
		if err == nil && speed > maximum {
			maximum = speed
		}
	}

	return maximum
}

func windowsLinkBandwidthMbps(ctx context.Context) int64 {
	output, err := runPowerShell(ctx, "Get-NetAdapter -ErrorAction SilentlyContinue | Where-Object Status -eq 'Up' | Select-Object -ExpandProperty LinkSpeed")
	if err != nil {
		return 0
	}

	var maximum int64
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		value, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		multiplier := float64(1)
		switch strings.ToUpper(fields[1]) {
		case "GBPS":
			multiplier = 1000
		case "MBPS":
			multiplier = 1
		case "KBPS":
			multiplier = 0.001
		default:
			continue
		}
		if speed := int64(value * multiplier); speed > maximum {
			maximum = speed
		}
	}

	return maximum
}

func runPowerShell(ctx context.Context, script string) (string, error) {
	command := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	output, err := command.Output()
	if err != nil {
		return "", err
	}

	return string(output), nil
}

var _ ports.MachineCapacityReader = (*MachineCapacityReader)(nil)
