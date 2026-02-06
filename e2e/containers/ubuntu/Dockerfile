FROM ubuntu:24.04

# Install minimal dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    file

# Create test user
RUN useradd -m -s /bin/bash testuser

# Copy toto binary
COPY toto /usr/local/bin/toto
RUN chmod +x /usr/local/bin/toto

# Setup user environment
USER testuser
WORKDIR /home/testuser

# Add ~/.local/bin and ~/go/bin to PATH
ENV PATH="/home/testuser/.local/bin:/home/testuser/go/bin:${PATH}"

# Copy resource definitions to ~/manifests directory
# runtime.cue.upgrade is not a .cue file so toto won't load it until renamed
COPY --chown=testuser:testuser config/manifests/runtime.cue /home/testuser/manifests/runtime.cue
COPY --chown=testuser:testuser config/manifests/runtime.cue.upgrade /home/testuser/manifests/runtime.cue.upgrade
COPY --chown=testuser:testuser config/manifests/tools.cue /home/testuser/manifests/tools.cue
COPY --chown=testuser:testuser config/manifests/delegation.cue /home/testuser/manifests/delegation.cue
COPY --chown=testuser:testuser config/manifests/toolset.cue /home/testuser/manifests/toolset.cue

# Copy registry test manifests
# tools.cue.old contains older versions for upgrade/downgrade tests
COPY --chown=testuser:testuser config/registry/tools.cue /home/testuser/manifests/registry/tools.cue
COPY --chown=testuser:testuser config/registry/tools.cue.old /home/testuser/manifests/registry/tools.cue.old

# Copy dependency test manifests
COPY --chown=testuser:testuser config/dependency-test/ /home/testuser/dependency-test/

# Default command
CMD ["toto", "version"]
