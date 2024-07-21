package registry

import lockerLib "github.com/168yy/plus-core/core/v2/locker"

type LockerRegistry struct {
	registry[lockerLib.ILocker]
}
