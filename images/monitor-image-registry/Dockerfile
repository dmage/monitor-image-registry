FROM openshift/origin-base

RUN INSTALL_PKGS="openshift-monitor-image-registry" && \
    yum --enablerepo=origin-local-release install -y ${INSTALL_PKGS} && \
    rpm -V ${INSTALL_PKGS} && \
    yum clean all

LABEL io.k8s.display-name="OpenShift Container Platform Image Registry Monitor" \
      io.k8s.description="This is a component of OpenShift Container Platform and provides a monitoring component."
      io.openshift.tags="openshift,docker,registry"

# doesn't require a root user.
USER 1001
EXPOSE 8080

CMD ["/usr/bin/monitor-image-registry", "-logtostderr", "-v=1"]
