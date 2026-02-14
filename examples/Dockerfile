FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    build-essential \
    vim

RUN useradd -m -s /bin/bash lipnoise

COPY tomei /usr/local/bin/tomei
RUN chmod +x /usr/local/bin/tomei

USER lipnoise
WORKDIR /home/lipnoise

ENV PATH="/home/lipnoise/.local/bin:${PATH}"

COPY --chown=lipnoise:lipnoise minimal/ /home/lipnoise/examples/minimal/
COPY --chown=lipnoise:lipnoise real-world/ /home/lipnoise/examples/real-world/

CMD ["bash"]
