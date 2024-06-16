package main

import (
	"fmt"
	"log/slog"

	"github.com/miekg/dns"
)

type dnsServer struct {
	server    *dns.Server
	dnsLookup func(name string) (string, error)
}

func (s *dnsServer) parseQuery(r *dns.Msg, m *dns.Msg) {
	for _, q := range m.Question {
		switch q.Qtype {
		case dns.TypeA:
			ip, err := s.dnsLookup(q.Name)
			if err != nil {
				slog.Error("error resolving dns", "name", q.Name, "err", err)
				m.SetRcode(r, dns.RcodeServerFailure)
				return
			}

			if ip != "" {
				rr, err := dns.NewRR(fmt.Sprintf("%s A %s", q.Name, ip))
				if err == nil {
					m.Answer = append(m.Answer, rr)
				}
			} else {
				slog.Error("DNS Query for unknown name", "name", q.Name)
				m.SetRcode(r, dns.RcodeNameError)
				return
			}
		}
	}
}

func (s *dnsServer) handleDnsRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Compress = false
	m.Authoritative = true

	switch r.Opcode {
	case dns.OpcodeQuery:
		s.parseQuery(r, m)
	}

	// log.Printf("Dns Response: %v", m)

	_ = w.WriteMsg(m)
}
