package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"github.com/tinyrange/tinyrange/pkg/build2"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem/dbconfig"
	"github.com/tinyrange/tinyrange/pkg/log"
	"github.com/tinyrange/tinyrange/pkg/netstack/ns"
	"github.com/tinyrange/tinyrange/pkg/path"
	"github.com/tinyrange/tinyrange/pkg/vmm"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"
)

func getDriverPath(driverPath string) (string, error) {
	if driverPath == "" {
		return "", fmt.Errorf("driver path required")
	}

	if path.Unix.IsAbs(driverPath) && runtime.GOOS == "windows" {
		// windows paths are in the format /letter/path

		// get the drive letter
		driveLetter := driverPath[1:2]
		// convert to windows path
		driverPath = fmt.Sprintf("%s:\\%s", driveLetter, strings.ReplaceAll(driverPath[3:], "/", "\\"))
	}

	return driverPath, nil
}

type sshProxyMonitor struct {
	mtx           sync.Mutex
	driver        vmm.ProxyDriver
	client        *ssh.Client
	session       *ssh.Session
	driverPath    string
	serverBaseUrl string
	dialer        proxy.ContextDialer
}

func (s *sshProxyMonitor) generateDatabaseConfig(configs []dbconfig.BuildDatabaseConfig) ([]dbconfig.BuildDatabaseConfig, error) {
	if len(configs) != 1 {
		return nil, fmt.Errorf("multiple database configs not supported")
	}

	return []dbconfig.BuildDatabaseConfig{
		{
			DefaultBuildDirectory: &dbconfig.DefaultBuildDirectory{},
		},
		{
			RemoteBuildDirectory: &dbconfig.RemoteBuildDirectory{
				BaseURL: s.serverBaseUrl + "/db",
			},
		},
	}, nil
}

func (s *sshProxyMonitor) generateConfig() ([]config.TinyRangeConfig, error) {
	configs := s.driver.Configs()

	if len(configs) != 1 {
		return nil, fmt.Errorf("multiple configs not supported")
	}

	firstConfig := configs[0]

	var ret config.TinyRangeConfig

	ret.Version = firstConfig.Version

	databaseConfigs, err := s.generateDatabaseConfig(firstConfig.BuildDatabaseConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to generate database config: %w", err)
	}
	ret.BuildDatabaseConfig = databaseConfigs

	ret.Architecture = firstConfig.Architecture
	ret.RootArchitecture = firstConfig.RootArchitecture
	ret.Kernel = firstConfig.Kernel
	ret.InitFilesystem = firstConfig.InitFilesystem

	ret.Filesystems = firstConfig.Filesystems

	ret.Interaction = config.InteractionRemote
	ret.CPUCores = firstConfig.CPUCores
	ret.MemoryMB = firstConfig.MemoryMB
	ret.AutoScale = firstConfig.AutoScale
	// Don't set debug

	return []config.TinyRangeConfig{ret}, nil
}

// DialInternalContext implements ns.NetStack.
func (s *sshProxyMonitor) DialInternalContext(ctx context.Context, network string, address string) (net.Conn, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.dialer == nil {
		if deadline, ok := ctx.Deadline(); ok {
			// sleep until the deadline
			time.Sleep(time.Until(deadline))
		}

		return nil, context.DeadlineExceeded
	}

	conn, err := s.dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w", address, err)
	}

	return conn, nil
}

// ListenInternal implements ns.NetStack.
func (s *sshProxyMonitor) ListenInternal(network string, address string) (net.Listener, error) {
	return nil, fmt.Errorf("ListenInternal not implemented")
}

// ListenPacketInternal implements ns.NetStack.
func (s *sshProxyMonitor) ListenPacketInternal(network string, address string) (net.PacketConn, error) {
	return nil, fmt.Errorf("ListenPacketInternal not implemented")
}

// NetStack implements vmm.ProxyMonitor.
func (s *sshProxyMonitor) NetStack() ns.NetStack {
	return s
}

// Run implements vmm.ProxyMonitor.
func (s *sshProxyMonitor) Run(bindOutput bool) error {
	if bindOutput {
		s.session.Stdout = os.Stdout
		s.session.Stderr = os.Stderr
	}

	configUuid := uuid.New()

	serveMux := http.NewServeMux()

	serveMux.HandleFunc(fmt.Sprintf("POST /%s", configUuid.String()), func(w http.ResponseWriter, r *http.Request) {
		s.mtx.Lock()
		defer s.mtx.Unlock()

		log.Debug("received request for config", "url", r.URL.String(), "ptr", unsafe.Pointer(s))

		var req config.ProxyLoadRequest

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Error("failed to decode request", "error", err)
			http.Error(w, fmt.Sprintf("failed to decode request: %s", err), http.StatusBadRequest)
			return
		}

		socksDialer, err := proxy.SOCKS5("tcp", req.Socks5Address, nil, s.client)
		if err != nil {
			log.Error("failed to create socks5 dialer", "error", err)
			http.Error(w, fmt.Sprintf("failed to create socks5 dialer: %s", err), http.StatusInternalServerError)
			return
		}

		contextDialer, ok := socksDialer.(proxy.ContextDialer)
		if !ok {
			log.Error("failed to cast socks5 dialer to context dialer")
			http.Error(w, "failed to cast socks5 dialer to context dialer", http.StatusInternalServerError)
			return
		}

		s.dialer = contextDialer
		if s.dialer == nil {
			log.Error("failed to create dialer")
			http.Error(w, "failed to create dialer", http.StatusInternalServerError)
			return
		}

		configs, err := s.generateConfig()
		if err != nil {
			log.Error("failed to generate config", "error", err)
			http.Error(w, fmt.Sprintf("failed to generate config: %s", err), http.StatusInternalServerError)
			return
		}

		if err := json.NewEncoder(w).Encode(configs); err != nil {
			log.Error("failed to encode config", "error", err)
			http.Error(w, fmt.Sprintf("failed to encode config: %s", err), http.StatusInternalServerError)
			return
		}
	})

	build2.RegisterBuildDirectoryHandler(serveMux, s.driver.BuildDatabase(), "/db")

	listen, err := s.client.ListenTCP(&net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 0,
	})
	if err != nil {
		return fmt.Errorf("failed to listen on ssh: %w", err)
	}

	go func() {
		if err := http.Serve(listen, serveMux); err != nil {
			log.Error("failed to serve web server", "error", err)
		}
	}()

	s.serverBaseUrl = fmt.Sprintf("http://%s", listen.Addr().String())

	command := fmt.Sprintf("%s -proxy %s", s.driverPath, fmt.Sprintf("http://%s/%s", listen.Addr().String(), configUuid.String()))

	// start the session
	if err := s.session.Start(command); err != nil {
		return fmt.Errorf("failed to start ssh session: %w", err)
	}

	// wait for the session to finish
	if err := s.session.Wait(); err != nil {
		return fmt.Errorf("failed to wait for ssh session: %w", err)
	}

	return nil
}

// Shutdown implements vmm.ProxyMonitor.
func (s *sshProxyMonitor) Shutdown() error {
	if err := s.session.Signal(ssh.SIGKILL); err != nil {
		log.Error("failed to send shutdown signal", "error", err)
		return fmt.Errorf("failed to send shutdown signal: %w", err)
	}

	if err := s.session.Close(); err != nil {
		log.Error("failed to close ssh session", "error", err)
		return fmt.Errorf("failed to close ssh session: %w", err)
	}

	if err := s.client.Close(); err != nil {
		log.Error("failed to close ssh client", "error", err)
		return fmt.Errorf("failed to close ssh client: %w", err)
	}

	log.Debug("ssh client closed")

	return nil
}

var (
	_ vmm.ProxyMonitor = &sshProxyMonitor{}
)

func main() {
	vmm.ProxyEntry(
		func(driver vmm.ProxyDriver) (vmm.PrepareResult, error) {
			return vmm.PrepareResult{}, nil
		},
		func(driver vmm.ProxyDriver) (vmm.ProxyMonitor, error) {
			url := driver.URL()
			if url.Scheme != "ssh" {
				return nil, fmt.Errorf("ssh scheme required, got %s", url.Scheme)
			}

			username := url.User.Username()
			if username == "" {
				user, err := user.Current()
				if err != nil {
					return nil, fmt.Errorf("failed to get current user: %w", err)
				}

				username = user.Username
			}
			host := url.Hostname()
			if host == "" {
				return nil, fmt.Errorf("ssh host required")
			}
			port := url.Port()
			if port == "" {
				port = "22"
			}
			driverPath, err := getDriverPath(url.Path)
			if err != nil {
				return nil, fmt.Errorf("failed to get driver path: %w", err)
			}

			config := ssh.ClientConfig{
				User: username,
			}

			if password, ok := url.User.Password(); ok {
				config.Auth = []ssh.AuthMethod{
					ssh.Password(password),
				}
			}

			homeDir, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to get home directory: %w", err)
			}

			var signers []ssh.Signer

			for _, key := range []string{
				"id_rsa",
				"id_dsa",
				"id_ecdsa",
				"id_ed25519",
			} {
				keyPath := path.Native.Join(homeDir, ".ssh", key)
				if _, err := os.Stat(keyPath); err != nil {
					continue
				}

				keyFile, err := os.Open(keyPath)
				if err != nil {
					return nil, fmt.Errorf("failed to open key file %s: %w", keyPath, err)
				}
				defer keyFile.Close()

				keyBytes, err := io.ReadAll(keyFile)
				if err != nil {
					return nil, fmt.Errorf("failed to read key file %s: %w", keyPath, err)
				}

				signer, err := ssh.ParsePrivateKey(keyBytes)
				if err != nil {
					return nil, fmt.Errorf("failed to parse private key %s: %w", keyPath, err)
				}

				signers = append(signers, signer)
			}

			if len(signers) > 0 {
				config.Auth = append(config.Auth, ssh.PublicKeys(signers...))
			}

			hostKeys := make(map[string]ssh.PublicKey)

			// parse known_hosts
			knownHostsPath := path.Native.Join(homeDir, ".ssh", "known_hosts")
			if _, err := os.Stat(knownHostsPath); err == nil {
				knownHosts, err := os.ReadFile(knownHostsPath)
				if err != nil {
					return nil, fmt.Errorf("failed to read known hosts file %s: %w", knownHostsPath, err)
				}
				for len(knownHosts) > 0 && knownHosts[len(knownHosts)-1] == '\n' {
					_, hosts, pubKey, _, rest, err := ssh.ParseKnownHosts(knownHosts)
					if err != nil {
						return nil, fmt.Errorf("failed to parse known hosts file %s: %w", knownHostsPath, err)
					}

					for _, host := range hosts {
						// if the host doesn't have a port specified, use the default port
						if !strings.Contains(host, ":") {
							host = net.JoinHostPort(host, "22")
						}

						hostKeys[host] = pubKey
					}

					knownHosts = rest
				}
			}

			config.HostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
				if _, ok := hostKeys[hostname]; !ok {
					return fmt.Errorf("unknown host %s", hostname)
				}

				if ssh.FingerprintSHA256(key) != ssh.FingerprintSHA256(hostKeys[hostname]) {
					return fmt.Errorf("host key mismatch for %s", hostname)
				}

				return nil
			}

			client, err := ssh.Dial("tcp", net.JoinHostPort(host, port), &config)
			if err != nil {
				return nil, fmt.Errorf("failed to dial ssh: %w", err)
			}

			session, err := client.NewSession()
			if err != nil {
				return nil, fmt.Errorf("failed to create ssh session: %w", err)
			}

			monitor := &sshProxyMonitor{
				driver:     driver,
				client:     client,
				session:    session,
				driverPath: driverPath,
			}

			return monitor, nil
		},
	)
}
