Deploy a signed OpenTela binary to the two-head-node test mesh (ocf-1, ocf-2). Follow each step below in order.

## Hosts

| Host | IP | User |
|------|-----|------|
| ocf-1 | 140.238.223.116 | ubuntu |
| ocf-2 | 152.67.64.117 | ubuntu |

## Steps

### 1. Check BUILD_SIGN_KEY

Check if `BUILD_SIGN_KEY` is set in the environment:

```bash
echo "${BUILD_SIGN_KEY:?BUILD_SIGN_KEY is not set}"
```

If not set, ask the user to set it: `export BUILD_SIGN_KEY=<key>`

### 2. Build signed binary

```bash
cd src && make build-signed
```

Output binary: `src/build/entry`

### 3. Stop existing services (parallel)

```bash
ssh ocf-1 'sudo systemctl stop otela || true'
ssh ocf-2 'sudo systemctl stop otela || true'
```

### 4. Transfer binary (parallel)

```bash
scp src/build/entry ocf-1:/tmp/ocf && ssh ocf-1 'sudo mv /tmp/ocf /home/ubuntu/ocf && sudo chmod +x /home/ubuntu/ocf'
scp src/build/entry ocf-2:/tmp/ocf && ssh ocf-2 'sudo mv /tmp/ocf /home/ubuntu/ocf && sudo chmod +x /home/ubuntu/ocf'
```

### 5. Transfer systemd unit (parallel)

```bash
scp deploy/test/otela.service ocf-1:/tmp/otela.service && ssh ocf-1 'sudo mv /tmp/otela.service /etc/systemd/system/otela.service && sudo systemctl daemon-reload'
scp deploy/test/otela.service ocf-2:/tmp/otela.service && ssh ocf-2 'sudo mv /tmp/otela.service /etc/systemd/system/otela.service && sudo systemctl daemon-reload'
```

### 6. Transfer configs (parallel)

```bash
ssh ocf-1 'mkdir -p ~/.config/opentela' && scp deploy/test/ocf-1.cfg.yaml ocf-1:~/.config/opentela/cfg.yaml
ssh ocf-2 'mkdir -p ~/.config/opentela' && scp deploy/test/ocf-2.cfg.yaml ocf-2:~/.config/opentela/cfg.yaml
```

### 7. Start ocf-1

```bash
ssh ocf-1 'sudo systemctl start otela'
```

### 8. Wait for ocf-1 and fetch bootstrap

Poll every 2 seconds, up to 30 seconds:

```bash
for i in $(seq 1 15); do
  RESULT=$(curl -s http://140.238.223.116:8092/v1/dnt/bootstraps) && [ -n "$RESULT" ] && echo "$RESULT" && break
  sleep 2
done
```

Save the bootstrap multiaddr from the response.

**Note:** On first deploy, ocf-1 logs a warning about failing to reach ocf-2's bootstrap. This is expected.

### 9. Update ocf-2 config with ocf-1's bootstrap

Append to `deploy/test/ocf-2.cfg.yaml`:

```yaml
bootstrap:
  sources:
    - "<OCF1_BOOTSTRAP_MULTIADDR>"
```

Then SCP the updated config:

```bash
scp deploy/test/ocf-2.cfg.yaml ocf-2:~/.config/opentela/cfg.yaml
```

### 10. Start ocf-2

```bash
ssh ocf-2 'sudo systemctl start otela'
```

### 11. Wait for ocf-2 and fetch bootstrap

Poll every 2 seconds, up to 30 seconds:

```bash
for i in $(seq 1 15); do
  RESULT=$(curl -s http://152.67.64.117:8092/v1/dnt/bootstraps) && [ -n "$RESULT" ] && echo "$RESULT" && break
  sleep 2
done
```

### 12. Update ocf-1 config with ocf-2's bootstrap

Append to `deploy/test/ocf-1.cfg.yaml`:

```yaml
bootstrap:
  sources:
    - "<OCF2_BOOTSTRAP_MULTIADDR>"
```

Then SCP the updated config:

```bash
scp deploy/test/ocf-1.cfg.yaml ocf-1:~/.config/opentela/cfg.yaml
```

### 13. Restart ocf-1

```bash
ssh ocf-1 'sudo systemctl restart otela'
```

### 14. Verify mesh

Poll both nodes' peer tables (every 2s, up to 30s) until both show the other:

```bash
curl -s http://140.238.223.116:8092/v1/dnt/table
curl -s http://152.67.64.117:8092/v1/dnt/table
```

Both should list the other node. If verification fails, check logs:

```bash
ssh ocf-1 'journalctl -u otela -n 50 --no-pager'
ssh ocf-2 'journalctl -u otela -n 50 --no-pager'
```
