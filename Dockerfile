FROM golang as build

ADD . /src
RUN ["/bin/bash", "-exo", "pipefail", "-c", "cd /src; go generate; CGO_ENABLED=0 go build -o /dockerhub-chainreactor ."]


FROM scratch

COPY --from=build /dockerhub-chainreactor /dockerhub-chainreactor
COPY --from=grandmaster/cacerts /etc/ssl/certs /etc/ssl/certs

WORKDIR /data
CMD ["/dockerhub-chainreactor"]
