package memory

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"anti2api-golang/refactor/internal/logger"
)

// Config 提供集中式的内存回收配置。
//
// 目标：在 Docker 等严格内存限制场景下，更积极地把空闲堆内存归还给操作系统。
type Config struct {
	// TargetHeapRetention 表示允许保留的“额外堆占用”比例。
	// 例如 0.2 表示允许 retained ≈ heapInuse * (1 + 0.2)；超出时将触发 FreeOSMemory。
	TargetHeapRetention float64
	// FreeOSMemoryInterval 表示后台监控与尝试归还内存的周期。
	FreeOSMemoryInterval time.Duration
	// ForceGCThreshold 表示当 HeapAlloc 超过该阈值时，后台会主动触发一次 GC（不一定归还给 OS）。
	// 0 表示自动/禁用（会根据 GOMEMLIMIT 推导一个默认阈值）。
	ForceGCThreshold int64
}

const (
	defaultTargetHeapRetention  = 0.2
	defaultFreeOSMemoryInterval = 5 * time.Second

	defaultLargeRequestThreshold = int64(1 << 20) // 1MiB
	defaultAggressiveLimitFrac   = 0.8

	// 避免在低内存场景反复触发 FreeOSMemory 的绝对阈值（空闲堆超过该值才尝试归还）。
	// 8MiB 对于容器环境来说是合理的阈值，可以更积极地归还内存给 OS。
	defaultMinIdleReclaimBytes = int64(8 << 20) // 8MiB
	// Rate limit：避免频繁 FreeOSMemory 造成额外开销/抖动。
	defaultMinReclaimInterval = 2 * time.Second
	// Rate limit：避免频繁 runtime.GC()。
	defaultMinGCInterval = 750 * time.Millisecond
)

type reclaimSignal struct {
	reason         string
	allocatedBytes int64
}

type manager struct {
	cfg Config

	largeRequestThreshold int64
	aggressiveLimitFrac   float64

	memLimitBytes       int64
	minIdleReclaimBytes int64
	minReclaimInterval  time.Duration
	minGCInterval       time.Duration

	lastReclaimUnixNano atomic.Int64
	lastGCUnixNano      atomic.Int64

	mu sync.Mutex
	ch chan reclaimSignal
}

var (
	once sync.Once
	mgr  *manager
)

func Init() {
	once.Do(func() {
		cfg := defaultConfig()

		// 解析（或自动推导）GOMEMLIMIT，用于更激进的回收策略判断。
		memLimitBytes, ok := parseByteSize(os.Getenv("GOMEMLIMIT"))
		if !ok || memLimitBytes <= 0 {
			// 允许用 GOMEMLIMIT=auto 开启自动推导。
			if strings.EqualFold(strings.TrimSpace(os.Getenv("GOMEMLIMIT")), "auto") || os.Getenv("GOMEMLIMIT") == "" {
				if cgroupLimit, ok2 := detectCgroupMemoryLimit(); ok2 && cgroupLimit > 0 {
					autoLimit := (cgroupLimit * 8) / 10
					if autoLimit > 0 {
						_ = debug.SetMemoryLimit(autoLimit)
						memLimitBytes = autoLimit
					}
				}
			}
		}

		// ForceGCThreshold 默认值：若存在可用的 GOMEMLIMIT，则在接近限制前提前触发一次 GC。
		if cfg.ForceGCThreshold <= 0 && memLimitBytes > 0 {
			cfg.ForceGCThreshold = (memLimitBytes * 7) / 10 // 70%
		}

		if cfg.TargetHeapRetention <= 0 || cfg.TargetHeapRetention >= 1 {
			cfg.TargetHeapRetention = defaultTargetHeapRetention
		}
		if cfg.FreeOSMemoryInterval <= 0 {
			cfg.FreeOSMemoryInterval = defaultFreeOSMemoryInterval
		}

		mgr = &manager{
			cfg:                   cfg,
			largeRequestThreshold: defaultLargeRequestThreshold,
			aggressiveLimitFrac:   defaultAggressiveLimitFrac,
			memLimitBytes:         memLimitBytes,
			minIdleReclaimBytes:   defaultMinIdleReclaimBytes,
			minReclaimInterval:    defaultMinReclaimInterval,
			minGCInterval:         defaultMinGCInterval,
			ch:                    make(chan reclaimSignal, 1),
		}

		go mgr.run()
	})
}

func defaultConfig() Config {
	return Config{
		TargetHeapRetention:  defaultTargetHeapRetention,
		FreeOSMemoryInterval: defaultFreeOSMemoryInterval,
		ForceGCThreshold:     0,
	}
}

// AfterLargeRequest 在大请求完成后调用（建议用 defer），用于异步触发更激进的回收。
//
// 注意：该函数不会在调用方 goroutine 内执行 GC/FreeOSMemory，以避免影响请求延迟。
func AfterLargeRequest(allocatedBytes int64) {
	Init()
	m := mgr
	if m == nil {
		return
	}
	if allocatedBytes < m.largeRequestThreshold {
		return
	}
	select {
	case m.ch <- reclaimSignal{reason: "after_large_request", allocatedBytes: allocatedBytes}:
	default:
		// 丢弃重复信号，避免堆积。
	}
}

// ForceReclaim 立即尝试回收：触发 GC + FreeOSMemory（同步执行，谨慎在请求路径调用）。
func ForceReclaim() {
	Init()
	m := mgr
	if m == nil {
		return
	}
	m.freeOSMemory(true, "force_reclaim")
}

func (m *manager) run() {
	ticker := time.NewTicker(m.cfg.FreeOSMemoryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.onTick()
		case sig := <-m.ch:
			// 对于非常大的请求，允许更激进（绕过部分 rate-limit）。
			force := sig.allocatedBytes >= 16*m.largeRequestThreshold
			m.freeOSMemory(force, sig.reason)
		}
	}
}

func (m *manager) onTick() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	heapAlloc := int64(ms.HeapAlloc)
	if m.cfg.ForceGCThreshold > 0 && heapAlloc >= m.cfg.ForceGCThreshold {
		m.forceGC(false, "heap_alloc_threshold")
	}

	heapInuse := int64(ms.HeapInuse)
	retained := int64(ms.HeapSys) - int64(ms.HeapReleased)
	if retained < 0 {
		retained = 0
	}
	idleRetained := retained - heapInuse
	if idleRetained < 0 {
		idleRetained = 0
	}

	shouldFree := false
	if heapInuse > 0 && m.cfg.TargetHeapRetention > 0 {
		maxRetained := heapInuse + int64(float64(heapInuse)*m.cfg.TargetHeapRetention)
		if retained > maxRetained && idleRetained >= m.minIdleReclaimBytes {
			shouldFree = true
		}
	}

	if m.memLimitBytes > 0 && m.aggressiveLimitFrac > 0 {
		aggressiveAt := int64(float64(m.memLimitBytes) * m.aggressiveLimitFrac)
		if aggressiveAt > 0 && heapAlloc >= aggressiveAt {
			shouldFree = true
		}
	}

	if shouldFree {
		// Tick 场景不强制，避免频繁触发。
		m.freeOSMemory(false, "monitor")
	}
}

func (m *manager) forceGC(force bool, reason string) {
	now := time.Now()
	last := time.Unix(0, m.lastGCUnixNano.Load())
	if !force && now.Sub(last) < m.minGCInterval {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	last = time.Unix(0, m.lastGCUnixNano.Load())
	if !force && now.Sub(last) < m.minGCInterval {
		return
	}

	runtime.GC()
	m.lastGCUnixNano.Store(now.UnixNano())

	if logger.GetLevel() >= logger.LogLow {
		logger.Debug("memory: runtime.GC reason=%s", reason)
	}
}

func (m *manager) freeOSMemory(force bool, reason string) {
	now := time.Now()
	last := time.Unix(0, m.lastReclaimUnixNano.Load())
	if !force && now.Sub(last) < m.minReclaimInterval {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	last = time.Unix(0, m.lastReclaimUnixNano.Load())
	if !force && now.Sub(last) < m.minReclaimInterval {
		return
	}

	logEnabled := logger.GetLevel() >= logger.LogLow
	var before runtime.MemStats
	if logEnabled {
		runtime.ReadMemStats(&before)
	}

	debug.FreeOSMemory()
	m.lastReclaimUnixNano.Store(now.UnixNano())

	if logEnabled {
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		logger.Debug(
			"memory: FreeOSMemory reason=%s heapAlloc=%s->%s retained=%s->%s heapReleased=%s->%s",
			reason,
			formatBytes(before.HeapAlloc),
			formatBytes(after.HeapAlloc),
			formatBytes(retainedBytes(before)),
			formatBytes(retainedBytes(after)),
			formatBytes(before.HeapReleased),
			formatBytes(after.HeapReleased),
		)
	}
}

func retainedBytes(ms runtime.MemStats) uint64 {
	if ms.HeapSys < ms.HeapReleased {
		return 0
	}
	return ms.HeapSys - ms.HeapReleased
}

func formatBytes(b uint64) string {
	const MiB = 1024 * 1024
	if b < MiB {
		return fmt.Sprintf("%dB", b)
	}
	return fmt.Sprintf("%.1fMiB", float64(b)/MiB)
}

func detectCgroupMemoryLimit() (int64, bool) {
	// cgroups v2
	if b, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		s := strings.TrimSpace(string(b))
		if s != "" && s != "max" {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 && v < (1<<62) {
				return v, true
			}
		}
	}
	// cgroups v1
	if b, err := os.ReadFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"); err == nil {
		s := strings.TrimSpace(string(b))
		if s != "" {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 && v < (1<<62) {
				// 一些环境会返回一个极大的数字表示“无限制”；忽略这种情况。
				if v > (1 << 60) {
					return 0, false
				}
				return v, true
			}
		}
	}
	return 0, false
}

func parseByteSize(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}

	// Split numeric and unit parts.
	i := 0
	for i < len(s) {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' {
			i++
			continue
		}
		break
	}
	if i == 0 {
		return 0, false
	}

	numStr := strings.TrimSpace(s[:i])
	unitStr := strings.TrimSpace(s[i:])
	if unitStr == "" {
		unitStr = "B"
	}

	n, err := strconv.ParseFloat(numStr, 64)
	if err != nil || n <= 0 {
		return 0, false
	}

	mult, ok := byteSizeMultiplier(unitStr)
	if !ok {
		return 0, false
	}
	v := n * float64(mult)
	if v <= 0 {
		return 0, false
	}
	if v > float64(int64(^uint64(0)>>1)) {
		return 0, false
	}
	return int64(v), true
}

func byteSizeMultiplier(unit string) (int64, bool) {
	u := strings.ToLower(strings.TrimSpace(unit))
	switch u {
	case "b":
		return 1, true
	case "k", "kb":
		return 1000, true
	case "m", "mb":
		return 1000 * 1000, true
	case "g", "gb":
		return 1000 * 1000 * 1000, true
	case "t", "tb":
		return 1000 * 1000 * 1000 * 1000, true
	case "kib":
		return 1024, true
	case "mib":
		return 1024 * 1024, true
	case "gib":
		return 1024 * 1024 * 1024, true
	case "tib":
		return 1024 * 1024 * 1024 * 1024, true
	default:
		return 0, false
	}
}
