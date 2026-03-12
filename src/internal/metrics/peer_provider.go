package metrics

import (
	"opentela/internal/protocol"
	"strings"
)

type ServiceInfo struct {
	Name  string
	Model string
}

type NodeTablePeerProvider struct{}

func (p *NodeTablePeerProvider) GetScrapablePeers() []PeerInfo {
	table := protocol.GetConnectedPeers()
	if table == nil {
		return nil
	}

	var peers []PeerInfo
	for _, peer := range *table {
		if peer.ID == protocol.MyID {
			continue
		}
		if len(peer.Service) == 0 {
			continue
		}

		services := extractServices(peer.Service)
		labels := buildPeerLabels(peer.ID, peer.ProviderID, services)

		peers = append(peers, PeerInfo{
			ID:      peer.ID,
			Address: "libp2p://" + peer.ID,
			Labels:  labels,
		})
	}
	return peers
}

func extractServices(services []protocol.Service) []ServiceInfo {
	var infos []ServiceInfo
	for _, svc := range services {
		si := ServiceInfo{Name: svc.Name}
		for _, ig := range svc.IdentityGroup {
			parts := strings.SplitN(ig, "=", 2)
			if len(parts) == 2 && parts[0] == "model" {
				si.Model = parts[1]
			}
		}
		infos = append(infos, si)
	}
	return infos
}

func buildPeerLabels(peerID, providerID string, services []ServiceInfo) map[string]string {
	labels := map[string]string{
		"peer_id":     peerID,
		"provider_id": providerID,
	}
	if len(services) > 0 {
		if services[0].Name != "" {
			labels["service"] = services[0].Name
		}
		if services[0].Model != "" {
			labels["model"] = services[0].Model
		}
	}
	return labels
}
