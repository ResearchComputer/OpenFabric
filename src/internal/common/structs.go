package common

type Bootstraps struct {
	Bootstraps []string `json:"bootstraps"`
}

type ModelData struct {
	Id        string `json:"id"`
	Object    string `json:"object"`
	CreatedAt int64 `json:"created"`
	OwnedBy   string `json:"owned_by"`
}

type LMAvailableModels struct {
	Object string      `json:"object"`
	Models []ModelData `json:"data"`
}

type GPUSpec struct {
	Name        string `json:"name"`
	TotalMemory int64  `json:"total_memory"`
	UsedMemory  int64  `json:"used_memory"`
}

type HardwareSpec struct {
	GPUs            []GPUSpec `json:"gpus"`
	Memory          int64     `json:"host_memory"`
	MemoryBandwidth int64     `json:"host_memory_bandwidth"`
	UsedMemory      int64     `json:"host_memory_used"`
}
