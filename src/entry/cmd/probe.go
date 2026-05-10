package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var probeCmd = &cobra.Command{
	Use:   "probe",
	Short: "Run a libp2p latency or throughput probe to a peer",
	RunE: func(cmd *cobra.Command, args []string) error {
		target, _ := cmd.Flags().GetString("target")
		kind, _ := cmd.Flags().GetString("kind")
		count, _ := cmd.Flags().GetInt("count")
		nbytes, _ := cmd.Flags().GetInt64("bytes")
		timeout, _ := cmd.Flags().GetDuration("timeout")
		if target == "" {
			return fmt.Errorf("--target is required")
		}
		body, _ := json.Marshal(map[string]any{
			"target":     target,
			"kind":       kind,
			"count":      count,
			"bytes":      nbytes,
			"timeout_ms": int(timeout / time.Millisecond),
		})
		port := viper.GetString("port")
		if port == "" {
			port = "8092"
		}
		url := "http://127.0.0.1:" + port + "/v1/probe/run"
		ctx := cmd.Context()
		if ctx == nil {
			if root := cmd.Root(); root != nil {
				ctx = root.Context()
			}
		}
		if ctx == nil {
			ctx = context.Background()
		}
		client := &http.Client{Timeout: timeout + 5*time.Second}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("contact local node at %s: %w", url, err)
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		fmt.Fprintln(cmd.OutOrStdout(), string(respBody))
		var parsed struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return fmt.Errorf("parse response: %w", err)
		}
		if !parsed.OK {
			return fmt.Errorf("probe failed: %s", parsed.Error)
		}
		return nil
	},
}

//nolint:gochecknoinits
func init() {
	probeCmd.Flags().String("target", "", "target peer id (required)")
	probeCmd.Flags().String("kind", "latency", "probe kind: latency | throughput | libp2p_ping")
	probeCmd.Flags().Int("count", 20, "number of samples")
	probeCmd.Flags().Int64("bytes", 10*1024*1024, "bytes per iteration (throughput only)")
	probeCmd.Flags().Duration("timeout", 60*time.Second, "total probe timeout")
}
