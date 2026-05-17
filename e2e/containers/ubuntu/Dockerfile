FROM ubuntu:24.04

# Install minimal dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    file \
    gcc \
    gnupg \
    libc6-dev \
    sudo

# Create test user with passwordless sudo for privileged tool tests
RUN useradd -m -s /bin/bash testuser && \
    echo "testuser ALL=(ALL) NOPASSWD: ALL" > /etc/sudoers.d/testuser && \
    chown root:root /etc/sudoers.d/testuser && \
    chmod 0440 /etc/sudoers.d/testuser

# Copy tomei binary
COPY tomei /usr/local/bin/tomei
RUN chmod +x /usr/local/bin/tomei

# Setup user environment
USER testuser
WORKDIR /home/testuser

# Add ~/.local/bin and ~/go/bin to PATH
ENV PATH="/home/testuser/.local/bin:/home/testuser/go/bin:${PATH}"

# Copy resource definitions to ~/manifests directory
# runtime.cue.upgrade is not a .cue file so tomei won't load it until renamed
COPY --chown=testuser:testuser config/manifests/runtime.cue /home/testuser/manifests/runtime.cue
COPY --chown=testuser:testuser config/manifests/runtime.cue.upgrade /home/testuser/manifests/runtime.cue.upgrade
COPY --chown=testuser:testuser config/manifests/tools.cue /home/testuser/manifests/tools.cue
COPY --chown=testuser:testuser config/manifests/delegation.cue /home/testuser/manifests/delegation.cue
COPY --chown=testuser:testuser config/manifests/toolset.cue /home/testuser/manifests/toolset.cue
# Copy delegation test manifests (separate directory to avoid affecting basic tests)
COPY --chown=testuser:testuser config/delegation-test/ /home/testuser/delegation-test/

# Copy registry test manifests
# tools.cue.old contains older versions for upgrade/downgrade tests
COPY --chown=testuser:testuser config/registry/tools.cue /home/testuser/manifests/registry/tools.cue
COPY --chown=testuser:testuser config/registry/tools.cue.old /home/testuser/manifests/registry/tools.cue.old
# Copy dependency test manifests
COPY --chown=testuser:testuser config/dependency-test/ /home/testuser/dependency-test/

# Copy installer-repo-test manifests
COPY --chown=testuser:testuser config/installer-repo-test/ /home/testuser/installer-repo-test/

# Copy logs-test manifests
COPY --chown=testuser:testuser config/logs-test/ /home/testuser/logs-test/

# Copy three-segment package test manifests
COPY --chown=testuser:testuser config/three-segment-test/ /home/testuser/three-segment-test/

# Copy taint-on-upgrade test manifests
COPY --chown=testuser:testuser config/taint-on-upgrade-test/ /home/testuser/taint-on-upgrade-test/

# Copy tar-xz-test manifests
COPY --chown=testuser:testuser config/tar-xz-test/ /home/testuser/tar-xz-test/

# Copy update-flags-test manifests
COPY --chown=testuser:testuser config/update-flags-test/ /home/testuser/update-flags-test/

# Copy commands-test manifests
COPY --chown=testuser:testuser config/commands-test/ /home/testuser/commands-test/

# Copy binary-name-test manifests
COPY --chown=testuser:testuser config/binary-name-test/ /home/testuser/binary-name-test/

# Copy privileged-test manifests
COPY --chown=testuser:testuser config/privileged-test/ /home/testuser/privileged-test/

# Copy system-package-test manifests
COPY --chown=testuser:testuser config/system-package-test/ /home/testuser/system-package-test/

# Default command
CMD ["tomei", "version"]
