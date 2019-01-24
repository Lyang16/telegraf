package kube_inventory

import (
	"context"
	"io/ioutil"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/internal/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

// KubernetesInventory represents the config object for the plugin.
type KubernetesInventory struct {
	URL               string            `toml:"url"`
	BearerToken       string            `toml:"bearer_token"`
	BearerTokenString string            `toml:"bearer_token_string"`
	Namespace         string            `toml:"namespace"`
	ResponseTimeout   internal.Duration `toml:"response_timeout"` // Timeout specified as a string - 3s, 1m, 1h
	ResourceExclude   []string          `toml:"resource_exclude"`
	ResourceInclude   []string          `toml:"resource_include"`
	MaxConfigMapAge   internal.Duration `toml:"max_config_map_age"`

	tls.ClientConfig
	client *client
}

var sampleConfig = `
  ## URL for the kubelet
  url = "https://127.0.0.1"

  ## Namespace to use
  # namespace = "default"

  ## Use bearer token for authorization. ('bearer_token' takes priority)
  # bearer_token = "/path/to/bearer/token"
  ## OR
  # bearer_token_string = "abc_123"

  ## Set response_timeout (default 5 seconds)
  # response_timeout = "5s"

  ## Optional Resources to exclude from gathering
  ## Leave them with blank with try to gather everything available.
  ## Values can be - "daemonsets", deployments", "nodes", "persistentvolumes",
  ## "persistentvolumeclaims", "pods", "statefulsets"
  # resource_exclude = [ "deployments", "nodes", "statefulsets" ]

  ## Optional Resources to include when gathering
  ## Overrides resource_exclude if both set.
  # resource_include = [ "deployments", "nodes", "statefulsets" ]

  ## Optional TLS Config
  # tls_ca = "/path/to/cafile"
  # tls_cert = "/path/to/certfile"
  # tls_key = "/path/to/keyfile"
  ## Use TLS but skip chain & host verification
  # insecure_skip_verify = false
`

// SampleConfig returns a sample config
func (ki *KubernetesInventory) SampleConfig() string {
	return sampleConfig
}

// Description returns the description of this plugin
func (ki *KubernetesInventory) Description() string {
	return "Read metrics from the Kubernetes api"
}

// Gather collects kubernetes metrics from a given URL.
func (ki *KubernetesInventory) Gather(acc telegraf.Accumulator) (err error) {
	if ki.client == nil {
		if ki.client, err = ki.initClient(); err != nil {
			return err
		}
	}

	wg := sync.WaitGroup{}
	ctx := context.Background()

	if len(ki.ResourceInclude) == 0 {
		for _, f := range availableCollectors {
			wg.Add(1)
			go func(f func(ctx context.Context, acc telegraf.Accumulator, k *KubernetesInventory)) {
				defer wg.Done()
				f(ctx, acc, ki)
			}(f)
		}
	} else {
		for _, n := range ki.ResourceInclude {
			wg.Add(1)
			go func(f func(ctx context.Context, acc telegraf.Accumulator, k *KubernetesInventory)) {
				defer wg.Done()
				f(ctx, acc, ki)
			}(availableCollectors[n])
		}
	}

	wg.Wait()

	return nil
}

var availableCollectors = map[string]func(ctx context.Context, acc telegraf.Accumulator, ki *KubernetesInventory){
	"daemonsets":             collectDaemonSets,
	"deployments":            collectDeployments,
	"nodes":                  collectNodes,
	"persistentvolumes":      collectPersistentVolumes,
	"persistentvolumeclaims": collectPersistentVolumeClaims,
	"pods":                   collectPods,
	"statefulsets":           collectStatefulSets,
}

func (ki *KubernetesInventory) initClient() (*client, error) {
	if len(ki.ResourceInclude) == 0 {
		for i := range ki.ResourceExclude {
			delete(availableCollectors, ki.ResourceExclude[i])
		}
	}

	if ki.BearerToken != "" {
		token, err := ioutil.ReadFile(ki.BearerToken)
		if err != nil {
			return nil, err
		}
		ki.BearerTokenString = strings.TrimSpace(string(token))
	}

	return newClient(ki.URL, ki.Namespace, ki.BearerTokenString, ki.ResponseTimeout.Duration, ki.ClientConfig)
}

func atoi(s string) int64 {
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return int64(i)
}

var (
	daemonSetMeasurement             = "kubernetes_daemonset"
	deploymentMeasurement            = "kubernetes_deployment"
	nodeMeasurement                  = "kubernetes_node"
	persistentVolumeMeasurement      = "kubernetes_persistentvolume"
	persistentVolumeClaimMeasurement = "kubernetes_persistentvolumeclaim"
	podContainerMeasurement          = "kubernetes_pod_container"
	statefulSetMeasurement           = "kubernetes_statefulset"
)

func init() {
	inputs.Add("kube_inventory", func() telegraf.Input {
		return &KubernetesInventory{
			ResponseTimeout: internal.Duration{Duration: time.Second * 5},
			Namespace:       "default",
		}
	})
}
