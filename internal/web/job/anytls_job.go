package job

import (
	"github.com/mhsanaei/3x-ui/v3/internal/anytls"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// AnytlsJob reconciles the running anytls-server sidecars against the enabled
// anytls inbounds, restarts crashed ones, and folds their scraped per-client
// traffic into the usual accounting.
type AnytlsJob struct {
	inboundService service.InboundService
}

func NewAnytlsJob() *AnytlsJob {
	return new(AnytlsJob)
}

// Run reconciles desired anytls inbounds with running nodes and records
// per-client traffic deltas and online status.
func (j *AnytlsJob) Run() {
	desired, err := j.inboundService.DesiredAnytlsInstances()
	if err != nil {
		logger.Warning("anytls job: get desired instances failed:", err)
		return
	}

	routedTags := make(map[string]bool)
	activeTags := make([]string, 0, len(desired))
	for _, inst := range desired {
		activeTags = append(activeTags, inst.Tag)
		if inst.RouteThroughXray {
			routedTags[inst.Tag] = true
		}
	}

	mgr := anytls.GetManager()
	mgr.Reconcile(desired)

	deltas, onlineEmails := mgr.CollectTraffic()

	// A routed inbound's total is already metered through the Xray bridge, so
	// only non-routed inbounds are rolled up; per-client deltas are always kept.
	clientTraffics := make([]*xray.ClientTraffic, 0, len(deltas))
	inboundUp := make(map[string]int64)
	inboundDown := make(map[string]int64)
	for _, d := range deltas {
		clientTraffics = append(clientTraffics, &xray.ClientTraffic{
			Email: d.Email,
			Up:    d.Up,
			Down:  d.Down,
		})
		if !routedTags[d.Tag] {
			inboundUp[d.Tag] += d.Up
			inboundDown[d.Tag] += d.Down
		}
	}

	traffics := make([]*xray.Traffic, 0, len(inboundUp))
	for tag, up := range inboundUp {
		traffics = append(traffics, &xray.Traffic{
			IsInbound: true,
			Tag:       tag,
			Up:        up,
			Down:      inboundDown[tag],
		})
	}

	if len(traffics) > 0 || len(clientTraffics) > 0 {
		if _, _, err := j.inboundService.AddTraffic(traffics, clientTraffics); err != nil {
			logger.Warning("anytls job: add traffic failed:", err)
		}
	}

	j.inboundService.RefreshLocalOnlineClients(onlineEmails, activeTags)
}
