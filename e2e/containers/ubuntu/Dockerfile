FROM ubuntu:24.04

# Install minimal dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates

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
COPY --chown=testuser:testuser config/runtime.cue /home/testuser/manifests/runtime.cue
COPY --chown=testuser:testuser config/runtime.cue.upgrade /home/testuser/manifests/runtime.cue.upgrade
COPY --chown=testuser:testuser config/tools.cue /home/testuser/manifests/tools.cue
COPY --chown=testuser:testuser config/delegation.cue /home/testuser/manifests/delegation.cue

# Default command
CMD ["toto", "version"]
