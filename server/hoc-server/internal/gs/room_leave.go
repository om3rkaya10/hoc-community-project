package gs

import "hoc-server/internal/session"

func init() {
	session.RegisterRoomLeaveObserver(func(event session.RoomLeaveEvent) {
		if event.Destroying && clock != nil {
			clock.Disarm(event.Room)
		}
		notifyRoomLeave3003(event.Room, event.Leaver, event.Reason)
	})
}
