FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    vim

RUN useradd -m -s /bin/bash lipnoise

COPY tomei /usr/local/bin/tomei
RUN chmod +x /usr/local/bin/tomei

USER lipnoise
WORKDIR /home/lipnoise

ENV PATH="/home/lipnoise/.local/bin:${PATH}"

COPY --chown=lipnoise:lipnoise minimal/ /home/lipnoise/examples/minimal/

CMD ["bash"]
