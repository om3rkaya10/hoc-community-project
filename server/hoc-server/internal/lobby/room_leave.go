package lobby

import (
	"fmt"

	"hoc-server/internal/config"
	"hoc-server/internal/session"
	"hoc-server/internal/wire/glblock"
)

func init() {
	session.RegisterRoomLeaveObserver(func(event session.RoomLeaveEvent) {
		if !event.Destroying || !event.WasHost {
			return
		}
		pkt := glblock.Empty(0xe02f)
		for _, survivor := range event.Survivors {
			if survivor == nil || survivor.Conn == nil {
				continue
			}
			if _, err := survivor.Conn.Write(pkt); err != nil {
				fmt.Printf(" [ROOM] survivor e02f write fail user=%q: %v\n", survivor.Username, err)
				continue
			}
			fmt.Printf(" [ROOM] survivor e02f clean-exit user=%q room=%d\n",
				survivor.Username, event.Room.ID)
			if config.ServerKitabe || config.ServerTalent {
				// The room links are reset just after observers return. Arm the same
				// one-shot trade-GS restore used by a voluntary e02e so a survivor
				// forced out by the host does not lose Kitabe/Talent transport.
				survivor.RequestTradeGSRebootstrap()
			}
		}
	})
}
