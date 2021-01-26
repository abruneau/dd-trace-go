// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016 Datadog, Inc.

package profiler

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"runtime/pprof"
	"time"

	pprofile "github.com/google/pprof/profile"
	"gopkg.in/DataDog/dd-trace-go.v1/profiler/internal/stackparse"
)

// ProfileType represents a type of profile that the profiler is able to run.
type ProfileType int

const (
	// HeapProfile reports memory allocation samples; used to monitor current
	// and historical memory usage, and to check for memory leaks.
	HeapProfile ProfileType = iota
	// CPUProfile determines where a program spends its time while actively consuming
	// CPU cycles (as opposed to while sleeping or waiting for I/O).
	CPUProfile
	// BlockProfile shows where goroutines block waiting on synchronization primitives
	// (including timer channels). Block profile is not enabled by default.
	BlockProfile
	// MutexProfile reports the lock contentions. When you think your CPU is not fully utilized due
	// to a mutex contention, use this profile. Mutex profile is not enabled by default.
	MutexProfile
	// GoroutineProfile reports stack traces of all current goroutines
	GoroutineProfile
	// GoroutineWaitProfile reports stack traces and wait durations for
	// goroutines that have been waiting or blocked by a syscall for > 1 minute
	// since the last GC.
	GoroutineWaitProfile
	// MetricsProfile reports top-line metrics associated with user-specified profiles
	MetricsProfile
)

func (t ProfileType) String() string {
	switch t {
	case HeapProfile:
		return "heap"
	case CPUProfile:
		return "cpu"
	case MutexProfile:
		return "mutex"
	case BlockProfile:
		return "block"
	case GoroutineProfile:
		return "goroutine"
	case GoroutineWaitProfile:
		return "goroutinewait"
	case MetricsProfile:
		return "metrics"
	default:
		return "unknown"
	}
}

// Filename is the identifier used on upload.
func (t ProfileType) Filename() string {
	// There are subtle differences between the root and String() (see GoroutineProfile)
	switch t {
	case HeapProfile:
		return "heap.pprof"
	case CPUProfile:
		return "cpu.pprof"
	case MutexProfile:
		return "mutex.pprof"
	case BlockProfile:
		return "block.pprof"
	case GoroutineProfile:
		return "goroutines.pprof"
	case GoroutineWaitProfile:
		return "goroutineswait.pprof"
	case MetricsProfile:
		return "metrics.json"
	default:
		return "unknown"
	}
}

// Tag used on profile metadata
func (t ProfileType) Tag() string {
	return fmt.Sprintf("profile_type:%s", t)
}

// profile specifies a profiles data (gzipped protobuf, json), and the types contained within it.
type profile struct {
	// name indicates profile type and format (e.g. cpu.pprof, metrics.json)
	name string
	data []byte
}

// batch is a collection of profiles of different types, collected at roughly the same time. It maps
// to what the Datadog UI calls a profile.
type batch struct {
	start, end time.Time
	host       string
	profiles   []*profile
}

func (b *batch) addProfile(p *profile) {
	b.profiles = append(b.profiles, p)
}

func (p *profiler) runProfile(t ProfileType) (*profile, error) {
	switch t {
	case HeapProfile:
		return heapProfile(p.cfg)
	case CPUProfile:
		return cpuProfile(p.cfg)
	case MutexProfile:
		return mutexProfile(p.cfg)
	case BlockProfile:
		return blockProfile(p.cfg)
	case GoroutineProfile:
		return goroutineProfile(p.cfg)
	case GoroutineWaitProfile:
		return goroutineWaitProfile(p.cfg)
	case MetricsProfile:
		return p.collectMetrics()
	default:
		return nil, errors.New("profile type not implemented")
	}
}

// writeHeapProfile writes the heap profile; replaced in tests
var writeHeapProfile = pprof.WriteHeapProfile

func heapProfile(cfg *config) (*profile, error) {
	var buf bytes.Buffer
	start := now()
	if err := writeHeapProfile(&buf); err != nil {
		return nil, err
	}
	end := now()
	tags := append(cfg.tags, HeapProfile.Tag())
	cfg.statsd.Timing("datadog.profiler.go.collect_time", end.Sub(start), tags, 1)
	return &profile{
		name: HeapProfile.Filename(),
		data: buf.Bytes(),
	}, nil
}

var (
	// startCPUProfile starts the CPU profile; replaced in tests
	startCPUProfile = pprof.StartCPUProfile
	// stopCPUProfile stops the CPU profile; replaced in tests
	stopCPUProfile = pprof.StopCPUProfile
)

func cpuProfile(cfg *config) (*profile, error) {
	var buf bytes.Buffer
	start := now()
	if err := startCPUProfile(&buf); err != nil {
		return nil, err
	}
	time.Sleep(cfg.cpuDuration)
	stopCPUProfile()
	end := now()
	tags := append(cfg.tags, CPUProfile.Tag())
	cfg.statsd.Timing("datadog.profiler.go.collect_time", end.Sub(start), tags, 1)
	return &profile{
		name: CPUProfile.Filename(),
		data: buf.Bytes(),
	}, nil
}

// lookpupProfile looks up the profile with the given name and writes it to w. It returns
// any errors encountered in the process. It is replaced in tests.
var lookupProfile = func(name string, w io.Writer, debug int) error {
	prof := pprof.Lookup(name)
	if prof == nil {
		return errors.New("profile not found")
	}
	return prof.WriteTo(w, debug)
}

func blockProfile(cfg *config) (*profile, error) {
	var buf bytes.Buffer
	start := now()
	if err := lookupProfile(BlockProfile.String(), &buf, 0); err != nil {
		return nil, err
	}
	end := now()
	tags := append(cfg.tags, BlockProfile.Tag())
	cfg.statsd.Timing("datadog.profiler.go.collect_time", end.Sub(start), tags, 1)
	return &profile{
		name: BlockProfile.Filename(),
		data: buf.Bytes(),
	}, nil
}

func mutexProfile(cfg *config) (*profile, error) {
	var buf bytes.Buffer
	start := now()
	if err := lookupProfile(MutexProfile.String(), &buf, 0); err != nil {
		return nil, err
	}
	end := now()
	tags := append(cfg.tags, MutexProfile.Tag())
	cfg.statsd.Timing("datadog.profiler.go.collect_time", end.Sub(start), tags, 1)
	return &profile{
		name: MutexProfile.Filename(),
		data: buf.Bytes(),
	}, nil
}

func goroutineProfile(cfg *config) (*profile, error) {
	var buf bytes.Buffer
	start := now()
	if err := lookupProfile(GoroutineProfile.String(), &buf, 0); err != nil {
		return nil, err
	}
	end := now()
	tags := append(cfg.tags, GoroutineProfile.Tag())
	cfg.statsd.Timing("datadog.profiler.go.collect_time", end.Sub(start), tags, 1)
	return &profile{
		name: GoroutineProfile.Filename(),
		data: buf.Bytes(),
	}, nil
}

func goroutineWaitProfile(cfg *config) (*profile, error) {
	var (
		text  = &bytes.Buffer{}
		pprof = &bytes.Buffer{}
		start = now()
	)
	if err := lookupProfile(GoroutineProfile.String(), text, 2); err != nil {
		return nil, err
	} else if err := goroutineDebug2ToPprof(text, pprof); err != nil {
		return nil, err
	}
	end := now()
	tags := append(cfg.tags, GoroutineWaitProfile.Tag())
	cfg.statsd.Timing("datadog.profiler.go.collect_time", end.Sub(start), tags, 1)

	return &profile{
		name: GoroutineWaitProfile.Filename(),
		data: pprof.Bytes(),
	}, nil
}

func goroutineDebug2ToPprof(r io.Reader, w io.Writer) error {
	goroutines, errs := stackparse.Parse(r)

	functionID := uint64(1)
	locationID := uint64(1)

	p := &pprofile.Profile{}
	m := &pprofile.Mapping{ID: 1, HasFunctions: true}
	p.Mapping = []*pprofile.Mapping{m}
	p.SampleType = []*pprofile.ValueType{
		{
			Type: "waitduration",
			Unit: "nanoseconds",
		},
	}

	for _, g := range goroutines {
		// TODO(fg) exclude goroutines w/ g.Wait == 0?

		sample := &pprofile.Sample{
			Value: []int64{g.Wait.Nanoseconds()},
			Label: map[string][]string{
				"state": {g.State}, // TODO(fg) split into atomicstatus/waitreason?
			},
			NumUnit:  map[string][]string{"goid": {"id"}},
			NumLabel: map[string][]int64{"goid": {int64(g.ID)}},
			// TODO(fg) add g.LockedToThread, g.CreatedBy, ...?
		}

		// Based on internal discussion, the current strategy is to use virtual
		// frames to indicate truncated stacks, see [1] for how python/jd does it.
		// [1] https://github.com/DataDog/dd-trace-py/blob/e933d2485b9019a7afad7127f7c0eb541341cdb7/ddtrace/profiling/exporter/pprof.pyx#L117-L121
		if g.FramesElided {
			g.Stack = append(g.Stack, &stackparse.Frame{
				Func: "...additional frames elided...",
			})
		}

		for _, call := range g.Stack {
			function := &pprofile.Function{
				ID:       functionID,
				Name:     call.Func,
				Filename: call.File,
			}
			p.Function = append(p.Function, function)
			functionID++

			location := &pprofile.Location{
				ID:      locationID,
				Mapping: m,
				Line: []pprofile.Line{{
					Function: function,
					Line:     int64(call.Line),
				}},
			}
			p.Location = append(p.Location, location)
			locationID++

			sample.Location = append([]*pprofile.Location{location}, sample.Location...)
		}

		p.Sample = append(p.Sample, sample)
	}

	if errs != nil {
		for _, err := range errs.Errors {
			p.Comments = append(p.Comments, "error: "+err.Error())
		}
	}

	if err := p.CheckValid(); err != nil {
		return fmt.Errorf("marshalGoroutineDebug2Profile: %s", err)
	} else if err := p.Write(w); err != nil {
		return fmt.Errorf("marshalGoroutineDebug2Profile: %s", err)
	}
	return nil
}

func (p *profiler) collectMetrics() (*profile, error) {
	var buf bytes.Buffer
	start := now()
	if err := p.met.report(start, &buf); err != nil {
		return nil, err
	}
	end := now()
	tags := append(p.cfg.tags, MetricsProfile.Tag())
	p.cfg.statsd.Timing("datadog.profiler.go.collect_time", end.Sub(start), tags, 1)
	return &profile{
		name: MetricsProfile.Filename(),
		data: buf.Bytes(),
	}, nil
}

// now returns current time in UTC.
func now() time.Time {
	return time.Now().UTC()
}
