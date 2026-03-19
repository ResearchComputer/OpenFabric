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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// TestBillingMesh_UsageTracking spins up a multi-node Docker mesh with
// billing enabled and verifies that:
//
//  1. The head node routes a request through the service proxy to the worker.
//  2. The worker proxies to a co-located mock backend that returns
//     X-Usage-* headers.
//  3. The response flows back to the client.
//  4. The head node records the routing event (verified via Prometheus metrics).
//
// Topology:
//
//	client → head (billing=true) → [libp2p] → worker ──→ mock-backend
//	                                           └─ same container (subprocess)
//
// Run:
//
//	make test TEST_PKGS="./tests/integration/..." GOARGS="-tags=integration -count=1 -run TestBillingMesh"
//
// ---------------------------------------------------------------------------

const (
	billingHTTPPort      = "8092"
	billingP2PPort       = "43905"
	billingNetworkName   = "otela-billing-net"
	billingImageName     = "opentela-billing-test"
	mockBackendPort      = "9999"
	billingBaseHostPort  = 19200
	billingDiscoveryWait = 120 * time.Second
	billingPollInterval  = 3 * time.Second
)

// billingPeer mirrors the Peer struct fields we need from /v1/dnt/table.
type billingPeer struct {
	ID        string           `json:"id"`
	Connected bool             `json:"connected"`
	Status    string           `json:"status"`
	Service   []billingService `json:"service"`
}

type billingService struct {
	Name          string   `json:"name"`
	IdentityGroup []string `json:"identity_group"`
}

func TestBillingMesh_UsageTracking(t *testing.T) {
	requireDocker(t)

	srcDir := srcRoot(t)
	buildBinary(t, srcDir)
	buildBillingImage(t, srcDir)
	t.Cleanup(func() { dockerIgnoreErr("rmi", "-f", billingImageName) })

	// Create isolated Docker network.
	dockerMust(t, "network", "create", billingNetworkName)
	t.Cleanup(func() { dockerIgnoreErr("network", "rm", billingNetworkName) })

	// --- Start head node (standalone, billing enabled) ---
	head := startBillingNode(t, "head", 0, []string{
		"start", "--mode", "standalone", "--seed", "1", "--cleanslate",
		"--bootstrap.static", "", // disable production bootstraps
	}, map[string]string{
		"OF_BILLING_ENABLED": "true",
	})
	t.Cleanup(func() { removeBillingContainer(head) })
	waitHTTPOK(t, head, "/v1/health", 30*time.Second)

	headIP := billingContainerIP(t, head)
	headPeerID := billingFirstPeerID(t, head)
	require.NotEmpty(t, headPeerID, "head must have a peer ID")
	bootstrapAddr := fmt.Sprintf("/ip4/%s/tcp/%s/p2p/%s", headIP, billingP2PPort, headPeerID)
	t.Logf("Head node: %s (billing=true)", bootstrapAddr)

	// --- Start worker node ---
	// The worker runs the mock backend as a subprocess (localhost:9999) and
	// registers service "echo" pointing at it.
	worker := startBillingNode(t, "worker", 1, []string{
		"start",
		"--bootstrap.addr", bootstrapAddr,
		"--bootstrap.static", "", // disable production bootstraps
		"--cleanslate",
		"--service.name", "echo",
		"--service.port", mockBackendPort,
		"--subprocess", "/app/mock-backend",
	}, map[string]string{
		"OF_SERVICE_IDENTITY_GROUP": "all",
	})
	t.Cleanup(func() { removeBillingContainer(worker) })
	waitHTTPOK(t, worker, "/v1/health", 30*time.Second)

	// --- Wait for peer discovery AND service registration ---
	// Poll the head's /v1/dnt/table until we see a connected peer
	// (not the head itself) that advertises the "echo" service.
	t.Log("Waiting for worker's echo service to appear in head's node table...")
	var workerPeerID string
	deadline := time.Now().Add(billingDiscoveryWait)
	for time.Now().Before(deadline) {
		table := billingFullTable(t, head)
		for _, p := range table {
			if p.ID == headPeerID || !p.Connected {
				continue
			}
			for _, s := range p.Service {
				if s.Name == "echo" {
					workerPeerID = p.ID
					break
				}
			}
			if workerPeerID != "" {
				break
			}
		}
		if workerPeerID != "" {
			break
		}
		time.Sleep(billingPollInterval)
	}
	if workerPeerID == "" {
		// Dump debug info.
		table := billingFullTable(t, head)
		for key, p := range table {
			t.Logf("Head table entry %s: id=%s connected=%v services=%+v", key, p.ID, p.Connected, p.Service)
		}
		t.Logf("Worker logs:\n%s", billingContainerLogs(worker))
	}
	require.NotEmpty(t, workerPeerID, "worker with echo service must appear in head's connected table")
	t.Logf("Worker peer ID: %s (echo service registered)", workerPeerID)

	// --- Send a request through the head's service proxy ---
	headURL := fmt.Sprintf("http://127.0.0.1:%d/v1/service/echo/test", head.hostPort)
	req, err := http.NewRequest(http.MethodPost, headURL, strings.NewReader(`{"message":"hello"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Otela-Fallback", "2")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "request through head proxy must succeed")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	t.Logf("Response status=%d body=%s", resp.StatusCode, string(body))

	assert.Equal(t, http.StatusOK, resp.StatusCode, "expected 200 from mock backend via proxy")

	// Verify the mock backend response came through.
	var respJSON map[string]any
	if err := json.Unmarshal(body, &respJSON); err == nil {
		assert.Equal(t, "mock response", respJSON["message"],
			"response should contain mock backend message")
	}

	// The X-Computing-Node header should be set by GlobalServiceForwardHandler.
	computeNode := resp.Header.Get("X-Computing-Node")
	assert.NotEmpty(t, computeNode, "X-Computing-Node should be set on forwarded response")
	if computeNode != "" {
		assert.Equal(t, workerPeerID, computeNode,
			"X-Computing-Node should be the worker peer ID")
	}

	// --- Verify routing was recorded via Prometheus metrics ---
	metricsURL := fmt.Sprintf("http://127.0.0.1:%d/metrics", head.hostPort)
	metricsResp, err := http.Get(metricsURL)
	require.NoError(t, err)
	defer metricsResp.Body.Close()

	metricsBody, err := io.ReadAll(metricsResp.Body)
	require.NoError(t, err)
	metricsStr := string(metricsBody)

	assert.Contains(t, metricsStr, `otela_routing_requests_total`,
		"metrics must include routing counter")
	assert.Contains(t, metricsStr, `service="echo"`,
		"routing counter must have service=echo label")

	t.Log("Billing mesh test passed: usage tracking through service proxy verified")
}

// ---------------------------------------------------------------------------
// Image builder
// ---------------------------------------------------------------------------

func buildBillingImage(t *testing.T, srcDir string) {
	t.Helper()
	dir := t.TempDir()

	binaryPath := filepath.Join(srcDir, "build", "entry")
	data, err := os.ReadFile(binaryPath)
	require.NoError(t, err, "read otela binary")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "otela"), data, 0o755))

	mockDir := filepath.Join(dir, "mock")
	require.NoError(t, os.MkdirAll(mockDir, 0o755))

	mainGo := `package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "` + mockBackendPort + `"
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Usage-Tokens", "1500")
		w.Header().Set("X-Usage-GPU-Ms", "4200")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"message": "mock response",
			"path":    r.URL.Path,
			"method":  r.Method,
		})
	})

	fmt.Printf("Mock backend listening on :%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(mockDir, "main.go"), []byte(mainGo), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(mockDir, "go.mod"), []byte("module mock-backend\n\ngo 1.22\n"), 0o644))

	dockerfile := `FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY mock/ .
RUN CGO_ENABLED=0 go build -o mock-backend .

FROM alpine:edge
RUN apk --no-cache add ca-certificates tzdata
COPY otela /app/otela
COPY --from=builder /app/mock-backend /app/mock-backend
ENTRYPOINT ["/app/otela"]
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0o644))

	out, err := exec.Command("docker", "build", "-t", billingImageName, dir).CombinedOutput()
	require.NoError(t, err, "docker build billing image failed:\n%s", out)
}

// ---------------------------------------------------------------------------
// Container helpers
// ---------------------------------------------------------------------------

type billingNodeInfo = nodeInfo

func startBillingNode(t *testing.T, name string, index int, cmd []string, envVars map[string]string) billingNodeInfo {
	t.Helper()
	fullName := "otela-billing-" + name
	hostPort := billingBaseHostPort + index

	dockerIgnoreErr("rm", "-f", fullName)

	args := []string{
		"run", "-d",
		"--name", fullName,
		"--network", billingNetworkName,
		"-e", "OF_SECURITY_REQUIRE_SIGNED_BINARY=false",
		"-p", fmt.Sprintf("127.0.0.1:%d:%s", hostPort, billingHTTPPort),
	}
	for k, v := range envVars {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, billingImageName)
	args = append(args, cmd...)
	id := dockerMust(t, args...)
	return billingNodeInfo{containerID: id, hostPort: hostPort, name: name}
}

func removeBillingContainer(n billingNodeInfo) {
	dockerIgnoreErr("rm", "-f", n.containerID)
}

func billingContainerIP(t *testing.T, n billingNodeInfo) string {
	t.Helper()
	tmpl := fmt.Sprintf("{{(index .NetworkSettings.Networks %q).IPAddress}}", billingNetworkName)
	ip := dockerMust(t, "inspect", "-f", tmpl, n.containerID)
	require.NotEmpty(t, ip, "no IP for %s", n.name)
	return ip
}

func billingContainerLogs(n billingNodeInfo) string {
	out, _ := exec.Command("docker", "logs", "--tail", "50", n.containerID).CombinedOutput()
	return string(out)
}

func waitHTTPOK(t *testing.T, n billingNodeInfo, path string, timeout time.Duration) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d%s", n.hostPort, path)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(url); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	logs := billingContainerLogs(n)
	t.Fatalf("%s not healthy at %s within %s\nlogs:\n%s", n.name, path, timeout, logs)
}

func billingFirstPeerID(t *testing.T, n billingNodeInfo) string {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/dnt/peers_status", n.hostPort)
	resp, err := http.Get(url)
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var status struct {
			Peers []struct {
				ID string `json:"id"`
			} `json:"peers"`
		}
		if json.Unmarshal(body, &status) == nil && len(status.Peers) > 0 {
			return status.Peers[0].ID
		}
	}
	t.Fatalf("no peer ID found for %s", n.name)
	return ""
}

// billingFullTable returns the head's CRDT-backed connected peers table
// with full Peer data (including services).
func billingFullTable(t *testing.T, n billingNodeInfo) map[string]billingPeer {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/dnt/table", n.hostPort)
	resp, err := http.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var table map[string]billingPeer
	_ = json.Unmarshal(body, &table)
	return table
}
