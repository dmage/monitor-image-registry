FROM openshift/origin-base

COPY hello.c .
COPY make-dir-image.sh .
COPY default-policy.json /etc/containers/policy.json

RUN INSTALL_PKGS="openshift-monitor-image-registry" && \
    yum --enablerepo=origin-local-release install -y ${INSTALL_PKGS} && \
    rpm -V ${INSTALL_PKGS} && \
    yum install -y gcc glibc-static shasum && \
    ./make-dir-image.sh && \
    yum -y remove gcc glibc-static shasum && \
    yum clean all

LABEL io.k8s.display-name="OpenShift Container Platform Image Registry Monitor" \
      io.k8s.description="This is a component of OpenShift Container Platform and provides a monitoring component." \
      io.openshift.tags="openshift,docker,registry"

# doesn't require a root user.
USER 1001
EXPOSE 8080

CMD ["/usr/bin/monitor-image-registry", "-logtostderr", "-v=1"]
