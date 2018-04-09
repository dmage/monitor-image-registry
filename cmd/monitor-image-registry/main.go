package main

import (
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/containers/image/docker"
	"github.com/containers/image/image"
	"github.com/containers/image/types"
	"github.com/golang/glog"
	"github.com/opencontainers/go-digest"
	"github.com/prometheus/client_golang/prometheus"

	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	addr           = flag.String("listen", ":8080", "the address to listen on for HTTP requests")
	kubeconfigPath = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	registry       = flag.String("registry", "docker-registry.default.svc.cluster.local:5000", "FIXME")
	pullImage      = flag.String("pull-image", "openshift/httpd:latest", "FIXME")
)

func buildConfigFromFlags(kubeconfigPath string) (*restclient.Config, error) {
	if kubeconfigPath == "" {
		glog.Warning("flag -kubeconfig was not specified, using inClusterConfig")
		kubeconfig, err := restclient.InClusterConfig()
		if err == nil {
			return kubeconfig, nil
		}
		glog.Warningf("error creating inClusterConfig, falling back to default config: %v", err)
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.ExplicitPath = kubeconfigPath
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
}

const namespace = "monitor_image_registry"

type check struct {
	problem   *prometheus.GaugeVec
	timestamp *prometheus.GaugeVec
}

func newCheck(name string) *check {
	c := &check{
		problem: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: name,
				Name:      "failed",
				Help:      "The latency of various app creation steps.",
			},
			nil,
		),
		timestamp: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: name,
				Name:      "timestamp",
				Help:      "The latency of various app creation steps.",
			},
			nil,
		),
	}
	prometheus.MustRegister(c.problem)
	prometheus.MustRegister(c.timestamp)
	return c
}

var (
	pullImageCheck = newCheck("pull_image")
)

var kubeconfig *restclient.Config

func main() {
	flag.Parse()

	http.HandleFunc("/healthz", handleHealthz)
	http.Handle("/metrics", prometheus.Handler())

	var err error
	kubeconfig, err = buildConfigFromFlags(*kubeconfigPath)
	if err != nil {
		glog.Exitf("failed to load kubernetes config: %v", err)
	}

	go func() {
		for {
			err := tryToPullImage()
			if err == nil {
				// XXX(dmage): update is not atomic
				pullImageCheck.problem.With(nil).Set(0)
				pullImageCheck.timestamp.With(prometheus.Labels{"result": "success"}).SetToCurrentTime()
			} else {
				glog.Error(err)
				pullImageCheck.problem.With(nil).Set(1)
				pullImageCheck.timestamp.With(prometheus.Labels{"result": "failure"}).SetToCurrentTime()
			}
			time.Sleep(30 * time.Second)
		}
	}()

	glog.Fatal(http.ListenAndServe(*addr, nil))
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "ok")
}

func tryToPullImage() error {
	ref, err := docker.ParseReference(fmt.Sprintf("//%s/%s", *registry, *pullImage))
	if err != nil {
		return err
	}

	systemContext := &types.SystemContext{
		DockerInsecureSkipTLSVerify: true,
		DockerAuthConfig: &types.DockerAuthConfig{
			Username: "unused--monitor-image-registry",
			Password: kubeconfig.BearerToken, // FIXME(dmage): it's empty when the cert-based authentication is used
		},
	}

	src, err := ref.NewImageSource(systemContext)
	if err != nil {
		return fmt.Errorf("check image %s: %v", src.Reference().DockerReference(), err)
	}
	defer src.Close()

	glog.V(1).Infof("checking image %s...", src.Reference().DockerReference())
	img, err := image.FromUnparsedImage(systemContext, image.UnparsedInstance(src, nil))
	if err != nil {
		return fmt.Errorf("check image %s: %v", src.Reference().DockerReference(), err)
	}

	glog.V(1).Infof("checking layers for image %s...", src.Reference().DockerReference())
	layers := img.LayerInfos()
	for _, layer := range layers {
		if err := func() error {
			glog.V(1).Infof("checking layer %s for image %s...", layer.Digest, src.Reference().DockerReference())
			r, _, err := src.GetBlob(layer)
			if err != nil {
				return err
			}
			defer r.Close()
			dgst, err := digest.FromReader(r)
			if err != nil {
				return err
			}
			if dgst != layer.Digest {
				return fmt.Errorf("digest mismatch")
			}
			return nil
		}(); err != nil {
			return fmt.Errorf("check image %s: layer %s: %v", src.Reference().DockerReference(), layer.Digest, err)
		}
	}
	glog.V(1).Infof("check succeed: was able to pull image %s", src.Reference().DockerReference())

	return nil
}
