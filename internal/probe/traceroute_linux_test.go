//go:build linux

package probe

import (
	"strings"
	"testing"

	"github.com/Arylite/netprobe/internal/api"
)

func TestTracerouteReachesLoopback(t *testing.T) {
	o := local().Run(ctx(), api.Check{Kind: api.KindRoute, Target: "127.0.0.1", IntervalSeconds: 60})
	if strings.Contains(o.Err, "not allowed here") {
		t.Skip(o.Err)
	}
	wantOK(t, o)
	wantFail(t, New(DefaultPolicy(), testTimeout).Run(ctx(), api.Check{Kind: api.KindRoute, Target: "127.0.0.1"}), "denied by the policy")
}
