//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	adminHostPort = 18093
	adminCtrPort  = "8093"
)

// startNodeWithExtras mirrors startNode but also accepts extra env vars
// (as "KEY=VAL" strings) and extra port mappings (as "host:ctr" strings).
func startNodeWithExtras(t *testing.T, name string, index int,
	env []string, extraPorts []string, cmd []string) nodeInfo {
	t.Helper()
	fullName := "opentela-test-" + name
	hostPort := baseHostPort + index

	dockerIgnoreErr("rm", "-f", fullName)

	args := []string{
		"run", "-d",
		"--name", fullName,
		"--network", networkName,
		"-e", "OF_SECURITY_REQUIRE_SIGNED_BINARY=false",
		"-p", fmt.Sprintf("127.0.0.1:%d:%s", hostPort, httpPort),
	}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	for _, p := range extraPorts {
		args = append(args, "-p", p)
	}
	args = append(args, imageName)
	args = append(args, cmd...)
	id := dockerMust(t, args...)
	return nodeInfo{containerID: id, hostPort: hostPort, name: name}
}

// TestAdminRoutePropagation spins up a two-node mesh, POSTs a unique
// convbench-* service to the bootstrap's admin route, and asserts that
// the worker observes the propagated service via /v1/dnt/table within
// 10 seconds.
//
// The admin server binds to the address configured by admin.bind
// (default 127.0.0.1). For Docker integration tests we override it to
// 0.0.0.0 via OF_ADMIN_BIND=0.0.0.0 so the mapped port is reachable
// from the test host.
func TestAdminRoutePropagation(t *testing.T) {
	requireDocker(t)

	srcDir := srcRoot(t)
	buildBinary(t, srcDir)
	buildImage(t, srcDir)
	t.Cleanup(func() { dockerIgnoreErr("rmi", "-f", imageName) })

	dockerMust(t, "network", "create", networkName)
	t.Cleanup(func() { dockerIgnoreErr("network", "rm", networkName) })

	// Bootstrap node: admin route enabled and bound to 0.0.0.0 so the
	// mapped port (adminHostPort → adminCtrPort) is reachable from the
	// test host.
	bootstrap := startNodeWithExtras(t, "admin-bootstrap", 0,
		[]string{
			"OF_ADMIN_ENABLED=true",
			"OF_ADMIN_PORT=" + adminCtrPort,
			"OF_ADMIN_BIND=0.0.0.0",
		},
		[]string{fmt.Sprintf("127.0.0.1:%d:%s", adminHostPort, adminCtrPort)},
		[]string{"start", "--mode", "standalone", "--seed", "1", "--cleanslate=false"},
	)
	t.Cleanup(func() { removeContainer(bootstrap) })
	waitHealthy(t, bootstrap)

	bootstrapIP := containerIP(t, bootstrap)
	bootstrapPeerID := firstPeerID(t, bootstrap)
	require.NotEmpty(t, bootstrapPeerID, "bootstrap must have a peer ID")
	bootstrapAddr := fmt.Sprintf("/ip4/%s/tcp/%s/p2p/%s", bootstrapIP, p2pPort, bootstrapPeerID)
	t.Logf("Bootstrap: %s", bootstrapAddr)

	worker := startNodeWithExtras(t, "admin-worker", 1,
		nil, nil,
		[]string{"start", "--bootstrap.addr", bootstrapAddr, "--cleanslate=false"},
	)
	t.Cleanup(func() { removeContainer(worker) })
	waitHealthy(t, worker)

	// POST a unique convbench-* service to the bootstrap admin route.
	svcName := fmt.Sprintf("convbench-test-%d", time.Now().UnixNano())
	body, _ := json.Marshal(map[string]any{"name": svcName, "port": 65000})
	postURL := fmt.Sprintf("http://127.0.0.1:%d/v1/_admin/register", adminHostPort)
	resp, err := http.Post(postURL, "application/json", bytes.NewReader(body))
	require.NoError(t, err, "POST to admin route failed")
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"admin register returned non-200: %s", respBody)
	t.Logf("Registered service %q, admin response: %s", svcName, respBody)

	// Poll the worker's CRDT node table until the service name appears.
	// /v1/dnt/table returns the full Peer objects including the Service
	// array, so the service name is present in the JSON body.
	workerTableURL := fmt.Sprintf("http://127.0.0.1:%d/v1/dnt/table", worker.hostPort)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r, err := http.Get(workerTableURL)
		if err == nil {
			raw, _ := io.ReadAll(r.Body)
			r.Body.Close()
			if strings.Contains(string(raw), svcName) {
				t.Logf("Worker observed service %q via CRDT propagation", svcName)
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Timeout — dump final state for debugging.
	t.Logf("Bootstrap logs:\n%s", containerLogs(bootstrap))
	t.Logf("Worker logs:\n%s", containerLogs(worker))
	t.Fatalf("worker did not observe service %q within 10s", svcName)
}
