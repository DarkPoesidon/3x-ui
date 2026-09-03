package service

import (
	"context"

	"github.com/mhsanaei/3x-ui/v3/internal/anytls"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// DesiredAnytlsInstances is the node config this panel should be running: one
// per enabled local anytls inbound, minus clients depletion-disabled in
// client_traffics, so the reconcile job and the push paths agree on one
// fingerprint.
func (s *InboundService) DesiredAnytlsInstances() ([]anytls.Instance, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).
		Where("protocol = ? AND enable = ? AND node_id IS NULL", model.AnyTLS, true).
		Find(&inbounds).Error
	if err != nil {
		return nil, err
	}
	if len(inbounds) == 0 {
		return nil, nil
	}

	ids := make([]int, 0, len(inbounds))
	for _, ib := range inbounds {
		ids = append(ids, ib.Id)
	}
	var disabledRows []xray.ClientTraffic
	err = db.Model(xray.ClientTraffic{}).
		Where("inbound_id IN ? AND enable = ?", ids, false).
		Select("inbound_id", "email").
		Find(&disabledRows).Error
	if err != nil {
		return nil, err
	}
	disabled := make(map[int]map[string]struct{}, len(disabledRows))
	for _, row := range disabledRows {
		if disabled[row.InboundId] == nil {
			disabled[row.InboundId] = map[string]struct{}{}
		}
		disabled[row.InboundId][row.Email] = struct{}{}
	}

	instances := make([]anytls.Instance, 0, len(inbounds))
	for _, ib := range inbounds {
		inst, ok := anytls.InstanceFromInbound(ib)
		if !ok {
			continue
		}
		if off := disabled[ib.Id]; len(off) > 0 {
			kept := make([]anytls.UserEntry, 0, len(inst.Users))
			for _, user := range inst.Users {
				if _, skip := off[user.Name]; !skip {
					kept = append(kept, user)
				}
			}
			inst.Users = kept
		}
		if len(inst.Users) == 0 {
			continue
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// applyLocalAnytls pushes one inbound's current client set to its node right
// after a client edit commits, so the change lands now instead of at the next
// reconcile tick. Failures are logged: the job is the backstop.
func (s *InboundService) applyLocalAnytls(inboundId int) {
	inbound, err := s.GetInbound(inboundId)
	if err != nil || inbound == nil || inbound.Protocol != model.AnyTLS || inbound.NodeID != nil {
		return
	}
	rt, err := s.runtimeFor(inbound)
	if err != nil {
		return
	}
	payload := inbound
	if inbound.Enable {
		if built, bErr := s.buildInboundForLocalRuntime(database.GetDB(), inbound); bErr == nil {
			payload = built
		}
	}
	if err := rt.UpdateInbound(context.Background(), inbound, payload); err != nil {
		logger.Debug("anytls: immediate client apply failed for inbound", inboundId, ":", err)
	}
}

// resetAnytlsClientQuota clears the node-side quota window for one client, so a
// renewed client is not re-blocked by the counter the node kept.
func (s *InboundService) resetAnytlsClientQuota(email string) {
	mgr := anytls.GetManager()
	if !mgr.HasRunning() {
		return
	}
	id, ok := s.localAnytlsInboundIdForEmail(email)
	if !ok {
		return
	}
	s.applyLocalAnytls(id)
	mgr.ResetQuota(email)
}

func (s *InboundService) resetAllAnytlsQuotas() {
	mgr := anytls.GetManager()
	if !mgr.HasRunning() {
		return
	}
	desired, err := s.DesiredAnytlsInstances()
	if err != nil {
		return
	}
	mgr.Reconcile(desired)
	for _, inst := range desired {
		for _, user := range inst.Users {
			mgr.ResetQuota(user.Name)
		}
	}
}

func (s *InboundService) localAnytlsInboundIdForEmail(email string) (int, bool) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	if err := db.Model(model.Inbound{}).
		Where("protocol = ? AND node_id IS NULL", model.AnyTLS).
		Find(&inbounds).Error; err != nil {
		return 0, false
	}
	for _, ib := range inbounds {
		inst, ok := anytls.InstanceFromInbound(ib)
		if !ok {
			continue
		}
		for _, user := range inst.Users {
			if user.Name == email {
				return ib.Id, true
			}
		}
	}
	return 0, false
}
