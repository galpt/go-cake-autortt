package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	Version           = "2.0.0"
	DefaultConfigPath = "/etc/config/cake-autortt"
)

// Config represents the application configuration
type Config struct {
	RTTUpdateInterval   int    `mapstructure:"rtt_update_interval" yaml:"rtt_update_interval"`
	MinHosts            int    `mapstructure:"min_hosts" yaml:"min_hosts"`
	MaxHosts            int    `mapstructure:"max_hosts" yaml:"max_hosts"`
	RTTMarginPercent    int    `mapstructure:"rtt_margin_percent" yaml:"rtt_margin_percent"`
	DefaultRTTMs        int    `mapstructure:"default_rtt_ms" yaml:"default_rtt_ms"`
	DLInterface         string `mapstructure:"dl_interface" yaml:"dl_interface"`
	ULInterface         string `mapstructure:"ul_interface" yaml:"ul_interface"`
	Debug               bool   `mapstructure:"debug" yaml:"debug"`
	TCPConnectTimeout   int    `mapstructure:"tcp_connect_timeout" yaml:"tcp_connect_timeout"`
	MaxConcurrentProbes int    `mapstructure:"max_concurrent_probes" yaml:"max_concurrent_probes"`
	WebEnabled          bool   `mapstructure:"web_enabled" yaml:"web_enabled"`
	WebPort             int    `mapstructure:"web_port" yaml:"web_port"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		RTTUpdateInterval:   5,
		MinHosts:            3,
		MaxHosts:            100,
		RTTMarginPercent:    10,
		DefaultRTTMs:        100,
		DLInterface:         "",
		ULInterface:         "",
		Debug:               false,
		TCPConnectTimeout:   3,
		MaxConcurrentProbes: 50,
		WebEnabled:          true,
		WebPort:             80,
	}
}

var (
	cfg     *Config
	rootCmd = &cobra.Command{
		Use:   "cake-autortt",
		Short: "Automatically adjust CAKE qdisc RTT parameter",
		Long: `cake-autortt automatically monitors active network connections and adjusts
the RTT parameter of CAKE qdisc on both ingress and egress interfaces for optimal performance.

This Go version uses TCP-based RTT measurement for more reliable results and supports
parallel processing for fast measurement of multiple hosts.`,
		Version: Version,
		Run:     runMain,
	}
)

func init() {
	cfg = DefaultConfig()

	// Command line flags
	rootCmd.Flags().IntVar(&cfg.RTTUpdateInterval, "rtt-update-interval", cfg.RTTUpdateInterval, "Interval between qdisc RTT updates (seconds)")
	rootCmd.Flags().IntVar(&cfg.MinHosts, "min-hosts", cfg.MinHosts, "Minimum hosts needed for RTT calculation")
	rootCmd.Flags().IntVar(&cfg.MaxHosts, "max-hosts", cfg.MaxHosts, "Maximum hosts to probe simultaneously")
	rootCmd.Flags().IntVar(&cfg.RTTMarginPercent, "rtt-margin-percent", cfg.RTTMarginPercent, "Percentage margin added to measured RTT")
	rootCmd.Flags().IntVar(&cfg.DefaultRTTMs, "default-rtt-ms", cfg.DefaultRTTMs, "Default RTT when no hosts available (milliseconds)")
	rootCmd.Flags().StringVar(&cfg.DLInterface, "dl-interface", cfg.DLInterface, "Download interface (auto-detected if not specified)")
	rootCmd.Flags().StringVar(&cfg.ULInterface, "ul-interface", cfg.ULInterface, "Upload interface (auto-detected if not specified)")
	rootCmd.Flags().BoolVar(&cfg.Debug, "debug", cfg.Debug, "Enable debug logging")
	rootCmd.Flags().IntVar(&cfg.TCPConnectTimeout, "tcp-timeout", cfg.TCPConnectTimeout, "TCP connection timeout for RTT measurement (seconds)")
	rootCmd.Flags().IntVar(&cfg.MaxConcurrentProbes, "max-concurrent", cfg.MaxConcurrentProbes, "Maximum concurrent TCP probes")

	// Add web server flags
	rootCmd.Flags().BoolVar(&cfg.WebEnabled, "web-enabled", cfg.WebEnabled, "Enable web interface")
	rootCmd.Flags().IntVar(&cfg.WebPort, "web-port", cfg.WebPort, "Web interface port")

	// Bind flags to viper
	viper.BindPFlags(rootCmd.Flags())
}

func loadConfig() error {
	// Set config file path
	viper.SetConfigName("cake-autortt")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("/etc/config/")
	viper.AddConfigPath("/etc/")
	viper.AddConfigPath(".")

	// Set environment variable prefix
	viper.SetEnvPrefix("CAKE_AUTORTT")
	viper.AutomaticEnv()

	// Read config file if it exists
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found, use defaults
		logMessage("WARN", "Config file not found, using defaults")
	}

	// Unmarshal config
	if err := viper.Unmarshal(cfg); err != nil {
		return fmt.Errorf("error unmarshaling config: %w", err)
	}

	return nil
}

func logMessage(level, message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	// Skip DEBUG messages when debug is disabled
	if level == "DEBUG" && !cfg.Debug {
		return
	}

	// Always log to stdout for now (can be extended to syslog later)
	if cfg.Debug || level == "INFO" || level == "ERROR" || level == "WARN" {
		fmt.Printf("[%s] cake-autortt %s: %s\n", timestamp, level, message)
	}
}

func runMain(cmd *cobra.Command, args []string) {
	// Load configuration
	if err := loadConfig(); err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	logMessage("INFO", fmt.Sprintf("Starting cake-autortt v%s", Version))
	logMessage("INFO", fmt.Sprintf("Config: rtt_update_interval=%ds, min_hosts=%d, max_hosts=%d",
		cfg.RTTUpdateInterval, cfg.MinHosts, cfg.MaxHosts))
	logMessage("INFO", fmt.Sprintf("Config: rtt_margin=%d%%, default_rtt=%dms, tcp_timeout=%ds",
		cfg.RTTMarginPercent, cfg.DefaultRTTMs, cfg.TCPConnectTimeout))

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize the cake autortt service
	service, err := NewCakeAutoRTTService(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize service: %v", err)
	}

	// Initialize web server if enabled
	var webServer *WebServer
	if cfg.WebEnabled {
		webServer = NewWebServer(service, cfg)
		// Start web server in a separate goroutine
		go func() {
			if err := webServer.Start(); err != nil {
				logMessage("ERROR", fmt.Sprintf("Web server error: %v", err))
			}
		}()
		logMessage("INFO", fmt.Sprintf("Web interface available at http://localhost:%d/cake-autortt", cfg.WebPort))
	}

	// Start the service in a goroutine
	go func() {
		if err := service.Run(ctx); err != nil {
			logMessage("ERROR", fmt.Sprintf("Service error: %v", err))
			cancel()
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	logMessage("INFO", "Shutting down cake-autortt")
	cancel()

	// Give the service time to clean up
	time.Sleep(1 * time.Second)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
