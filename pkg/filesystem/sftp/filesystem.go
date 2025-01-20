package sftp

import (
	"fmt"
)

type sftpContext interface{}

// https://datatracker.ietf.org/doc/html/draft-ietf-secsh-filexfer-02
type sftpFilesystem interface {
	// 4
	PktInit(ctx sftpContext, pkt *pktInit) (ResponsePacket, error)

	// 6.3
	PktOpen(ctx sftpContext, pkt *pktOpen) (ResponsePacket, error)
	PktClose(ctx sftpContext, pkt *pktClose) (ResponsePacket, error)

	// 6.4
	PktRead(ctx sftpContext, pkt *pktRead) (ResponsePacket, error)
	PktWrite(ctx sftpContext, pkt *pktWrite) (ResponsePacket, error)

	// 6.5
	PktRemove(ctx sftpContext, pkt *pktRemove) (ResponsePacket, error) // returns SSH_FXP_STATUS
	PktRename(ctx sftpContext, pkt *pktRename) (ResponsePacket, error) // returns SSH_FXP_STATUS

	// 6.6
	PktMkdir(ctx sftpContext, pkt *pktMkdir) (ResponsePacket, error) // Returns SSH_FXP_STATUS
	PktRmdir(ctx sftpContext, pkt *pktRmdir) (ResponsePacket, error) // Returns SSH_FXP_STATUS

	// 6.7
	PktOpenDir(ctx sftpContext, pkt *pktOpenDir) (ResponsePacket, error)
	PktReadDir(ctx sftpContext, pkt *pktReadDir) (ResponsePacket, error)

	// 6.8
	PktStat(ctx sftpContext, pkt *pktStat) (ResponsePacket, error)
	PktLstat(ctx sftpContext, pkt *pktLstat) (ResponsePacket, error)
	PktFstat(ctx sftpContext, pkt *pktFstat) (ResponsePacket, error)

	// 6.9
	PktSetStat(ctx sftpContext, pkt *pktSetStat) (ResponsePacket, error)
	PktFSetStat(ctx sftpContext, pkt *pktFSetStat) (ResponsePacket, error)

	// 6.10
	PktReadlink(ctx sftpContext, pkt *pktReadlink) (ResponsePacket, error)
	PktSymlink(ctx sftpContext, pkt *pktSymlink) (ResponsePacket, error)

	// 6.11
	PktRealPath(ctx sftpContext, pkt *pktRealPath) (ResponsePacket, error)
}

func handlePacket(fs sftpFilesystem, ctx sftpContext, pkt any) (ResponsePacket, error) {
	switch pkt := pkt.(type) {
	// 4
	case *pktInit:
		return fs.PktInit(ctx, pkt)

	// 6.3
	case *pktOpen:
		return fs.PktOpen(ctx, pkt)
	case *pktClose:
		return fs.PktClose(ctx, pkt)

	// 6.4
	case *pktRead:
		return fs.PktRead(ctx, pkt)
	case *pktWrite:
		return fs.PktWrite(ctx, pkt)

	// 6.5
	case *pktRemove:
		return fs.PktRemove(ctx, pkt)
	case *pktRename:
		return fs.PktRename(ctx, pkt)

	// 6.6
	case *pktMkdir:
		return fs.PktMkdir(ctx, pkt)
	case *pktRmdir:
		return fs.PktRmdir(ctx, pkt)

	// 6.7
	case *pktOpenDir:
		return fs.PktOpenDir(ctx, pkt)
	case *pktReadDir:
		return fs.PktReadDir(ctx, pkt)

	// 6.8
	case *pktStat:
		return fs.PktStat(ctx, pkt)
	case *pktLstat:
		return fs.PktLstat(ctx, pkt)
	case *pktFstat:
		return fs.PktFstat(ctx, pkt)

	// 6.9
	case *pktSetStat:
		return fs.PktSetStat(ctx, pkt)
	case *pktFSetStat:
		return fs.PktFSetStat(ctx, pkt)

	// 6.10
	case *pktReadlink:
		return fs.PktReadlink(ctx, pkt)
	case *pktSymlink:
		return fs.PktSymlink(ctx, pkt)

	// 6.11
	case *pktRealPath:
		return fs.PktRealPath(ctx, pkt)

	default:
		return nil, fmt.Errorf("did not handle: %T %+v", pkt, pkt)
	}
}
