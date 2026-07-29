package server

import (
	"errors"
	"opentela/internal/protocol"
	"testing"
)

func TestResolveUniqueLocalService(t *testing.T) {
	protocol.ResetLocalServicesForTest()
	t.Cleanup(protocol.ResetLocalServicesForTest)

	protocol.SetLocalServicesForTest([]protocol.Service{
		{Name: "svc-one", Host: "localhost", Port: "8080"},
	})
	service, err := resolveUniqueLocalService("svc-one")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if service.Port != "8080" {
		t.Fatalf("port=%q, want 8080", service.Port)
	}
}

func TestResolveUniqueLocalServiceRejectsDuplicates(t *testing.T) {
	protocol.ResetLocalServicesForTest()
	t.Cleanup(protocol.ResetLocalServicesForTest)

	protocol.SetLocalServicesForTest([]protocol.Service{
		{Name: "svc-dupe", Host: "localhost", Port: "8080"},
		{Name: "svc-dupe", Host: "localhost", Port: "8081"},
	})
	_, err := resolveUniqueLocalService("svc-dupe")
	if !errors.Is(err, errDuplicateLocalService) {
		t.Fatalf("err=%v, want duplicate", err)
	}
}
