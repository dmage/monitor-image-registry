## Build from the sources

    hack/env make build-images

## Get the pre-built image

    docker pull dmage/origin-monitor-image-registry
    docker tag dmage/origin-monitor-image-registry openshift/origin-monitor-image-registry

## Run the monitor

    oc -n smoketest adm policy add-role-to-user admin -z default
    oc new-app openshift/origin-monitor-image-registry
    oc patch dc/origin-monitor-image-registry -p '{"spec":{"template":{"spec":{"containers":[{"name":"origin-monitor-image-registry","imagePullPolicy":"Never"}]}}}}'
