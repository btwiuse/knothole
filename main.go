////go:generate go run pkg/codegen/cleanup/main.go
////go:generate go run pkg/codegen/main.go

package main

import (
	"context"
	"sync"
	"time"

	"github.com/bep/debounce"

	//"encoding/json"
	"flag"
	//"fmt"
	"log"

	// "gopkg.in/yaml.v3"
	"github.com/kubernot/wrangler/pkg/generic"
	//"sigs.k8s.io/yaml"

	"github.com/rancher/lasso/pkg/controller"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kubernot/wrangler/pkg/generated/controllers/apps"
	"github.com/kubernot/wrangler/pkg/generated/controllers/networking.k8s.io"
	v1 "github.com/kubernot/wrangler/pkg/generated/controllers/networking.k8s.io/v1"
	nv1 "k8s.io/api/networking/v1"

	// "github.com/btwiuse/wrangler-sample/pkg/generated/controllers/samplecontroller.k8s.io"
	"github.com/kubernot/wrangler/pkg/kubeconfig"
	"github.com/kubernot/wrangler/pkg/signals"
	"github.com/kubernot/wrangler/pkg/start"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
)

var (
	masterURL      string
	kubeconfigFile string
)

func init() {
	flag.StringVar(&kubeconfigFile, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.Parse()
}

type Debouncer struct {
	Exec func(func())
	Stop chan struct{}
}

var Debouncers = map[string]*Debouncer{}
var DebouncersLock sync.Mutex

func GetDebouncer(s string) *Debouncer {
	DebouncersLock.Lock()
	defer DebouncersLock.Unlock()
	if _, ok := Debouncers[s]; !ok {
		Debouncers[s] = &Debouncer{
			Exec: debounce.New(2 * time.Second),
			Stop: make(chan struct{}, 1),
		}
	}
	return Debouncers[s]
}

func main() {
	ctx := context.Background()
	// set up signals so we handle the first shutdown signal gracefully
	done := signals.SetupSignalHandler()

	// This will load the kubeconfig file in a style the same as kubectl
	cfg, err := kubeconfig.GetNonInteractiveClientConfig(kubeconfigFile).ClientConfig()
	if err != nil {
		logrus.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	// Raw k8s client, used to events
	kubeClient := kubernetes.NewForConfigOrDie(cfg)
	_ = kubeClient

	scheme := runtime.NewScheme()
	nv1.AddToScheme(scheme)
	controllerFactory, err := controller.NewSharedControllerFactoryFromConfig(cfg, scheme)
	if err != nil {
		panic(err)
	}
	_ = controllerFactory
	opts := &generic.FactoryOptions{
		SharedControllerFactory: controllerFactory,
	}

	// Generated apps controller
	apps := apps.NewFactoryFromConfigOrDie(cfg)
	_ = apps
	networking := networking.NewFactoryFromConfigWithOptionsOrDie(cfg, opts)
	_ = networking
	ingressController := v1.New(controllerFactory).Ingress()
	counter := 0
	ingressController.OnRemove(ctx, "ingress-handler", func(s string, i *nv1.Ingress) (*nv1.Ingress, error) {
		counter += 1
		go (func() {
			time.Sleep(time.Second)
			GetDebouncer(s).Exec(func() {
				log.Println("# on remove", s, counter)
			})
		})()
		return nil, nil
	})
	ingressController.OnChange(ctx, "ingress-handler", func(s string, i *nv1.Ingress) (*nv1.Ingress, error) {
		counter += 1
		if i == nil {
			return nil, nil
		}
		go (func() {
			GetDebouncer(s).Exec(func() {
				log.Println("# on change", s, counter)
			})
		})()
		return i, nil
	})
	// Generated sample controller
	// sample := samplecontroller.NewFactoryFromConfigOrDie(cfg)

	// The typical pattern is to build all your controller/clients then just pass to each handler
	// the bare minimum of what they need.  This will eventually help with writing tests.  So
	// don't pass in something like kubeClient, apps, or sample
	/*
		Register(ctx,
			kubeClient.CoreV1().Events(""),
			apps.Apps().V1().Deployment(),
			sample.Samplecontroller().V1alpha1().Foo())
	*/

	// Start all the controllers
	if err := start.All(ctx, 2, apps, networking); err != nil {
		logrus.Fatalf("Error starting: %s", err.Error())
	}

	<-done
}
