//go:build !linux

package probe

import (
	"context"
	"errors"
	"net/netip"
)

func trace(context.Context, netip.Addr, int) (route, error) {
	return route{}, errors.New("traceroute needs Linux")
}
