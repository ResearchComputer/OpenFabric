//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	numWorkers      = 3
	httpPort        = "8092"
	p2pPort         = "43905"
	discoverTimeout = 90 * time.Second
	pollInterval    = 3 * time.Second
	imageName       = "opentela-integration-test"
	networkName     = "opentela-test-net"
	baseHostPort    = 19092
)

type peerEntry struct {
	ID        string `json:"id"`
	Connected bool   `json:"connected"`
	Status    string `json:"status"`
}

type nodeInfo struct {
	containerID string
	hostPort    int
	name        string
}

// TestPeerDiscovery verifies that N nodes connected to the same bootstrap
// discover each other through DHT + GossipSub + CRDT replication.
//
// Topology: 1 bootstrap (standalone) + 3 workers → all 4 should see each other.
//
// Run: make test TEST_PKGS="./tests/integration/..." GOARGS="-tags=integration -count=1"
func TestPeerDiscovery(t *testing.T) {
	requireDocker(t)
	totalNodes := numWorkers + 1

	// Build binary and Docker image
	srcDir := srcRoot(t)
	buildBinary(t, srcDir)
	buildImage(t, srcDir)
	t.Cleanup(func() { dockerIgnoreErr("rmi", "-f", imageName) })

	// Create isolated Docker network
	dockerMust(t, "network", "create", networkName)
	t.Cleanup(func() { dockerIgnoreErr("network", "rm", networkName) })

	// Start bootstrap node (standalone mode, deterministic key from seed=1)
	bootstrap := startNode(t, "bootstrap", 0, []string{
		"start", "--mode", "standalone", "--seed", "1", "--cleanslate=false",
	})
	t.Cleanup(func() { removeContainer(bootstrap) })
	waitHealthy(t, bootstrap)

	// Discover bootstrap's peer ID and container IP
	bootstrapIP := containerIP(t, bootstrap)
	bootstrapPeerID := firstPeerID(t, bootstrap)
	require.NotEmpty(t, bootstrapPeerID, "bootstrap must have a peer ID")
	bootstrapAddr := fmt.Sprintf("/ip4/%s/tcp/%s/p2p/%s", bootstrapIP, p2pPort, bootstrapPeerID)
	t.Logf("Bootstrap: %s", bootstrapAddr)

	// Start worker nodes pointing at the bootstrap
	workers := make([]nodeInfo, numWorkers)
	for i := range workers {
		workers[i] = startNode(t, fmt.Sprintf("worker-%d", i), i+1, []string{
			"start",
			"--bootstrap.addr", bootstrapAddr,
			"--cleanslate=false",
		})
		t.Cleanup(func() { removeContainer(workers[i]) })
	}
	for _, w := range workers {
		waitHealthy(t, w)
	}

	// Poll until every node sees all others as connected in its CRDT node table
	allNodes := append([]nodeInfo{bootstrap}, workers...)
	deadline := time.Now().Add(discoverTimeout)
	for time.Now().Before(deadline) {
		allOK := true
		for _, node := range allNodes {
			table := nodeTable(t, node)
			n := countConnected(table)
			if n < totalNodes {
				t.Logf("  %-12s %d/%d connected", node.name, n, totalNodes)
				allOK = false
			}
		}
		if allOK {
			t.Logf("All %d nodes discovered each other", totalNodes)
			// Final assertion
			for _, node := range allNodes {
				table := nodeTable(t, node)
				assert.GreaterOrEqual(t, countConnected(table), totalNodes,
					"%s should see all %d peers", node.name, totalNodes)
			}
			return
		}
		time.Sleep(pollInterval)
	}

	// Timeout — dump final state for debugging
	t.Log("--- Final state ---")
	for _, node := range allNodes {
		table := nodeTable(t, node)
		t.Logf("%s: %d connected", node.name, countConnected(table))
		for k, p := range table {
			t.Logf("  %s connected=%v status=%s", k, p.Connected, p.Status)
		}
		logs := containerLogs(node)
		t.Logf("%s logs (last 30 lines):\n%s", node.name, logs)
	}
	t.Fatalf("timeout: not all %d nodes discovered each other within %s", totalNodes, discoverTimeout)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func requireDocker(t *testing.T) {
	t.Helper()
	if out, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skipf("Docker not available: %v\n%s", err, out)
	}
}

func srcRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	// file is src/tests/integration/peer_discovery_test.go → src/
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func buildBinary(t *testing.T, srcDir string) {
	t.Helper()
	cmd := exec.Command("make", "build")
	cmd.Dir = srcDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "make build failed:\n%s", out)
}

func buildImage(t *testing.T, srcDir string) {
	t.Helper()
	dir := t.TempDir()

	// Copy the pre-built binary into the Docker context
	binaryPath := filepath.Join(srcDir, "build", "entry")
	data, err := os.ReadFile(binaryPath)
	require.NoError(t, err, "read binary")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "otela"), data, 0755))

	dockerfile := strings.Join([]string{
		"FROM alpine:edge",
		"RUN apk --no-cache add ca-certificates tzdata",
		"COPY otela /app/otela",
		`ENTRYPOINT ["/app/otela"]`,
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0644))

	out, err := exec.Command("docker", "build", "-t", imageName, dir).CombinedOutput()
	require.NoError(t, err, "docker build failed:\n%s", out)
}

func startNode(t *testing.T, name string, index int, cmd []string) nodeInfo {
	t.Helper()
	fullName := "opentela-test-" + name
	hostPort := baseHostPort + index

	// Remove leftover container from a previous run
	dockerIgnoreErr("rm", "-f", fullName)

	args := []string{
		"run", "-d",
		"--name", fullName,
		"--network", networkName,
		"-p", fmt.Sprintf("127.0.0.1:%d:%s", hostPort, httpPort),
		imageName,
	}
	args = append(args, cmd...)
	id := dockerMust(t, args...)
	return nodeInfo{containerID: id, hostPort: hostPort, name: name}
}

func removeContainer(n nodeInfo) {
	dockerIgnoreErr("rm", "-f", n.containerID)
}

func containerIP(t *testing.T, n nodeInfo) string {
	t.Helper()
	tmpl := fmt.Sprintf("{{(index .NetworkSettings.Networks %q).IPAddress}}", networkName)
	ip := dockerMust(t, "inspect", "-f", tmpl, n.containerID)
	require.NotEmpty(t, ip, "no IP for %s", n.name)
	return ip
}

func containerLogs(n nodeInfo) string {
	out, _ := exec.Command("docker", "logs", "--tail", "30", n.containerID).CombinedOutput()
	return string(out)
}

func waitHealthy(t *testing.T, n nodeInfo) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/health", n.hostPort)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(url); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	logs := containerLogs(n)
	t.Fatalf("%s not healthy within 30s\nlogs:\n%s", n.name, logs)
}

func nodeTable(t *testing.T, n nodeInfo) map[string]peerEntry {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/dnt/table", n.hostPort)
	resp, err := http.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var table map[string]peerEntry
	_ = json.Unmarshal(body, &table)
	return table
}

func firstPeerID(t *testing.T, n nodeInfo) string {
	t.Helper()
	table := nodeTable(t, n)
	for _, p := range table {
		return p.ID
	}
	t.Fatalf("no peers in table for %s", n.name)
	return ""
}

func countConnected(table map[string]peerEntry) int {
	n := 0
	for _, p := range table {
		if p.Connected {
			n++
		}
	}
	return n
}

func dockerMust(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	require.NoError(t, err, "docker %v failed:\n%s", args, out)
	return strings.TrimSpace(string(out))
}

func dockerIgnoreErr(args ...string) {
	exec.Command("docker", args...).Run()
}
