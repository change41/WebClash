package core

import (
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

const systemSampleInterval = 5 * time.Second

// SystemSnapshot is the host-level load snapshot exposed by /api/system/status.
// In Docker the figures come from the shared /proc, i.e. the host's view.
type SystemSnapshot struct {
	Ready     bool         `json:"ready"`
	UpdatedAt *int64       `json:"updatedAt"`
	CPU       SystemCPU    `json:"cpu"`
	Memory    SystemMemory `json:"memory"`
	Load      SystemLoad   `json:"load"`
}

type SystemCPU struct {
	Percent float64 `json:"percent"`
	Cores   int     `json:"cores"`
}

type SystemMemory struct {
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Percent float64 `json:"percent"`
}

type SystemLoad struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

// systemProbe gathers one raw sample; it is injectable for tests. CPUPercent
// reports ok=false on the baseline call (cpu.Percent(0) measures since the
// previous call, so the first result carries no window).
type systemProbe interface {
	CPUPercent() (float64, bool)
	Memory() (SystemMemory, error)
	Load() (SystemLoad, error)
}

type hostProbe struct {
	primed bool
}

func (p *hostProbe) CPUPercent() (float64, bool) {
	values, err := cpu.Percent(0, false)
	if err != nil || len(values) == 0 {
		return 0, false
	}
	if !p.primed {
		p.primed = true
		return 0, false
	}
	return values[0], true
}

func (p *hostProbe) Memory() (SystemMemory, error) {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return SystemMemory{}, err
	}
	return SystemMemory{Total: vm.Total, Used: vm.Used, Percent: vm.UsedPercent}, nil
}

func (p *hostProbe) Load() (SystemLoad, error) {
	avg, err := load.Avg()
	if err != nil {
		return SystemLoad{}, err
	}
	return SystemLoad{Load1: avg.Load1, Load5: avg.Load5, Load15: avg.Load15}, nil
}

// systemSampler polls host CPU/memory/load every 5s into a cached snapshot.
// A failed metric keeps its previous value instead of zeroing the snapshot.
type systemSampler struct {
	probe systemProbe
	cores int

	mu       sync.Mutex
	started  bool
	snap     SystemSnapshot
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func newSystemSampler() *systemSampler {
	return newSystemSamplerWithProbe(&hostProbe{})
}

func newSystemSamplerWithProbe(p systemProbe) *systemSampler {
	cores := runtime.NumCPU()
	return &systemSampler{
		probe: p,
		cores: cores,
		snap:  SystemSnapshot{CPU: SystemCPU{Cores: cores}},
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
}

// Start samples once immediately (the CPU baseline) and then every 5s. Safe to
// call once; the second CPU sample is when ready turns true.
func (s *systemSampler) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	go func() {
		defer close(s.done)
		s.sample(time.Now().UnixMilli())
		t := time.NewTicker(systemSampleInterval)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				s.sample(time.Now().UnixMilli())
			}
		}
	}()
}

// Stop terminates the sampler and waits for it. Safe when never started and on
// repeated calls.
func (s *systemSampler) Stop() {
	s.mu.Lock()
	started := s.started
	s.started = false
	s.mu.Unlock()
	if !started {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stop)
		<-s.done
	})
}

func (s *systemSampler) sample(now int64) {
	cpuPercent, cpuOK := s.probe.CPUPercent()
	memory, memoryErr := s.probe.Memory()
	sysLoad, loadErr := s.probe.Load()

	s.mu.Lock()
	defer s.mu.Unlock()
	updated := false
	if cpuOK {
		s.snap.CPU.Percent = cpuPercent
		s.snap.Ready = true
		updated = true
	}
	if memoryErr == nil {
		s.snap.Memory = memory
		updated = true
	}
	if loadErr == nil {
		s.snap.Load = sysLoad
		updated = true
	}
	if updated {
		s.snap.UpdatedAt = &now
	}
}

// Snapshot returns a copy of the latest sample.
func (s *systemSampler) Snapshot() SystemSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.snap
	snap.CPU.Cores = s.cores
	return snap
}

// SystemStatus returns the latest host CPU/memory/load sample.
func (c *Core) SystemStatus() SystemSnapshot {
	return c.system.Snapshot()
}
