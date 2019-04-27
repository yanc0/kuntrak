package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/yanc0/untrak/outputs"
	"github.com/yanc0/untrak/utils"
	yaml "gopkg.in/yaml.v2"

	"github.com/yanc0/untrak/kubernetes"

	"github.com/yanc0/untrak/config"
)

const (
	// BANNER is what is printed for help/info output.
	BANNER = "Untrack - %s\n"
)

var (
	// version indicates which version of the binary is running.
	version string
)

func main() {
	// Flags, command line parameters
	var cfgPathOpt = flag.String("config", "./untrak.yaml", "untrak Config Path")
	var outputOpt = flag.String("o", "text", "Output format")
	var versionOpt = flag.Bool("version", false, "Print version of the application")
	flag.Parse()

	if *versionOpt {
		fmt.Printf("%s\n", version)
		os.Exit(0)
	}

	var wg sync.WaitGroup
	var resourcesIn []*kubernetes.Resource
	var resourcesOut []*kubernetes.Resource

	// Config Load
	cfg, err := config.Load(*cfgPathOpt)
	if err != nil {
		log.Printf("[ERR] Cannot load %s file: %v\n", *cfgPathOpt, err)
		os.Exit(1)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		resourcesIn, err = getKubernetesResources(cfg.In)
		if err != nil {
			log.Printf("[ERR] Failed to get Kubernetes resources (in): %v\n", err)
			os.Exit(1)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		resourcesOut, err = getKubernetesResources(cfg.Out)
		if err != nil {
			log.Printf("[ERR] Failed to get Kubernetes resources (out): %v\n", err)
			os.Exit(1)
		}
	}()

	wg.Wait()

	untrackedResources := listUntrackedResources(resourcesIn, resourcesOut, cfg.Exclude)
	switch {
	case *outputOpt == "text":
		outputs.Text(untrackedResources)
	case *outputOpt == "yaml":
		outputs.YAML(untrackedResources)
	default:
		outputs.Text(untrackedResources)
	}
}

func getKubernetesResources(cfgs []*config.CommandConfig) ([]*kubernetes.Resource, error) {
	const yamlSeparator = "---\n"
	var resources []*kubernetes.Resource

	var wg sync.WaitGroup
	var mutex = &sync.Mutex{}

	for _, cfg := range cfgs {
		wg.Add(1)
		go func(cmd string, args ...string) {
			defer wg.Done()
			stdout, stderr, err := utils.Exec(cmd, args...)
			if err != nil {
				log.Println(cmd, args)
				log.Println(string(stderr))
				log.Println(err)
				os.Exit(1)
			}

			for _, yml := range strings.Split(string(stdout), yamlSeparator) {
				resource := kubernetes.Resource{}

				yaml.Unmarshal([]byte(yml), &resource)
				if err != nil {
					log.Println(err)
					os.Exit(1)
				}

				if resource.Empty() {
					// Failed to ummarshal properly
					continue
				}
				mutex.Lock()
				// When 'kubectl get' is used, an object List is returned with
				// all resources in items array
				if resource.Kind == "List" && resource.Items != nil {
					for _, item := range resource.Items {
						resources = append(resources, item)
					}
				} else {
					resources = append(resources, &resource)
				}
				mutex.Unlock()
			}

		}(cfg.Cmd, cfg.Args...)
	}
	wg.Wait()
	return resources, nil
}

func listUntrackedResources(in []*kubernetes.Resource, out []*kubernetes.Resource, kindExclude []string) []*kubernetes.Resource {
	var untrackedResources []*kubernetes.Resource
	for _, resourceOut := range out {
		if utils.StringInListCaseInsensitive(kindExclude, resourceOut.Kind) {
			continue
		}
		found := false
		for _, resourceIn := range in {
			if resourceOut.ID() == resourceIn.ID() {
				found = true
				break
			}
		}
		if !found {
			untrackedResources = append(untrackedResources, resourceOut)
		}
	}

	return untrackedResources
}
