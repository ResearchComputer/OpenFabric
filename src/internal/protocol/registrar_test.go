package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegisterAdHocService_AddsToLocalServices(t *testing.T) {
	// Reset package state so test is isolated.
	localServicesLock.Lock()
	localServices = nil
	localServicesLock.Unlock()

	// RegisterAdHocService calls provideService which requires a live libp2p
	// host (os.Exit(1) if none). We test the in-memory half (addLocalService)
	// directly, using the same Service shape that RegisterAdHocService builds.
	// The full CRDT-publish path is exercised by the integration test in Task 4.
	svc := Service{
		Name:          "convbench-test",
		Status:        "connected",
		Host:          "localhost",
		Port:          "65000",
		IdentityGroup: nil,
	}
	addLocalService(svc)

	svcs := snapshotLocalServices()
	assert.Len(t, svcs, 1)
	assert.Equal(t, "convbench-test", svcs[0].Name)
	assert.Equal(t, "65000", svcs[0].Port)
	assert.Equal(t, "localhost", svcs[0].Host)
	assert.Equal(t, "connected", svcs[0].Status)
}

func TestLocalServiceSnapshot(t *testing.T) {
	// start with empty registry
	localServices = nil
	addLocalService(Service{Name: "llm", Host: "localhost", Port: "8000", IdentityGroup: []string{"model=a"}})
	addLocalService(Service{Name: "llm", Host: "localhost", Port: "8000", IdentityGroup: []string{"model=b"}})

	snap := snapshotLocalServices()
	if len(snap) != 1 {
		t.Fatalf("expected 1 service after dedupe, got %d", len(snap))
	}
	if len(snap[0].IdentityGroup) != 2 {
		t.Fatalf("expected merged identity groups, got %v", snap[0].IdentityGroup)
	}
}
