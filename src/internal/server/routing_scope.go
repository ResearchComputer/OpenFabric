package server

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	serviceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)
	regionSlugPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

type routingPartition string

const (
	partitionPermissionless routingPartition = "permissionless"
	partitionTrustedRegion  routingPartition = "trusted_region"
)

type routeKind string

const (
	routeKindServiceIngress routeKind = "service_ingress"
	routeKindP2PIngress     routeKind = "p2p_ingress"
	routeKindWorker         routeKind = "worker"
)

type routingScope struct {
	Partition routingPartition
	Region    string
	RouteKind routeKind
	Service   string
}

func (s routingScope) trusted() bool {
	return s.Partition == partitionTrustedRegion
}

func (s routingScope) validate() error {
	if !serviceNamePattern.MatchString(s.Service) {
		return fmt.Errorf("invalid service name %q", s.Service)
	}
	if s.Partition == partitionTrustedRegion {
		if !regionSlugPattern.MatchString(s.Region) {
			return fmt.Errorf("invalid region %q", s.Region)
		}
		return nil
	}
	if s.Region != "" {
		return fmt.Errorf("permissionless scope must not set region")
	}
	return nil
}

func newRoutingScope(partition routingPartition, region string, kind routeKind, service string) (routingScope, error) {
	scope := routingScope{
		Partition: partition,
		Region:    strings.TrimSpace(region),
		RouteKind: kind,
		Service:   strings.TrimSpace(service),
	}
	if err := scope.validate(); err != nil {
		return routingScope{}, err
	}
	return scope, nil
}
