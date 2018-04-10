package main

import (
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/containers/image/copy"
	"github.com/containers/image/directory"
	"github.com/containers/image/docker"
	"github.com/containers/image/image"
	"github.com/containers/image/signature"
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
	insecure       = flag.Bool("insecure", true, "FIXME") // FIXME: by default should be false
	pullImage      = flag.String("pull-image", "openshift/httpd:latest", "FIXME")
	pushImageDir   = flag.String("push-image-dir", "/usr/share/monitor-image-registry/dir-image", "FIXME")
	pushImage      = flag.String("push-image", "smoketest/push-image:latest", "FIXME")
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

var (
	problem = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "problem",
			Help:      "The indicator of problems.",
		},
		[]string{"name"},
	)
	timestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "timestamp",
			Help:      "The timestamp when checks were performed for the last time.",
		},
		[]string{"name", "result"},
	)
)

func init() {
	prometheus.MustRegister(problem)
	prometheus.MustRegister(timestamp)
}

const (
	pullImageCheck = "pull_image"
	pushImageCheck = "push_image"
)

type checkResult struct {
	error
}

func (r checkResult) Float64() float64 {
	if r.error != nil {
		return 1
	}
	return 0
}

func (r checkResult) ResultLabel() string {
	if r.error != nil {
		return "failure"
	}
	return "success"
}

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
			if err != nil {
				glog.Errorf("pull image check failed: %v", err)
			}

			r := checkResult{error: err}
			// XXX(dmage): update is not atomic
			problem.With(prometheus.Labels{"name": pullImageCheck}).Set(r.Float64())
			timestamp.With(prometheus.Labels{"name": pullImageCheck, "result": r.ResultLabel()}).SetToCurrentTime()

			time.Sleep(30 * time.Second)
		}
	}()

	go func() {
		for {
			err := tryToPushImage()
			if err != nil {
				glog.Errorf("push image check failed: %v", err)
			}

			r := checkResult{error: err}
			// XXX(dmage): update is not atomic
			problem.With(prometheus.Labels{"name": pushImageCheck}).Set(r.Float64())
			timestamp.With(prometheus.Labels{"name": pushImageCheck, "result": r.ResultLabel()}).SetToCurrentTime()

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
		DockerInsecureSkipTLSVerify: *insecure,
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

func tryToPushImage() error {
	srcRef, err := directory.NewReference(*pushImageDir)
	if err != nil {
		return fmt.Errorf("invalid source name dir:%s: %v", *pushImageDir, err)
	}

	dstName := fmt.Sprintf("//%s/%s", *registry, *pushImage)
	dstRef, err := docker.ParseReference(dstName)
	if err != nil {
		return fmt.Errorf("invalid destination name docker:%s: %v", dstName, err)
	}

	systemContext := &types.SystemContext{
		DockerInsecureSkipTLSVerify: *insecure,
		DockerAuthConfig: &types.DockerAuthConfig{
			Username: "unused--monitor-image-registry",
			Password: kubeconfig.BearerToken, // FIXME(dmage): it's empty when the cert-based authentication is used
		},
	}

	policy, err := signature.DefaultPolicy(systemContext)
	if err != nil {
		return fmt.Errorf("unable to get default policy: %v", err)
	}

	policyContext, err := signature.NewPolicyContext(policy)
	if err != nil {
		return fmt.Errorf("unable to create new policy context: %v", err)
	}

	if err := copy.Image(policyContext, dstRef, srcRef, &copy.Options{
		SourceCtx:      systemContext,
		DestinationCtx: systemContext,
	}); err != nil {
		return fmt.Errorf("unable to copy from dir:%s to docker:%s: %v", *pushImageDir, dstName, err)
	}
	glog.V(1).Infof("check succeed: was able to push image %s", dstRef.DockerReference())

	return nil
}
