package sftp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"golang.org/x/crypto/ssh"
)

type SSHFSInternalServer struct {
	*SSHFSServer
	Addr string
}

func (s *SSHFSInternalServer) handleConnection(sshConn *ssh.ServerConn, channels <-chan ssh.NewChannel) {
	slog.Debug("sftp: got connection", "remote", sshConn.RemoteAddr().String())

	for newChannel := range channels {
		if t := newChannel.ChannelType(); t != "session" {
			newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
			return
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			slog.Warn("ssh: could not accept channel", "error", err)
			return
		}

		for req := range requests {
			switch {
			case req.Type == "subsystem" && string(req.Payload[4:]) == "sftp":
				go func() {
					defer channel.Close() // SSH_MSG_CHANNEL_CLOSE
					err := s.ServeSftp(channel)
					if err != nil {
						slog.Warn("failed to serve sftp", "error", err)
						return
					}
				}()
				req.Reply(true, nil)
			default:
				slog.Debug("ssh: unknown request", "type", req.Type, "reply", req.WantReply, "data", req.Payload)
			}
		}
	}
}

func (s *SSHFSInternalServer) Run(listen func(network, addr string) (net.Listener, error)) error {
	listener, err := listen("tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("ssh: failed to listen for connection: %v", err)
	}

	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			// Accept any connection since we use the VM IP Address.
			return nil, nil
		},
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("ssh: failed to generate key: %v", err)
	}

	hostSigner, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return fmt.Errorf("ssh: failed to make signer: %v", err)
	}

	config.AddHostKey(hostSigner)

	go func() {
		for {
			nConn, err := listener.Accept()
			if err != nil {
				slog.Debug("ssh: failed to accept", "error", err)
				return
			}

			slog.Debug("got connection", "addr", nConn.RemoteAddr())

			sshConn, chans, reqs, err := ssh.NewServerConn(nConn, config)
			if err != nil {
				slog.Debug("ssh: failed to make connection", "error", err)
				continue
			}

			// Discard all global out-of-band Requests
			go ssh.DiscardRequests(reqs)

			go s.handleConnection(sshConn, chans)
		}
	}()

	return nil
}

func NewInternalServer(fs filesystem.Directory, addr string) *SSHFSInternalServer {
	return &SSHFSInternalServer{SSHFSServer: New(fs), Addr: addr}
}
