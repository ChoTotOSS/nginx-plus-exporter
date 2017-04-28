FROM        alpine:3.5
WORKDIR /bin
COPY dist/exporter /bin/
COPY docker-entrypoint.sh /bin/
RUN chmod +x /bin/exporter

ENTRYPOINT ["./exporter"]
