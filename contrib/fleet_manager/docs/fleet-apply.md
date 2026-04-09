# Fleet Apply

`otela-fleet apply` provides declarative multi-cluster deployment. Define your desired state in a YAML file and the fleet manager reconciles it.

## Fleet File Format

```yaml
deployments:
  - cluster: jsc
    backend: sglang
    cmd: "python3 -m sglang.launch_server --model-path Qwen/Qwen3-0.6B --port $SERVICE_PORT --host 127.0.0.1 --tp-size 4"
    preset: A100_4
    replicas: 2

  - cluster: jsc
    backend: vllm
    cmd: "python3 -m vllm.entrypoints.openai.api_server --model meta-llama/Llama-3-8B --port $SERVICE_PORT"
    preset: A100_4_dev
    replicas: 1

  - cluster: euler
    backend: sglang
    cmd: "python3 -m sglang.launch_server --model-path Qwen/Qwen3-0.6B --port $SERVICE_PORT --host 127.0.0.1"
    preset: RTX3090_1
    replicas: 1
```

## Usage

### Dry run (show planned changes)

```bash
otela-fleet apply fleet.yaml --dry-run
```

Output:
```
Fleet file: fleet.yaml
Clusters: euler, jsc

  jsc: 1 running jobs
  euler: 0 running jobs

Planned actions (3):
  + deploy sglang (A100_4) on jsc
  + deploy vllm (A100_4_dev) on jsc
  + deploy sglang (RTX3090_1) on euler

(dry run - no changes made)
```

### Apply

```bash
otela-fleet apply fleet.yaml
```

## Reconciliation

The fleet manager compares the desired state (fleet file) against live SLURM jobs:

- **Too few replicas**: submits additional jobs
- **Too many replicas**: cancels excess jobs (newest first)
- **Correct count**: no action

### Job Identity

Each deployment is identified by a hash of `backend + cmd + preset`. This means:
- Changing the command triggers a redeploy (new hash)
- Changing the preset triggers a redeploy (new hash)
- Changing only the replica count scales up/down without redeploying

## Scaling

To scale a deployment, change the `replicas` field and re-apply:

```yaml
# Scale from 2 to 4 replicas
  - cluster: jsc
    backend: sglang
    cmd: "..."
    preset: A100_4
    replicas: 4
```

```bash
otela-fleet apply fleet.yaml
```

To remove a deployment, set `replicas: 0` or remove the entry and re-apply.
