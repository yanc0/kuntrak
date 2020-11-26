package main

import (
	"bytes"
	"flag"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"sync"

	"github.com/yanc0/untrak/outputs"
	"github.com/yanc0/untrak/utils"
	yaml "gopkg.in/yaml.v2"

	"github.com/yanc0/untrak/kubernetes"

	"github.com/yanc0/untrak/config"
)

func main() {
	// Flags, command line parameters
	var cfgPathOpt = flag.String("config", "./untrak.yaml", "untrak Config Path")
	var outputOpt = flag.String("o", "text", "Output format")
	var failOpt = flag.Bool("fail", false, "Fail on untracked resources")
	flag.Parse()

	var wg sync.WaitGroup
	var resourcesIn []*kubernetes.Resource
	var resourcesOut []*kubernetes.Resource

	// Config Load
	cfg, err := config.Load(*cfgPathOpt)
	if err != nil {
		log.Printf("[ERR] Cannot load %s file: %v\n", *cfgPathOpt, err)
		os.Exit(1)
	}

	cfg.NonNamespaced = append(cfg.NonNamespaced, kubernetes.DefaultNonNamespacedResources...)

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

	untrackedResources := listUntrackedResources(resourcesIn, resourcesOut, cfg.Exclude, cfg.NonNamespaced)
	switch {
	case *outputOpt == "text":
		outputs.Text(untrackedResources)
	case *outputOpt == "yaml":
		outputs.YAML(untrackedResources)
	default:
		outputs.Text(untrackedResources)
	}

	if len(untrackedResources) > 0 && *failOpt {
		os.Exit(1)
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

			// substitute env variables if any has been set
			for i, _ := range args {
				args[i] = os.ExpandEnv(args[i])
			}

			c := exec.Command(cmd, args...)
			var outb, errb bytes.Buffer
			c.Stdout = &outb
			c.Stderr = &errb
			err := c.Run()
			if err != nil {
				log.Fatal(err, errb.String())
			}
			stdoutDec := yaml.NewDecoder(&outb)
			for {
				tempResource := &kubernetes.Resource{}
				err := stdoutDec.Decode(tempResource)
				if err != nil && err != io.EOF {
					log.Printf("[ERR] Failed to decode yaml stream: %s\n", err.Error())
					os.Exit(1)
				}
				if err == io.EOF {
					break
				}
				if tempResource.Kind == "List" {
					mutex.Lock()
					resources = append(resources, tempResource.Items...)
					mutex.Unlock()
					continue
				}
				// Resource can be empty if yaml file has return lines, separators or comments
				// for example:
				// # empty resource
				// ---
				// ---
				// YAML decoder consider these lines valid but resource will be uninitialized
				if !tempResource.Empty() {
					mutex.Lock()
					resources = append(resources, tempResource)
					mutex.Unlock()
				}
			}
		}(cfg.Cmd, cfg.Args...)
	}
	wg.Wait()
	return resources, nil
}

func listUntrackedResources(in []*kubernetes.Resource, out []*kubernetes.Resource, excludeRegexp []string, nonNamespaced []string) []*kubernetes.Resource {
	var compiledExcludeRegexp []*regexp.Regexp
	for _, reg := range excludeRegexp {
		compiledExcludeRegexp = append(compiledExcludeRegexp, regexp.MustCompile("(?i)"+reg))
	}

	var untrackedResources []*kubernetes.Resource
	for _, resourceOut := range out {
		// Resource is in the exclude list, skip it
		if utils.StringListRegexpMatch(compiledExcludeRegexp, resourceOut.ID()) {
			continue
		}
		found := false
		for _, resourceIn := range in {

			// If input resource is not namespaced, compare only kind and Name
			if utils.StringInListCaseInsensitive(nonNamespaced, resourceIn.Kind) {
				if resourceOut.Kind == resourceIn.Kind && resourceOut.Metadata.Name == resourceIn.Metadata.Name {
					found = true
					break
				}
			}

			// If resource has been found in both IN an OUT, there is nothing to do
			if resourceOut.ID() == resourceIn.ID() {
				found = true
				break
			}
		}
		// If resource OUT is not found in IN, it is untracked
		if !found {
			untrackedResources = append(untrackedResources, resourceOut)
		}
	}

	return untrackedResources
}
