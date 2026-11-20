package core

import (
	"errors"
	"runtime"
	"testing"
)

type fakeProbe struct {
	cpuCalls  int
	cpuValue  float64
	memory    SystemMemory
	memoryErr bool
	load      SystemLoad
	loadErr   bool
}

func (f *fakeProbe) CPUPercent() (float64, bool) {
	f.cpuCalls++
	if f.cpuCalls == 1 {
		return 0, false
	}
	return f.cpuValue, true
}

func (f *fakeProbe) Memory() (SystemMemory, error) {
	if f.memoryErr {
		return SystemMemory{}, errors.New("memory probe failed")
	}
	return f.memory, nil
}

func (f *fakeProbe) Load() (SystemLoad, error) {
	if f.loadErr {
		return SystemLoad{}, errors.New("load probe failed")
	}
	return f.load, nil
}

func TestSystemSamplerReadyAfterBaseline(t *testing.T) {
	probe := &fakeProbe{
		cpuValue: 12.5,
		memory:   SystemMemory{Total: 1000, Used: 400, Percent: 40},
		load:     SystemLoad{Load1: 0.5, Load5: 0.7, Load15: 0.9},
	}
	s := newSystemSamplerWithProbe(probe)

	s.sample(1000)
	snap := s.Snapshot()
	if snap.Ready {
		t.Fatalf("ready after baseline sample, want false")
	}
	if snap.Memory.Total != 1000 || snap.Memory.Used != 400 || snap.Memory.Percent != 40 {
		t.Errorf("memory = %+v, want 1000/400/40", snap.Memory)
	}
	if snap.CPU.Cores != runtime.NumCPU() {
		t.Errorf("cores = %d, want %d", snap.CPU.Cores, runtime.NumCPU())
	}
	if snap.UpdatedAt == nil || *snap.UpdatedAt != 1000 {
		t.Errorf("updatedAt = %v, want 1000", snap.UpdatedAt)
	}

	s.sample(2000)
	snap = s.Snapshot()
	if !snap.Ready {
		t.Fatalf("ready = false after second CPU sample")
	}
	if snap.CPU.Percent != 12.5 {
		t.Errorf("cpu = %v, want 12.5", snap.CPU.Percent)
	}
	if snap.Load != (SystemLoad{Load1: 0.5, Load5: 0.7, Load15: 0.9}) {
		t.Errorf("load = %+v", snap.Load)
	}
}

func TestSystemSamplerKeepsLastValueOnProbeFailure(t *testing.T) {
	probe := &fakeProbe{
		cpuValue: 10,
		memory:   SystemMemory{Total: 2000, Used: 500, Percent: 25},
		load:     SystemLoad{Load1: 1, Load5: 2, Load15: 3},
	}
	s := newSystemSamplerWithProbe(probe)
	s.sample(1000)
	s.sample(2000)

	probe.cpuValue = 30
	probe.memoryErr = true
	probe.loadErr = true
	s.sample(3000)

	snap := s.Snapshot()
	if snap.CPU.Percent != 30 {
		t.Errorf("cpu = %v, want 30", snap.CPU.Percent)
	}
	if snap.Memory.Total != 2000 || snap.Memory.Used != 500 || snap.Memory.Percent != 25 {
		t.Errorf("memory = %+v, want previous sample retained", snap.Memory)
	}
	if snap.Load != (SystemLoad{Load1: 1, Load5: 2, Load15: 3}) {
		t.Errorf("load = %+v, want previous sample retained", snap.Load)
	}
	if snap.UpdatedAt == nil || *snap.UpdatedAt != 3000 {
		t.Errorf("updatedAt = %v, want 3000", snap.UpdatedAt)
	}
}
