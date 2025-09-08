package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
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
	recentLogs  []LogEntry
	logMutex    sync.RWMutex
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

// NewCakeAutoRTTService creates a new service instance
func NewCakeAutoRTTService(config *Config) (*CakeAutoRTTService, error) {
	ctx, cancel := context.WithCancel(context.Background())
	service := &CakeAutoRTTService{
		config:     config,
		running:    false,
		lastRTT:    make(map[string]int),
		lastUpdate: time.Now(),
		ctx:        ctx,
		cancel:     cancel,
		recentLogs: make([]LogEntry, 0, 100),
	}

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
			s.lastUpdate = time.Now()
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

	// Convert to slice and limit to max hosts
	hosts := make([]string, 0, len(hostSet))
	for host := range hostSet {
		hosts = append(hosts, host)
		if len(hosts) >= s.config.MaxHosts {
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

	// Create a semaphore to limit concurrent connections
	sem := make(chan struct{}, s.config.MaxConcurrentProbes)
	results := make(chan RTTMeasurement, len(hosts))
	var wg sync.WaitGroup

	// Start measurements for all hosts
	for _, host := range hosts {
		wg.Add(1)
		go func(h string) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			rtt, err := s.measureSingleHostTCP(h)
			results <- RTTMeasurement{Host: h, RTT: rtt, Err: err}
		}(host)
	}

	// Close results channel when all goroutines complete
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

// measureSingleHostTCP measures RTT to a single host using TCP connection
func (s *CakeAutoRTTService) measureSingleHostTCP(host string) (time.Duration, error) {
	// Try common ports in order of preference
	ports := []string{"80", "443", "22", "21", "25", "53"}

	timeout := time.Duration(s.config.TCPConnectTimeout) * time.Second

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
	// Add margin to measured RTT
	adjustedRTT := targetRTTMs * (1.0 + float64(s.config.RTTMarginPercent)/100.0)

	// Convert to microseconds for tc command
	rttUs := int(adjustedRTT * 1000)

	s.AddLog("INFO", fmt.Sprintf("Adjusting CAKE RTT to %.2fms (%dus)", adjustedRTT, rttUs))

	// Update RTT tracking with final adjusted value
	s.mutex.Lock()
	s.lastRTT["final"] = int(adjustedRTT)
	s.mutex.Unlock()

	// Update download interface
	if s.config.DLInterface != "" {
		if err := s.updateInterfaceRTT(s.config.DLInterface, rttUs); err != nil {
			s.AddLog("ERROR", fmt.Sprintf("Failed to update RTT on download interface %s: %v",
				s.config.DLInterface, err))
		} else {
			s.AddLog("DEBUG", fmt.Sprintf("Updated RTT on download interface %s", s.config.DLInterface))
		}
	}

	// Update upload interface
	if s.config.ULInterface != "" {
		if err := s.updateInterfaceRTT(s.config.ULInterface, rttUs); err != nil {
			s.AddLog("ERROR", fmt.Sprintf("Failed to update RTT on upload interface %s: %v",
				s.config.ULInterface, err))
		} else {
			s.AddLog("DEBUG", fmt.Sprintf("Updated RTT on upload interface %s", s.config.ULInterface))
		}
	}

	return nil
}

// Stop stops the service
func (s *CakeAutoRTTService) Stop() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.running = false
	s.cancel()
}

// AddLog adds a log entry to the recent logs
func (s *CakeAutoRTTService) AddLog(level, message string) {
	s.logMutex.Lock()
	defer s.logMutex.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
	}

	s.recentLogs = append(s.recentLogs, entry)

	// Keep only the last 100 log entries
	if len(s.recentLogs) > 100 {
		s.recentLogs = s.recentLogs[1:]
	}
}

// GetRecentLogs returns the recent log entries
func (s *CakeAutoRTTService) GetRecentLogs() []LogEntry {
	s.logMutex.RLock()
	defer s.logMutex.RUnlock()

	// Return a copy of the logs
	logs := make([]LogEntry, len(s.recentLogs))
	copy(logs, s.recentLogs)
	return logs
}

// GetSystemStatus returns the current system status
func (s *CakeAutoRTTService) GetSystemStatus() SystemStatus {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return SystemStatus{
		Running:     s.running,
		LastUpdate:  s.lastUpdate,
		CurrentRTT:  s.lastRTT,
		ActiveHosts: s.activeHosts, // Use the properly tracked active hosts count
		DLInterface: s.config.DLInterface,
		ULInterface: s.config.ULInterface,
		Config:      s.config,
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
