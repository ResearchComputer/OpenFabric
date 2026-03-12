package process

import (
	"testing"
)

func TestNewProcessManager_ReturnsSingleton(t *testing.T) {
	pm = nil
	pm1 := NewProcessManager()
	pm2 := NewProcessManager()
	if pm1 != pm2 {
		t.Fatal("expected NewProcessManager to return the same singleton instance")
	}
}

func TestNewProcessManager_NotNil(t *testing.T) {
	pm = nil
	result := NewProcessManager()
	if result == nil {
		t.Fatal("expected non-nil ProcessManager")
	}
}

func TestStartCriticalProcess_EmptyCommand(t *testing.T) {
	pm = nil
	// Should not panic when given an empty command string
	StartCriticalProcess("")
}

func TestHealthCheck_NoProcesses(t *testing.T) {
	pm = nil
	result := HealthCheck()
	if !result {
		t.Fatal("expected HealthCheck to return true when no processes exist")
	}
}
