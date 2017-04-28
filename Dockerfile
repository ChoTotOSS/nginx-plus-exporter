FROM        alpine:3.5
WORKDIR /bin
COPY dist/nginx-plus-exporter /bin/
RUN chmod +x /bin/nginx-plus-exporter
EXPOSE 9913
ENTRYPOINT ["./nginx-plus-exporter"]
