package main

import (
	"fmt"
	"io"

	"github.com/tinyrange/tinyrange/experimental/crumblecracker/kvm"
	"github.com/tinyrange/tinyrange/pkg/log"
)

// Based on: https://github.com/copy/v86/blob/master/src/uart.js
const (
	serialDLAB = 0x80

	serialIER_MSI  = 0x8
	serialIER_THRI = 0x2
	serialIER_RDI  = 0x1

	serialIIR_MSI    = 0x0
	serialIIR_NO_INT = 0x1
	serialIIR_THRI   = 0x2
	serialIIR_RDI    = 0x4
	serialIIR_RLSI   = 0x6
	serialIIR_CTI    = 0xc

	serialLSR_DR   = 0x1
	serialLSR_THRE = 0x20
	serialLSR_TEMT = 0x40
)

type SerialDevice struct {
	Base   uint16
	Writer io.Writer
	IRQ    byte
	Input  chan byte

	vm VMDevice

	baudRate     uint16
	ier          byte
	iir          byte
	modemStatus  byte
	modemControl byte
	fifoControl  byte
	lineControl  byte
	scratch      byte
	lsr          byte

	ints uint32
}

func (s *SerialDevice) Init(dev VMDevice) error {
	s.Input = make(chan byte, 1024)

	s.vm = dev

	s.lsr = serialLSR_TEMT | serialLSR_THRE
	s.iir = serialIIR_NO_INT

	return nil
}

func (s *SerialDevice) Write(data []byte) (int, error) {
	for _, b := range data {
		s.Input <- b
	}

	s.lsr |= serialLSR_DR

	if s.fifoControl&1 != 0 {
		s.throwInterrupt(serialIIR_CTI)
	} else {
		s.throwInterrupt(serialIIR_RDI)
	}

	return len(data), nil
}

func (s *SerialDevice) Ports() []uint16 {
	return []uint16{
		s.Base,
		s.Base + 1,
		s.Base + 2,
		s.Base + 3,
		s.Base + 4,
		s.Base + 5,
		s.Base + 6,
		s.Base + 7,
	}
}

func (s *SerialDevice) IO(io *kvm.KVMIoEvent) error {
	port := io.Port - s.Base
	switch port {
	case 0:
		switch io.Direction {
		case kvm.IoDirectionRead: // serial read
			if s.lineControl&serialDLAB != 0 {
				io.Write([]byte{byte(s.baudRate & 0xFF)})
				return nil
			} else {
				var data byte = 0

				select {
				case data = <-s.Input:
				default:
					s.lsr &^= serialLSR_DR
					if err := s.clearInterrupt(serialIIR_CTI); err != nil {
						return fmt.Errorf("failed to clear interrupt: %w", err)
					}
					if err := s.clearInterrupt(serialIIR_RDI); err != nil {
						return fmt.Errorf("failed to clear interrupt: %w", err)
					}
				}

				io.Write([]byte{byte(data)})

				return nil
			}
		case kvm.IoDirectionWrite: // serial write
			if s.lineControl&serialDLAB != 0 {
				s.baudRate = s.baudRate&0xFF00 | uint16(io.Read()[0])
				return nil
			}

			data := io.Read()

			if _, err := s.Writer.Write(data); err != nil {
				return fmt.Errorf("failed to write to stdout: %w", err)
			}

			if err := s.throwInterrupt(serialIIR_THRI); err != nil {
				return fmt.Errorf("failed to throw interrupt: %w", err)
			}

			return nil
		}
	case 1:
		switch io.Direction {
		case kvm.IoDirectionRead: // serial interrupt read
			if s.lineControl&serialDLAB != 0 {
				io.Write([]byte{byte(s.baudRate >> 8)})
			} else {
				io.Write([]byte{byte(s.ier & 0xf)})
			}

			return nil
		case kvm.IoDirectionWrite: // serial interrupt write
			b := io.Read()[0]

			if s.lineControl&serialDLAB != 0 {
				s.baudRate = s.baudRate&0xFF | uint16(b)<<8
				// dbg_log("baud rate: "+h(this.baud_rate), LOG_SERIAL)
			} else {
				if (s.ier&serialIIR_THRI == 0) && (b&serialIIR_THRI != 0) {
					// re-throw THRI if it was masked
					if err := s.throwInterrupt(serialIIR_THRI); err != nil {
						return fmt.Errorf("failed to throw interrupt: %w", err)
					}
				}

				s.ier = b & 0xF
				// dbg_log("interrupt enable: "+h(out_byte), LOG_SERIAL)
				if err := s.checkInterrupt(); err != nil {
					return fmt.Errorf("failed to check interrupt: %w", err)
				}
			}

			return nil
		}
	case 2:
		switch io.Direction {
		case kvm.IoDirectionRead:
			ret := s.iir & 0xf

			if s.iir == serialIIR_THRI {
				if err := s.clearInterrupt(serialIIR_THRI); err != nil {
					return fmt.Errorf("failed to clear interrupt: %w", err)
				}
			}

			if s.fifoControl&1 != 0 {
				ret |= 0xc0
			}

			io.Write([]byte{ret})

			return nil
		case kvm.IoDirectionWrite:
			data := io.Read()
			s.fifoControl = data[0]
			return nil
		}
	case 3:
		switch io.Direction {
		case kvm.IoDirectionRead:
			io.Write([]byte{s.lineControl})
			return nil
		case kvm.IoDirectionWrite:
			data := io.Read()
			s.lineControl = data[0]
			return nil
		}
	case 4:
		switch io.Direction {
		case kvm.IoDirectionRead:
			io.Write([]byte{s.modemControl})
			return nil
		case kvm.IoDirectionWrite:
			data := io.Read()
			s.modemControl = data[0]
			return nil
		}
	case 5:
		switch io.Direction {
		case kvm.IoDirectionRead:
			io.Write([]byte{s.lsr})
			return nil
		}
	case 6:
		switch io.Direction {
		case kvm.IoDirectionRead:
			s.modemStatus &= 0xf0
			io.Write([]byte{s.modemStatus})
			return nil
		case kvm.IoDirectionWrite:
			data := io.Read()
			s.setModemStatus(data[0])
			return nil
		}
	case 7:
		switch io.Direction {
		case kvm.IoDirectionRead:
			io.Write([]byte{s.scratch})
			return nil
		case kvm.IoDirectionWrite:
			data := io.Read()
			s.scratch = data[0]
			return nil
		}
	}

	log.Info("unknown serial io", "port", fmt.Sprintf("0x%x", io.Port-s.Base), "direction", io.Direction, "size", io.Size)
	return nil
}

func (s *SerialDevice) setModemStatus(status byte) {
	prev_delta_bits := s.modemStatus & 0x0F
	// compare the bits that have changed and shift them into the delta bits
	delta := (s.modemStatus ^ status) >> 4
	// The delta should stay set if they were previously set
	delta |= prev_delta_bits

	// update the current modem status
	s.modemStatus = status
	// update the delta bits based on the changes and previous
	// values, but also leave the delta bits set if they were
	// passed in as part of the status
	s.modemStatus |= delta
}

func (s *SerialDevice) checkInterrupt() error {
	if (s.lsr&serialLSR_DR != 0) && (s.ier&serialIER_RDI != 0) {
		s.iir = serialIIR_RDI
	} else if (s.lsr&serialLSR_THRE != 0) && (s.ier&serialIER_THRI != 0) {
		s.iir = serialIIR_THRI
	} else {
		s.iir = serialIIR_NO_INT
	}
	if s.iir != serialIIR_NO_INT {
		return s.vm.RaiseIrq(s.IRQ)
	} else {
		return s.vm.LowerIrq(s.IRQ)
	}
}

func (s *SerialDevice) throwInterrupt(interrupt byte) error {
	s.ints |= (1 << interrupt)
	return s.checkInterrupt()
}

func (s *SerialDevice) clearInterrupt(interrupt byte) error {
	s.ints &^= (1 << interrupt)
	return s.checkInterrupt()
}

var (
	_ IODevice = (*SerialDevice)(nil)
)
