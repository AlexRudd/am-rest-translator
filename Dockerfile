# am-rest-translator
FROM scratch

# Add ca-certificates.crt
ADD ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Add executable
ADD translator /

ENTRYPOINT [ "/translator" ]
