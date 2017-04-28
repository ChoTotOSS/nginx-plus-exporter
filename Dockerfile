FROM        alpine:3.5
WORKDIR /bin
COPY dist/nginx-plus-exporter /bin/
RUN chmod +x /bin/nginx-plus-exporter

ENTRYPOINT ["./nginx-plus-exporter"]
