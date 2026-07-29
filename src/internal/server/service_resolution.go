package server

import (
	"errors"
	"fmt"
	"net/http"
	"opentela/internal/protocol"
)

var (
	errLocalServiceNotFound  = errors.New("local service not found")
	errDuplicateLocalService = errors.New("duplicate local service name")
)

func resolveUniqueLocalService(name string) (protocol.Service, error) {
	if !serviceNamePattern.MatchString(name) {
		return protocol.Service{}, fmt.Errorf("invalid service name %q", name)
	}
	matches := protocol.LocalServicesByName(name)
	switch len(matches) {
	case 0:
		return protocol.Service{}, errLocalServiceNotFound
	case 1:
		return matches[0], nil
	default:
		return protocol.Service{}, errDuplicateLocalService
	}
}

func writeServiceResolutionError(statusWriter interface {
	JSON(int, any)
}, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errDuplicateLocalService):
		statusWriter.JSON(http.StatusForbidden, map[string]string{"error": "duplicate local service name"})
		return true
	case errors.Is(err, errLocalServiceNotFound):
		statusWriter.JSON(http.StatusForbidden, map[string]string{"error": "local service not found"})
		return true
	default:
		statusWriter.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return true
	}
}
