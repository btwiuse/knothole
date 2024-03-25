////go:generate go run pkg/codegen/cleanup/main.go
////go:generate go run pkg/codegen/main.go

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	teleport "github.com/webteleport/ufo/apps/teleport/handler"

	"github.com/webteleport/wtf"

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

	// "github.com/btwiuse/knothole/pkg/generated/controllers/samplecontroller.k8s.io"
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
	Exec    func(func())  `json:"-"`
	Stop    chan struct{} `json:"-"`
	Ingress *nv1.Ingress  `json:"Ingress"`
}

type IngressEvent struct {
	*nv1.Ingress
	Action string
}

var IngressEvents = make(chan IngressEvent, 100)

var AllRoutes = map[string]Routes{}

type Routes map[string]string

func ProcessIngressEvent() {
	ev := <-IngressEvents
	if ev.Action == "Delete" {
		for _, rule := range ev.Ingress.Spec.Rules {
			host := rule.Host
			delete(AllRoutes, host)
		}
		return
	}
	// log.Println("TODO", ev.ObjectMeta.Namespace, ev.ObjectMeta.Name)
	for _, rule := range ev.Ingress.Spec.Rules {
		host := rule.Host
		log.Println("-", host)
		routes, ok := AllRoutes[host]
		if !ok {
			AllRoutes[host] = map[string]string{}
			routes = AllRoutes[host]
			log.Println("(create)")
		} else {
			log.Println("(update)")
		}
		for _, path := range rule.IngressRuleValue.HTTP.Paths {
			// assuming service is used instead of resource reference
			if path.Backend.Service == nil {
				log.Println("Warn: unsupported backend type")
				continue
			}
			ns := "default"
			svc := path.Backend.Service.Name
			port := path.Backend.Service.Port.Number
			upstream := fmt.Sprintf("%s.%s.svc.cluster.local:%d", svc, ns, port)
			log.Println("  -", path.Path, upstream)

			relay := fmt.Sprintf("https://ufo.k0s.io/%s?clobber=ingress&persist=1", host)
			go wtf.Serve(relay, teleport.Handler(upstream))

			s, ok := routes[path.Path]
			if !ok {
				// create path
				routes[path.Path] = upstream
			} else {
				if s != upstream {
					// update path
					routes[path.Path] = upstream
				}
			}
		}
	}
	// TODO
}

var Debouncers *DebouncerMap = &DebouncerMap{
	Mutex:      &sync.Mutex{},
	Debouncers: map[string]*Debouncer{},
}

type DebouncerMap struct {
	*sync.Mutex `json:"-"`
	Debouncers  map[string]*Debouncer `json:"Debouncers"`
}

func (m *DebouncerMap) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	m.Lock()
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		log.Println(err)
	}
	io.WriteString(w, string(b))
	defer m.Unlock()
}

func (m *DebouncerMap) Del(s string) {
	m.Lock()
	defer m.Unlock()
	if v, ok := m.Debouncers[s]; ok {
		delete(m.Debouncers, s)
		v.Stop <- struct{}{}
	}
}

func (m *DebouncerMap) Get(s string, i *nv1.Ingress) *Debouncer {
	m.Lock()
	defer m.Unlock()
	v, ok := m.Debouncers[s]
	if !ok {
		m.Debouncers[s] = &Debouncer{
			Exec:    debounce.New(2 * time.Second),
			Stop:    make(chan struct{}, 1),
			Ingress: i,
		}
		return m.Debouncers[s]
	}
	return v
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
	go func() {
		for {
			ProcessIngressEvent()
		}
	}()
	ingressController.OnRemove(ctx, "ingress-handler", func(s string, i *nv1.Ingress) (*nv1.Ingress, error) {
		go (func() {
			time.Sleep(time.Second)
			debouncer := Debouncers.Get(s, i)
			debouncer.Exec(func() {
				log.Println("# on delete", s)
				Debouncers.Del(s)
				IngressEvents <- IngressEvent{Ingress: i, Action: "Delete"}
			})
		})()
		return nil, nil
	})
	ingressController.OnChange(ctx, "ingress-handler", func(s string, i *nv1.Ingress) (*nv1.Ingress, error) {
		if i == nil {
			return nil, nil
		}
		go (func() {
			debouncer := Debouncers.Get(s, i)
			debouncer.Exec(func() {
				// log.Println(i)
				// log.Println(i.ObjectMeta.Annotations)
				// v, ok := i.ObjectMeta.Annotations["kubernetes.io/ingress.class"]
				// log.Println(v, ok)
				v := i.Spec.IngressClassName
				if v != nil && *v == "k0s" {
					log.Println("# on update", s)
					IngressEvents <- IngressEvent{Ingress: i, Action: "Update"}
				}
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

	mux := http.NewServeMux()
	mux.Handle("/debouncers", Debouncers)
	wtf.Serve("https://ufo.k0s.io", mux)
	<-done
}
