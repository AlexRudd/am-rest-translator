CGO_ENABLED=0 GOOS=linux go build -o translator main.go
[ -e ca-certificates.crt ] || wget https://curl.haxx.se/ca/cacert.pem -O ca-certificates.crt
docker build -t $1 .
