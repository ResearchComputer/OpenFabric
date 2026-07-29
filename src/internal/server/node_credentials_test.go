package server

import (
	"net/http"
	"testing"
)

func TestNodeCredentialResponseOK(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusOK, http.StatusCreated} {
		if !nodeCredentialResponseOK(status) {
			t.Fatalf("status %d should be accepted", status)
		}
	}
	for _, status := range []int{http.StatusNoContent, http.StatusBadRequest, http.StatusUnauthorized, http.StatusServiceUnavailable} {
		if nodeCredentialResponseOK(status) {
			t.Fatalf("status %d should be rejected", status)
		}
	}
}
