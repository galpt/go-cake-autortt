package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/VictoriaMetrics/fastcache"
)

// CakeAutoRTTService represents the main service
type CakeAutoRTTService struct {
	config      *Config
	running     bool
	mutex       sync.RWMutex
	lastRTT     map[string]int
	activeHosts int
	lastUpdate  time.Time
	ctx         context.Context
	cancel      context.CancelFunc
	logMutex    sync.RWMutex
	// probe tracking
	probeMutex    sync.RWMutex
	currentProbes map[string]ProbeStatus
	// max snapshot entries to return for current probes (helps bound memory/use)
	currentProbesMaxEntries int
	// fastcache-backed storage for current probes (values marshaled JSON)
	currentProbeCache *fastcache.Cache
	// bounded queue of current probe keys (hosts) to allow iteration of recent/current probes
	currentProbeQueue []string
	// injectable probe function for testing (host, timeoutSec) -> (rtt, error)
	ProbeFunc func(host string, timeoutSec int) (time.Duration, error)
	// adaptive worker cap managed by background controller
	adaptiveWorkers int
	// completed probes buffer (recent finished probes for UI). protected by probeMutex
	completedProbes []CompletedProbe
	// how long to keep completed probes in seconds
	completedRetentionSec int
	// max completed entries to retain
	completedMaxEntries int
	// cpuReader is injectable for tests; returns total,idle or error
	cpuReader func() (uint64, uint64, error)
	// cpu sampling interval used by adaptive controller (injectable for tests)
	cpuSampleInterval time.Duration
	// fastcache-backed storage for recent logs
	recentLogCache *fastcache.Cache
	// bounded queue of recent log sequence IDs
	recentLogQueue []uint64
	// max recent logs to keep in queue (also used historically to maintain compatibility)
	recentLogsMaxEntries int
	// atomic sequence for log keys
	recentLogSeq uint64
}

// LogEntry represents a log entry
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// SystemStatus represents the current system status
type SystemStatus struct {
	Running     bool           `json:"running"`
	LastUpdate  time.Time      `json:"last_update"`
	CurrentRTT  map[string]int `json:"current_rtt"`
	ActiveHosts int            `json:"active_hosts"`
	DLInterface string         `json:"dl_interface"`
	ULInterface string         `json:"ul_interface"`
	Config      *Config        `json:"config"`
}

// RTTMeasurement represents a single RTT measurement
type RTTMeasurement struct {
	Host string
	RTT  time.Duration
	Err  error
}

// ProbeStatus represents the current state of a probe for the UI
type ProbeStatus struct {
	Host  string `json:"host"`
	Stage string `json:"stage"`
	RTTMs int    `json:"rtt_ms,omitempty"`
	Error string `json:"error,omitempty"`
}

// CompletedProbe holds a probe result with a timestamp for time-based retention
type CompletedProbe struct {
	Probe ProbeStatus
	When  time.Time
}

// NewCakeAutoRTTService creates a new service instance
func NewCakeAutoRTTService(config *Config) (*CakeAutoRTTService, error) {
	ctx, cancel := context.WithCancel(context.Background())
	service := &CakeAutoRTTService{
		config:     config,
		running:    false,
		lastRTT:    make(map[string]int),
		lastUpdate: time.Now().Local(),
		ctx:        ctx,
		cancel:     cancel,
		// recent logs are stored in fastcache+queue
		recentLogsMaxEntries:    100,
		recentLogQueue:          make([]uint64, 0, 100),
		recentLogCache:          fastcache.New(32 << 20),
		currentProbes:           make(map[string]ProbeStatus),
		currentProbesMaxEntries: 100, // limit snapshot size by default
		currentProbeCache:       fastcache.New(32 << 20),
		currentProbeQueue:       make([]string, 0, 100),
	}

	// default probe function uses the internal TCP probe implementation
	service.ProbeFunc = func(h string, timeoutSec int) (time.Duration, error) {
		return service.measureSingleHostTCP(h, timeoutSec)
	}

	// initialize adaptive worker cap to configured max
	service.mutex.RLock()
	service.adaptiveWorkers = service.config.MaxConcurrentProbes
	service.mutex.RUnlock()

	// setup completed probes retention defaults
	service.completedRetentionSec = 5 // keep completed probes visible for 5s by default
	service.completedMaxEntries = 50  // keep up to 50 completed entries

	// Start adaptive controller in background (best-effort; will no-op if /proc not available)
	if service.config.AdaptiveControllerEnabled {
		// default cpu reader reads /proc/stat
		service.cpuReader = func() (uint64, uint64, error) {
			data, err := os.ReadFile("/proc/stat")
			if err != nil {
				return 0, 0, err
			}
			lines := strings.Split(string(data), "\n")
			if len(lines) == 0 {
				return 0, 0, fmt.Errorf("unexpected /proc/stat format")
			}
			fields := strings.Fields(lines[0])
			if len(fields) < 5 || fields[0] != "cpu" {
				return 0, 0, fmt.Errorf("unexpected /proc/stat format")
			}
			var total uint64
			var idle uint64
			for i := 1; i < len(fields); i++ {
				var v uint64
				_, err := fmt.Sscan(fields[i], &v)
				if err != nil {
					return 0, 0, err
				}
				total += v
				if i == 4 {
					idle = v
				}
			}
			return total, idle, nil
		}
		// default sample interval
		service.cpuSampleInterval = 2 * time.Second
		go service.startAdaptiveController()
	}

	// Start a background goroutine to prune completed probes periodically
	go service.startCompletedPruner()

	// Auto-detect interfaces if not specified
	if err := service.autoDetectInterfaces(); err != nil {
		return nil, fmt.Errorf("failed to auto-detect interfaces: %w", err)
	}

	return service, nil
}

// Run starts the main service loop
func (s *CakeAutoRTTService) Run(ctx context.Context) error {
	s.mutex.Lock()
	s.running = true
	s.mutex.Unlock()

	s.AddLog("INFO", "Starting cake-autortt main loop")
	s.AddLog("INFO", fmt.Sprintf("Detected interfaces - DL: %s, UL: %s", s.config.DLInterface, s.config.ULInterface))

	ticker := time.NewTicker(time.Duration(s.config.RTTUpdateInterval) * time.Second)
	defer ticker.Stop()

	// Run initial measurement
	s.performRTTMeasurementCycle()

	for {
		select {
		case <-ctx.Done():
			s.AddLog("INFO", "Service stopped")
			return nil
		case <-ticker.C:
			s.performRTTMeasurementCycle()
			s.mutex.Lock()
			s.lastUpdate = time.Now().Local()
			s.mutex.Unlock()
		}
	}
}

// performRTTMeasurementCycle performs one complete RTT measurement and adjustment cycle
func (s *CakeAutoRTTService) performRTTMeasurementCycle() {
	// Extract hosts from conntrack
	hosts, err := s.extractHostsFromConntrack()
	if err != nil {
		s.AddLog("ERROR", fmt.Sprintf("Failed to extract hosts from conntrack: %v", err))
		return
	}

	s.AddLog("DEBUG", fmt.Sprintf("Found %d non-LAN hosts", len(hosts)))

	var rttToUse float64 = float64(s.config.DefaultRTTMs)
	shouldUpdate := true

	// Measure RTT if we have enough hosts
	if len(hosts) >= s.config.MinHosts {
		measuredRTT, activeCount, err := s.measureRTTTCP(hosts)
		if err != nil {
			s.AddLog("DEBUG", fmt.Sprintf("RTT measurement failed: %v, using default RTT: %.2fms", err, rttToUse))
			// Update RTT tracking with default
			s.mutex.Lock()
			s.lastRTT["default"] = int(rttToUse)
			s.activeHosts = activeCount // Use the count from failed measurement (partial success)
			s.mutex.Unlock()
		} else {
			rttToUse = measuredRTT
			s.AddLog("DEBUG", fmt.Sprintf("Using measured RTT: %.2fms", rttToUse))
			// Update RTT tracking with measured value
			s.mutex.Lock()
			s.lastRTT["measured"] = int(rttToUse)
			s.activeHosts = activeCount // Use the actual count from successful measurement
			s.mutex.Unlock()
		}
	} else {
		s.AddLog("DEBUG", fmt.Sprintf("Not enough hosts (%d < %d), using default RTT: %.2fms",
			len(hosts), s.config.MinHosts, rttToUse))
		// Update RTT tracking with default
		s.mutex.Lock()
		s.lastRTT["default"] = int(rttToUse)
		s.activeHosts = len(hosts) // Show discovered hosts even if not enough for measurement
		s.mutex.Unlock()
	}

	// Update CAKE RTT parameter
	if shouldUpdate {
		if err := s.adjustCakeRTT(rttToUse); err != nil {
			s.AddLog("ERROR", fmt.Sprintf("Failed to adjust CAKE RTT: %v", err))
		}
	}
}

// extractHostsFromConntrack parses /proc/net/nf_conntrack to extract non-LAN destination addresses
func (s *CakeAutoRTTService) extractHostsFromConntrack() ([]string, error) {
	file, err := os.Open("/proc/net/nf_conntrack")
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc/net/nf_conntrack: %w", err)
	}
	defer file.Close()

	hostSet := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	dstRegex := regexp.MustCompile(`dst=([0-9a-fA-F:.]+)`)

	for scanner.Scan() {
		line := scanner.Text()

		// Only process ESTABLISHED connections
		if !strings.Contains(line, "ESTABLISHED") {
			continue
		}

		// Extract destination IP
		matches := dstRegex.FindStringSubmatch(line)
		if len(matches) < 2 {
			continue
		}

		dstIP := matches[1]
		if !s.isLANAddress(dstIP) {
			hostSet[dstIP] = true
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading conntrack: %w", err)
	}

	// Convert to slice and limit to max hosts (read max from config under lock)
	s.mutex.RLock()
	maxHosts := s.config.MaxHosts
	s.mutex.RUnlock()

	hosts := make([]string, 0, len(hostSet))
	for host := range hostSet {
		hosts = append(hosts, host)
		if len(hosts) >= maxHosts {
			break
		}
	}

	return hosts, nil
}

// isLANAddress checks if an IP address is a LAN address
func (s *CakeAutoRTTService) isLANAddress(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return true // Invalid IP, treat as LAN
	}

	// Check for IPv4 private ranges
	if ip.To4() != nil {
		// 10.0.0.0/8
		if ip[12] == 10 {
			return true
		}
		// 172.16.0.0/12
		if ip[12] == 172 && ip[13] >= 16 && ip[13] <= 31 {
			return true
		}
		// 192.168.0.0/16
		if ip[12] == 192 && ip[13] == 168 {
			return true
		}
		// 169.254.0.0/16 (link-local)
		if ip[12] == 169 && ip[13] == 254 {
			return true
		}
		// 127.0.0.0/8 (loopback)
		if ip[12] == 127 {
			return true
		}
		// Multicast and reserved ranges
		if ip[12] >= 224 {
			return true
		}
	} else {
		// IPv6 checks
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return true
		}
		// fc00::/7 (unique local)
		if ip[0] == 0xfc || ip[0] == 0xfd {
			return true
		}
	}

	return false
}

// measureRTTTCP measures RTT using TCP connections to multiple hosts in parallel
// Returns the measured RTT, number of active hosts, and any error
func (s *CakeAutoRTTService) measureRTTTCP(hosts []string) (float64, int, error) {
	if len(hosts) == 0 {
		return 0, 0, fmt.Errorf("no hosts to measure")
	}

	s.AddLog("DEBUG", fmt.Sprintf("Measuring RTT using TCP for %d hosts", len(hosts)))

	// Worker-pool approach: create a bounded number of workers to avoid creating
	// thousands of goroutines and to control the probe rate.
	jobs := make(chan string, len(hosts))
	results := make(chan RTTMeasurement, len(hosts))

	// Read config under lock into local copy to avoid races while UpdateConfig runs
	s.mutex.RLock()
	cfg := *s.config
	s.mutex.RUnlock()

	// Determine number of workers (cap to a reasonable safety limit)
	workers := cfg.MaxConcurrentProbes
	if workers < 1 {
		workers = 1
	}
	// Safety cap to avoid overwhelming the system
	if workers > 500 {
		workers = 500
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			for h := range jobs {
				// Mark as probing
				s.setProbeStage(h, "probing")

				// Use injected probe function (defaults to internal TCP probe) so tests can mock it.
				rtt, err := s.ProbeFunc(h, cfg.TCPConnectTimeout)

				// Record result for UI and logs
				if err != nil {
					s.setProbeResult(h, 0, err)
				} else {
					s.setProbeResult(h, int(rtt.Nanoseconds()/1e6), nil)
				}

				results <- RTTMeasurement{Host: h, RTT: rtt, Err: err}

				// Small pacing to avoid synchronized bursts and excessive short-term load
				time.Sleep(time.Millisecond * time.Duration(10+(workerIdx%10)))
			}
		}(i)
	}

	// Enqueue jobs
	for _, h := range hosts {
		s.setProbeStage(h, "queued")
		jobs <- h
	}
	close(jobs)

	// Close results channel when workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var validRTTs []float64
	aliveCount := 0

	for result := range results {
		if result.Err != nil {
			s.AddLog("DEBUG", fmt.Sprintf("Host %s: %v", result.Host, result.Err))
			continue
		}

		rttMs := float64(result.RTT.Nanoseconds()) / 1e6
		validRTTs = append(validRTTs, rttMs)
		aliveCount++
		s.AddLog("DEBUG", fmt.Sprintf("Host %s: RTT %.2fms", result.Host, rttMs))
	}

	s.AddLog("DEBUG", fmt.Sprintf("TCP summary: %d/%d hosts alive", aliveCount, len(hosts)))

	// Check if we have enough responding hosts
	if aliveCount < s.config.MinHosts {
		return 0, aliveCount, fmt.Errorf("not enough responding hosts (%d < %d)", aliveCount, s.config.MinHosts)
	}

	// Calculate statistics
	sort.Float64s(validRTTs)
	avgRTT := 0.0
	for _, rtt := range validRTTs {
		avgRTT += rtt
	}
	avgRTT /= float64(len(validRTTs))

	// Use worst (highest) RTT for conservative approach
	worstRTT := validRTTs[len(validRTTs)-1]

	s.AddLog("DEBUG", fmt.Sprintf("Using worst RTT: %.2fms (avg: %.2fms, worst: %.2fms)",
		worstRTT, avgRTT, worstRTT))

	return worstRTT, aliveCount, nil
}

// setProbeStage sets the stage for a given probe host
func (s *CakeAutoRTTService) setProbeStage(host, stage string) {
	s.probeMutex.Lock()
	defer s.probeMutex.Unlock()

	// If the host is already being tracked, update its stage and cache.
	if ps, ok := s.currentProbes[host]; ok {
		ps.Host = host
		ps.Stage = stage
		ps.Error = ""
		ps.RTTMs = 0
		s.currentProbes[host] = ps
		if s.currentProbeCache != nil {
			if b, err := json.Marshal(ps); err == nil {
				s.currentProbeCache.Set([]byte(host), b)
			}
		}
		return
	}

	// If we're at capacity, evict the oldest tracked probe to make room (FIFO).
	if s.currentProbesMaxEntries > 0 && len(s.currentProbes) >= s.currentProbesMaxEntries {
		if len(s.currentProbeQueue) > 0 {
			evict := s.currentProbeQueue[0]
			s.currentProbeQueue = s.currentProbeQueue[1:]
			delete(s.currentProbes, evict)
			if s.currentProbeCache != nil {
				s.currentProbeCache.Del([]byte(evict))
			}
		}
	}

	// Create a new probe entry and add to queue/map/cache.
	ps := ProbeStatus{
		Host:  host,
		Stage: stage,
		Error: "",
		RTTMs: 0,
	}
	s.currentProbes[host] = ps
	s.currentProbeQueue = append(s.currentProbeQueue, host)
	if s.currentProbeCache != nil {
		if b, err := json.Marshal(ps); err == nil {
			s.currentProbeCache.Set([]byte(host), b)
		}
	}
}

// setProbeResult records the final probe result
func (s *CakeAutoRTTService) setProbeResult(host string, rttMs int, err error) {
	s.probeMutex.Lock()
	defer s.probeMutex.Unlock()

	ps := s.currentProbes[host]
	ps.Host = host
	if err != nil {
		ps.Stage = "failed"
		ps.Error = err.Error()
		ps.RTTMs = 0
	} else {
		ps.Stage = "done"
		ps.RTTMs = rttMs
		ps.Error = ""
	}

	// Record result transiently then remove from currentProbes to avoid unbounded map growth.
	if ps.Stage == "done" || ps.Stage == "failed" {
		// append timestamped completed probe to buffer
		s.completedProbes = append(s.completedProbes, CompletedProbe{Probe: ps, When: time.Now()})
		// trim if over limit
		if len(s.completedProbes) > s.completedMaxEntries {
			s.completedProbes = s.completedProbes[len(s.completedProbes)-s.completedMaxEntries:]
		}

		// remove from map
		delete(s.currentProbes, host)

		// remove from queue (linear search - queue is small)
		for i, h := range s.currentProbeQueue {
			if h == host {
				s.currentProbeQueue = append(s.currentProbeQueue[:i], s.currentProbeQueue[i+1:]...)
				break
			}
		}

		// delete from cache
		if s.currentProbeCache != nil {
			s.currentProbeCache.Del([]byte(host))
		}
	} else {
		s.currentProbes[host] = ps
		if s.currentProbeCache != nil {
			if b, err := json.Marshal(ps); err == nil {
				s.currentProbeCache.Set([]byte(host), b)
			}
		}
	}
}

// GetCurrentProbes returns a snapshot of current probes
func (s *CakeAutoRTTService) GetCurrentProbes() []ProbeStatus {
	s.probeMutex.RLock()
	defer s.probeMutex.RUnlock()
	out := make([]ProbeStatus, 0, len(s.currentProbes))
	for _, v := range s.currentProbes {
		out = append(out, v)
	}

	// If configured, limit the number of entries returned to avoid large payloads
	max := s.currentProbesMaxEntries
	if max <= 0 {
		// defensive: if misconfigured, fallback to returning everything
		return out
	}

	// Deterministic trimming: sort by host then trim to the last `max` entries.
	if len(out) > max {
		sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
		out = out[len(out)-max:]
	}

	return out
}

// GetRecentCompletedProbes returns a copy of recent completed probes (done/failed)
func (s *CakeAutoRTTService) GetRecentCompletedProbes() []ProbeStatus {
	s.probeMutex.RLock()
	defer s.probeMutex.RUnlock()

	if len(s.completedProbes) == 0 {
		return nil
	}

	cutoff := time.Now().Add(-time.Duration(s.completedRetentionSec) * time.Second)
	out := make([]ProbeStatus, 0, len(s.completedProbes))
	for _, cp := range s.completedProbes {
		if cp.When.Before(cutoff) {
			continue
		}
		out = append(out, cp.Probe)
	}

	// If more than max entries (shouldn't happen due to pruning), trim to last N
	if len(out) > s.completedMaxEntries {
		out = out[len(out)-s.completedMaxEntries:]
	}

	return out
}

// GetRecentCompletedProbesWithTime returns recent completed probes with a formatted timestamp
func (s *CakeAutoRTTService) GetRecentCompletedProbesWithTime() []map[string]interface{} {
	s.probeMutex.RLock()
	defer s.probeMutex.RUnlock()

	if len(s.completedProbes) == 0 {
		return nil
	}

	cutoff := time.Now().Add(-time.Duration(s.completedRetentionSec) * time.Second)
	out := make([]map[string]interface{}, 0, len(s.completedProbes))
	for _, cp := range s.completedProbes {
		if cp.When.Before(cutoff) {
			continue
		}
		entry := map[string]interface{}{
			"host":   cp.Probe.Host,
			"stage":  cp.Probe.Stage,
			"rtt_ms": cp.Probe.RTTMs,
			"error":  cp.Probe.Error,
			"when":   cp.When.Format("15:04:05"),
		}
		out = append(out, entry)
	}

	if len(out) > s.completedMaxEntries {
		out = out[len(out)-s.completedMaxEntries:]
	}

	return out
}

// measureSingleHostTCP measures RTT to a single host using TCP connection
func (s *CakeAutoRTTService) measureSingleHostTCP(host string, timeoutSec int) (time.Duration, error) {
	// Try common ports in order of preference
	ports := []string{"80", "443", "22", "21", "25", "53"}

	timeout := time.Duration(timeoutSec) * time.Second

	for _, port := range ports {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
		if err != nil {
			continue // Try next port
		}
		conn.Close()
		return time.Since(start), nil
	}

	return 0, fmt.Errorf("no reachable ports found")
}

// adjustCakeRTT adjusts the CAKE qdisc RTT parameter
func (s *CakeAutoRTTService) adjustCakeRTT(targetRTTMs float64) error {
	// Read relevant config fields under lock to avoid races with UpdateConfig
	s.mutex.RLock()
	margin := s.config.RTTMarginPercent
	dlIface := s.config.DLInterface
	ulIface := s.config.ULInterface
	s.mutex.RUnlock()

	// Add margin to measured RTT
	adjustedRTT := targetRTTMs * (1.0 + float64(margin)/100.0)

	// Convert to microseconds for tc command
	rttUs := int(adjustedRTT * 1000)

	s.AddLog("INFO", fmt.Sprintf("Adjusting CAKE RTT to %.2fms (%dus)", adjustedRTT, rttUs))

	// Update RTT tracking with final adjusted value
	s.mutex.Lock()
	s.lastRTT["final"] = int(adjustedRTT)
	s.mutex.Unlock()

	// Update download interface
	if dlIface != "" {
		if err := s.updateInterfaceRTT(dlIface, rttUs); err != nil {
			s.AddLog("ERROR", fmt.Sprintf("Failed to update RTT on download interface %s: %v",
				dlIface, err))
		} else {
			s.AddLog("DEBUG", fmt.Sprintf("Updated RTT on download interface %s", dlIface))
		}
	}

	// Update upload interface
	if ulIface != "" {
		if err := s.updateInterfaceRTT(ulIface, rttUs); err != nil {
			s.AddLog("ERROR", fmt.Sprintf("Failed to update RTT on upload interface %s: %v",
				ulIface, err))
		} else {
			s.AddLog("DEBUG", fmt.Sprintf("Updated RTT on upload interface %s", ulIface))
		}
	}

	return nil
}

// Stop stops the service
func (s *CakeAutoRTTService) Stop() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.running = false

	// Reset caches to release memory
	if s.recentLogCache != nil {
		s.recentLogCache.Reset()
	}
	if s.currentProbeCache != nil {
		s.currentProbeCache.Reset()
	}

	s.cancel()
}

// AddLog adds a log entry to the recent logs
func (s *CakeAutoRTTService) AddLog(level, message string) {
	s.logMutex.Lock()
	defer s.logMutex.Unlock()

	entry := LogEntry{
		Timestamp: time.Now().Local(),
		Level:     level,
		Message:   message,
	}

	// Marshal and store in fastcache with an atomic sequence key.
	if s.recentLogCache != nil {
		if b, err := json.Marshal(entry); err == nil {
			seq := atomic.AddUint64(&s.recentLogSeq, 1)
			key := fmt.Sprintf("log:%d", seq)
			s.recentLogCache.Set([]byte(key), b)

			// Maintain bounded queue of recent log seq IDs
			if s.recentLogsMaxEntries <= 0 {
				s.recentLogsMaxEntries = 100
			}
			if len(s.recentLogQueue) >= s.recentLogsMaxEntries {
				ev := s.recentLogQueue[0]
				s.recentLogQueue = s.recentLogQueue[1:]
				// delete older entry from cache (best-effort)
				s.recentLogCache.Del([]byte(fmt.Sprintf("log:%d", ev)))
			}
			s.recentLogQueue = append(s.recentLogQueue, seq)
		}
	}
}

// GetRecentLogs returns the recent log entries
func (s *CakeAutoRTTService) GetRecentLogs() []LogEntry {
	s.logMutex.RLock()
	queue := make([]uint64, len(s.recentLogQueue))
	copy(queue, s.recentLogQueue)
	s.logMutex.RUnlock()

	out := make([]LogEntry, 0, len(queue))
	if s.recentLogCache == nil {
		return out
	}

	for _, seq := range queue {
		key := fmt.Sprintf("log:%d", seq)
		v := s.recentLogCache.Get(nil, []byte(key))
		if len(v) == 0 {
			continue
		}
		var le LogEntry
		if err := json.Unmarshal(v, &le); err != nil {
			continue
		}
		out = append(out, le)
	}
	return out
}

// GetSystemStatus returns the current system status
func (s *CakeAutoRTTService) GetSystemStatus() SystemStatus {
	s.mutex.RLock()
	// Make a copy of the config to avoid returning an internal pointer that could
	// change under the caller when UpdateConfig runs. This keeps the API thread-safe.
	cfgCopy := *s.config
	running := s.running
	lastUpdate := s.lastUpdate
	lastRTT := make(map[string]int, len(s.lastRTT))
	for k, v := range s.lastRTT {
		lastRTT[k] = v
	}
	active := s.activeHosts
	s.mutex.RUnlock()

	return SystemStatus{
		Running:     running,
		LastUpdate:  lastUpdate,
		CurrentRTT:  lastRTT,
		ActiveHosts: active, // Use the properly tracked active hosts count
		DLInterface: cfgCopy.DLInterface,
		ULInterface: cfgCopy.ULInterface,
		Config:      &cfgCopy,
	}
}

// GetQdiscStats returns the current qdisc statistics
func (s *CakeAutoRTTService) GetQdiscStats() (string, error) {
	cmd := exec.Command("tc", "-s", "qdisc")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get qdisc stats: %v", err)
	}
	return string(output), nil
}

// getAdaptiveWorkers returns the current adaptive worker cap
func (s *CakeAutoRTTService) getAdaptiveWorkers() int {
	s.mutex.RLock()
	v := s.adaptiveWorkers
	s.mutex.RUnlock()
	if v < 1 {
		return 1
	}
	return v
}

// setAdaptiveWorkers sets the adaptive worker cap
func (s *CakeAutoRTTService) setAdaptiveWorkers(n int) {
	if n < 1 {
		n = 1
	}
	s.mutex.Lock()
	s.adaptiveWorkers = n
	s.mutex.Unlock()
}

// computeAdaptiveTarget computes a new worker target given current workers, configured max, and cpu usage
func (s *CakeAutoRTTService) computeAdaptiveTarget(current, cfgMax int, cpuUsage float64) int {
	target := current
	if cpuUsage > 80.0 {
		target = int(float64(current) * 0.7)
		if target < 1 {
			target = 1
		}
	} else if cpuUsage < 30.0 {
		target = int(float64(current)*1.1) + 1
		if target > cfgMax {
			target = cfgMax
		}
	}
	return target
}

// startAdaptiveController runs a background loop sampling /proc/stat and
// adjusting the adaptive worker cap based on CPU utilization. It is a
// lightweight, best-effort controller intended for OpenWrt and Linux.
func (s *CakeAutoRTTService) startAdaptiveController() {
	// sample loop using injectable cpuReader and cpuSampleInterval
	// initial sample
	prevTotal, prevIdle, err := s.cpuReader()
	if err != nil {
		return
	}

	ticker := time.NewTicker(s.cpuSampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			total, idle, err := s.cpuReader()
			if err != nil {
				continue
			}
			dTotal := float64(total - prevTotal)
			dIdle := float64(idle - prevIdle)
			prevTotal = total
			prevIdle = idle
			if dTotal <= 0 {
				continue
			}
			cpuUsage := (1.0 - dIdle/dTotal) * 100.0

			s.mutex.RLock()
			cfgMax := s.config.MaxConcurrentProbes
			s.mutex.RUnlock()

			current := s.getAdaptiveWorkers()
			target := s.computeAdaptiveTarget(current, cfgMax, cpuUsage)

			if target != current {
				s.setAdaptiveWorkers(target)
				s.AddLog("INFO", fmt.Sprintf("Adaptive controller adjusted workers: %d -> %d (cpu %.1f%%)", current, target, cpuUsage))
			}
		}
	}
}

// startCompletedPruner periodically prunes old completed probe entries
func (s *CakeAutoRTTService) startCompletedPruner() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.pruneCompletedProbes()
		}
	}
}

// pruneCompletedProbes removes entries older than retention or over max entries
func (s *CakeAutoRTTService) pruneCompletedProbes() {
	s.probeMutex.Lock()
	defer s.probeMutex.Unlock()
	if len(s.completedProbes) == 0 {
		return
	}
	// Remove entries older than retention
	cutoff := time.Now().Add(-time.Duration(s.completedRetentionSec) * time.Second)
	i := 0
	for _, cp := range s.completedProbes {
		if cp.When.After(cutoff) {
			s.completedProbes[i] = cp
			i++
		}
	}
	s.completedProbes = s.completedProbes[:i]

	// Trim by max entries if needed
	if len(s.completedProbes) > s.completedMaxEntries {
		cut := len(s.completedProbes) - s.completedMaxEntries
		s.completedProbes = s.completedProbes[cut:]
	}
}

// UpdateConfig safely updates the service configuration at runtime
func (s *CakeAutoRTTService) UpdateConfig(newCfg *Config) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.config = newCfg
	s.AddLog("INFO", fmt.Sprintf("Configuration reloaded: min_hosts=%d max_hosts=%d max_concurrent_probes=%d",
		newCfg.MinHosts, newCfg.MaxHosts, newCfg.MaxConcurrentProbes))
}

// updateInterfaceRTT updates the RTT parameter for a specific interface
func (s *CakeAutoRTTService) updateInterfaceRTT(iface string, rttUs int) error {
	cmd := exec.Command("tc", "qdisc", "change", "root", "dev", iface, "cake", "rtt", fmt.Sprintf("%dus", rttUs))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tc command failed: %w, output: %s", err, string(output))
	}
	return nil
}

// autoDetectInterfaces automatically detects CAKE-enabled interfaces
func (s *CakeAutoRTTService) autoDetectInterfaces() error {
	if s.config.DLInterface != "" && s.config.ULInterface != "" {
		return nil // Both interfaces already specified
	}

	s.AddLog("DEBUG", "Auto-detecting CAKE interfaces")

	// Find interfaces with CAKE qdisc
	cmd := exec.Command("tc", "qdisc", "show")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to run tc qdisc show: %w", err)
	}

	var cakeInterfaces []string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "qdisc cake") {
			parts := strings.Fields(line)
			if len(parts) >= 5 {
				cakeInterfaces = append(cakeInterfaces, parts[4])
			}
		}
	}

	if len(cakeInterfaces) == 0 {
		return fmt.Errorf("no CAKE interfaces found")
	}

	// Auto-detect download interface (prefer ifb-* interfaces)
	if s.config.DLInterface == "" {
		for _, iface := range cakeInterfaces {
			if strings.HasPrefix(iface, "ifb") {
				s.config.DLInterface = iface
				break
			}
		}
		if s.config.DLInterface == "" && len(cakeInterfaces) > 0 {
			s.config.DLInterface = cakeInterfaces[0]
		}
	}

	// Auto-detect upload interface (prefer non-ifb interfaces)
	if s.config.ULInterface == "" {
		for _, iface := range cakeInterfaces {
			if !strings.HasPrefix(iface, "ifb") {
				s.config.ULInterface = iface
				break
			}
		}
		if s.config.ULInterface == "" && len(cakeInterfaces) > 0 {
			// Use the last interface if no non-ifb interface found
			s.config.ULInterface = cakeInterfaces[len(cakeInterfaces)-1]
		}
	}

	return nil
}
