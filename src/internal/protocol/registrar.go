package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"opentela/internal/common"

	"opentela/internal/platform"
	"sync"
	"time"

	ds "github.com/ipfs/go-datastore"
	"github.com/spf13/viper"
)

// localServices keeps a thread-safe copy of services this node provides
// so we can re-announce them on reconnects
var (
	localServices     []Service
	localServicesLock = &sync.RWMutex{}
)

// addLocalService appends (deduped) to localServices
func addLocalService(svc Service) {
	localServicesLock.Lock()
	defer localServicesLock.Unlock()
	// simple dedupe on Name|Host|Port
	key := svc.Name + "|" + svc.Host + "|" + svc.Port
	exists := false
	for i := range localServices {
		k := localServices[i].Name + "|" + localServices[i].Host + "|" + localServices[i].Port
		if k == key {
			// merge identity groups (dedupe)
			existing := make(map[string]struct{})
			for _, id := range localServices[i].IdentityGroup {
				existing[id] = struct{}{}
			}
			for _, id := range svc.IdentityGroup {
				if _, ok := existing[id]; !ok {
					localServices[i].IdentityGroup = append(localServices[i].IdentityGroup, id)
				}
			}
			exists = true
			break
		}
	}
	if !exists {
		localServices = append(localServices, svc)
	}
}

// snapshotLocalServices returns a copy of current local services
func snapshotLocalServices() []Service {
	localServicesLock.RLock()
	defer localServicesLock.RUnlock()
	out := make([]Service, len(localServices))
	copy(out, localServices)
	return out
}

func RegisterLocalServices() {
	serviceName := viper.GetString("service.name")
	servicePort := viper.GetString("service.port")
	if serviceName == "llm" && servicePort != "" {
		// register the service by first fetch available models on the port
		err := healthCheckRemote(servicePort, 6000)
		if err != nil {
			common.Logger.Error("could not health check LLM service: ", err)
			return
		}
		common.Logger.Debug("LLM service healthy")
		registerLLMService(servicePort)
	}
}

func healthCheckRemote(port string, maxTries int) error {
	const retryInterval = 10 * time.Second
	const logEveryN = 10 // log every 10 retries (~100s)
	start := time.Now()

	for tries := 1; tries <= maxTries; tries++ {
		_, err := common.RemoteGET("http://localhost:" + port + "/health")
		if err == nil {
			elapsed := time.Since(start).Truncate(time.Second)
			common.Logger.Infof("Health check passed after %d/%d attempts (%s elapsed)", tries, maxTries, elapsed)
			return nil
		}

		if tries == 1 || tries%logEveryN == 0 {
			elapsed := time.Since(start).Truncate(time.Second)
			remaining := time.Duration(maxTries-tries) * retryInterval
			common.Logger.Infof("Health check [%d/%d] elapsed %s, ~%s remaining",
				tries, maxTries, elapsed, remaining.Truncate(time.Second))
		}
		time.Sleep(retryInterval)
	}
	return fmt.Errorf("health check failed after %d attempts (%s elapsed)", maxTries, time.Since(start).Truncate(time.Second))
}

func registerLLMService(port string) {
	modelsBytes, err := common.RemoteGET("http://localhost:" + port + "/v1/models")
	if err != nil {
		common.Logger.Error("could not fetch models from LLM service: ", err)
	}
	common.Logger.Debug("Fetched models from LLM service: ", string(modelsBytes))
	var availableModels common.LMAvailableModels
	err = json.Unmarshal(modelsBytes, &availableModels)
	if err != nil {
		common.Logger.Error("could not unmarshal models from LLM service: ", err)
	}
	var identityGroup []string
	for _, model := range availableModels.Models {
		identityGroup = append(identityGroup, "model="+model.Id)
	}

	// register the models
	service := Service{
		Name:          "llm",
		Status:        "connected",
		Host:          "localhost",
		Port:          port,
		IdentityGroup: identityGroup,
	}
	provideService(service)
}

func provideService(service Service) {
	host, _ := GetP2PNode(nil)
	ctx := context.Background()
	store, _ := GetCRDTStore()
	key := ds.NewKey(host.ID().String())
	// track locally and publish full set (deduped)
	addLocalService(service)
	myself.Service = snapshotLocalServices()
	if viper.GetString("public-addr") != "" {
		myself.PublicAddress = viper.GetString("public-addr")
	}
	common.Logger.Debug("Registering LLM service: ", myself)
	value, err := json.Marshal(myself)
	UpdateNodeTableHook(key, value)
	common.ReportError(err, "Error while marshalling peer")
	err = store.Put(ctx, key, value)
	if err != nil {
		common.Logger.Debug("Error while providing service: ", err)
	}
}

// ReannounceLocalServices re-publishes this node's service entry, used after reconnects
func ReannounceLocalServices() {
	host, _ := GetP2PNode(nil)
	ctx := context.Background()
	store, _ := GetCRDTStore()
	key := ds.NewKey(host.ID().String())
	// refresh hardware and services
	myself.Hardware.GPUs = platform.GetGPUInfo()
	myself.Service = snapshotLocalServices()
	if viper.GetString("public-addr") != "" {
		myself.PublicAddress = viper.GetString("public-addr")
	}
	value, err := json.Marshal(myself)
	if err != nil {
		common.Logger.Error("Error marshalling self during reannounce: ", err)
		return
	}
	UpdateNodeTableHook(key, value)
	if err := store.Put(ctx, key, value); err != nil {
		common.Logger.Warn("Failed to reannounce local services: ", err)
	} else {
		common.Logger.Debug("Re-announced local services")
	}
}
